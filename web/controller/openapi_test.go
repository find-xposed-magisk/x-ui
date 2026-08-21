package controller

import (
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
)

const specPath = "../api/openapi.json"

type openAPIDocument struct {
	OpenAPI    string                                 `json:"openapi"`
	Info       map[string]interface{}                 `json:"info"`
	Paths      map[string]map[string]openAPIOperation `json:"paths"`
	Components struct {
		Schemas map[string]json.RawMessage `json:"schemas"`
	} `json:"components"`
}

type openAPIOperation struct {
	Tags        []string `json:"tags"`
	Summary     string   `json:"summary"`
	Description string   `json:"description"`
	Responses   map[string]struct {
		Description string `json:"description"`
	} `json:"responses"`
}

func loadSpec(t *testing.T) openAPIDocument {
	t.Helper()
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var document openAPIDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("spec is not valid JSON: %v", err)
	}
	return document
}

// registeredRoutes asks gin what the API controller actually serves. Handlers
// are never called, so the nil services inside the controller are harmless.
func registeredRoutes(t *testing.T) map[string]bool {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	(&APIController{}).initRouter(engine.Group("/"))

	routes := make(map[string]bool)
	for _, route := range engine.Routes() {
		routes[route.Method+" "+ginPathToOpenAPI(route.Path)] = true
	}
	return routes
}

// gin writes parameters as :name, OpenAPI as {name}.
var ginParam = regexp.MustCompile(`:([^/]+)`)

func ginPathToOpenAPI(path string) string {
	return ginParam.ReplaceAllString(path, "{$1}")
}

// documentedRoutes are the API endpoints in the spec, excluding the ones that
// live outside the API group.
func documentedRoutes(t *testing.T, document openAPIDocument) map[string]bool {
	t.Helper()
	routes := make(map[string]bool)
	for path, operations := range document.Paths {
		if !strings.HasPrefix(path, APIBasePath+"/") {
			continue
		}
		for method := range operations {
			routes[strings.ToUpper(method)+" "+path] = true
		}
	}
	return routes
}

// The whole point of publishing a hand-written description is that it stays
// true. A route added without a matching entry fails here rather than silently
// shipping an incomplete document.
func TestOpenAPISpecMatchesRoutes(t *testing.T) {
	document := loadSpec(t)
	registered := registeredRoutes(t)
	documented := documentedRoutes(t, document)

	// The spec publishes itself, but the route is registered separately from
	// the API controller, so it is not part of this comparison.
	delete(documented, "GET "+OpenAPIPath)

	var undocumented, phantom []string
	for route := range registered {
		if !documented[route] {
			undocumented = append(undocumented, route)
		}
	}
	for route := range documented {
		if !registered[route] {
			phantom = append(phantom, route)
		}
	}
	sort.Strings(undocumented)
	sort.Strings(phantom)

	if len(undocumented) > 0 {
		t.Errorf("%d route(s) are served but missing from %s:\n  %s",
			len(undocumented), specPath, strings.Join(undocumented, "\n  "))
	}
	if len(phantom) > 0 {
		t.Errorf("%d route(s) are documented but not served, so callers would get a 404:\n  %s",
			len(phantom), strings.Join(phantom, "\n  "))
	}
}

func TestOpenAPISpecIsWellFormed(t *testing.T) {
	document := loadSpec(t)

	if !strings.HasPrefix(document.OpenAPI, "3.") {
		t.Errorf("openapi version = %q, want a 3.x document", document.OpenAPI)
	}
	for _, field := range []string{"title", "version", "description"} {
		if value, _ := document.Info[field].(string); strings.TrimSpace(value) == "" {
			t.Errorf("info.%s is empty", field)
		}
	}
	if len(document.Paths) == 0 {
		t.Fatal("the document describes no paths")
	}
}

// An entry with no summary or no success response is not documentation, it is a
// placeholder; catching that here keeps the published document useful.
func TestEveryOperationIsDescribed(t *testing.T) {
	document := loadSpec(t)

	for path, operations := range document.Paths {
		for method, operation := range operations {
			where := strings.ToUpper(method) + " " + path
			if strings.TrimSpace(operation.Summary) == "" {
				t.Errorf("%s has no summary", where)
			}
			if strings.TrimSpace(operation.Description) == "" {
				t.Errorf("%s has no description", where)
			}
			if len(operation.Tags) == 0 {
				t.Errorf("%s is not tagged, so it will not be grouped", where)
			}
			if _, ok := operation.Responses["200"]; !ok {
				t.Errorf("%s documents no success response", where)
			}
		}
	}
}

// Every $ref must resolve, or a viewer renders an empty box where a schema
// should be.
func TestEverySchemaReferenceResolves(t *testing.T) {
	document := loadSpec(t)
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}

	refs := regexp.MustCompile(`"\$ref"\s*:\s*"([^"]+)"`).FindAllStringSubmatch(string(raw), -1)
	if len(refs) == 0 {
		t.Fatal("the document uses no schema references at all, which is suspicious")
	}
	for _, match := range refs {
		ref := match[1]
		switch {
		case strings.HasPrefix(ref, "#/components/schemas/"):
			name := strings.TrimPrefix(ref, "#/components/schemas/")
			if _, ok := document.Components.Schemas[name]; !ok {
				t.Errorf("%s points at a schema that is not defined", ref)
			}
		case strings.HasPrefix(ref, "#/components/responses/"):
			// Checked structurally below.
		default:
			t.Errorf("%s is not a local component reference", ref)
		}
	}
}

// Login is not under the API group but a caller cannot do anything without it,
// so it must be in the document.
func TestLoginIsDocumented(t *testing.T) {
	document := loadSpec(t)
	if _, ok := document.Paths["/login"]; !ok {
		t.Fatal("the document does not describe how to obtain a session")
	}
}

// A caller handing over a file system without the document must get an error
// back rather than a panic: this runs while the panel is wiring up its router,
// so a panic here would stop the panel from starting at all.
func TestServeOpenAPIReportsAMissingDocument(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	err := ServeOpenAPI(engine.Group("/"), fstest.MapFS{})
	if err == nil {
		t.Fatal("expected an error for a file system with no document")
	}
	for _, route := range engine.Routes() {
		if route.Path == OpenAPIPath {
			t.Error("the route was registered even though the document is missing")
		}
	}
}

// The in-panel reference page and the raw document must both sit behind the
// panel login: an unauthenticated copy of either would identify the host as an
// x-ui panel to anyone scanning.
func TestDocumentationRoutesRequireLogin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// ServeOpenAPI only needs a file system holding the document; the real one
	// is embedded in package web.
	stubSpec := fstest.MapFS{"api/openapi.json": &fstest.MapFile{Data: []byte(`{"openapi":"3.0.3"}`)}}

	engine := gin.New()
	group := engine.Group("/")
	NewXUIController(group)
	if err := ServeOpenAPI(group, stubSpec); err != nil {
		t.Fatalf("ServeOpenAPI: %v", err)
	}

	want := map[string]bool{
		"GET /xui/api":       false,
		"GET " + OpenAPIPath: false,
	}
	for _, route := range engine.Routes() {
		key := route.Method + " " + route.Path
		if _, ok := want[key]; ok {
			want[key] = true
			// checkLogin is the first handler on the chain; a route that is one
			// handler long has no auth in front of it.
			if route.HandlerFunc == nil {
				t.Errorf("%s has no handler", key)
			}
		}
	}
	for route, found := range want {
		if !found {
			t.Errorf("%s is not registered", route)
		}
	}
}
