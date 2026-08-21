package web

import (
	"encoding/json"
	"html/template"
	"io/fs"
	"os"
	"strings"
	"testing"
)

// Every page is assembled from the embedded templates at startup, and a parse
// error there is swallowed so the panel only fails once a request comes in.
// Parsing them here turns a broken template into a failing build instead.
func TestEmbeddedHtmlTemplatesParse(t *testing.T) {
	funcMap := template.FuncMap{
		"i18n": func(key string, args ...interface{}) (template.HTML, error) { return "", nil },
	}

	parsed := 0
	err := fs.WalkDir(htmlFS, "html", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".html") {
			return nil
		}
		if _, err := template.New("").Funcs(funcMap).ParseFS(htmlFS, path); err != nil {
			t.Errorf("%s: %v", path, err)
			return nil
		}
		parsed++
		return nil
	})
	if err != nil {
		t.Fatalf("walk templates: %v", err)
	}
	if parsed == 0 {
		t.Fatal("no templates were parsed; the embedded FS looks empty")
	}
}

// getHtmlTemplate skips directories that hold no templates, so it must still
// end up with the whole set defined.
func TestGetHtmlTemplateDefinesEveryPage(t *testing.T) {
	server := NewServer()
	funcMap := template.FuncMap{
		"i18n": func(key string, args ...interface{}) (template.HTML, error) { return "", nil },
	}

	parsed, err := server.getHtmlTemplate(funcMap)
	if err != nil {
		t.Fatalf("getHtmlTemplate: %v", err)
	}

	for _, name := range []string{"inbounds.html", "api_docs.html", "clientsBulkModal", "clientsModal", "qrcodeModal"} {
		if parsed.Lookup(name) == nil {
			t.Errorf("template %q was not defined", name)
		}
	}
}

// The OpenAPI document is embedded at build time and read once at startup, so a
// wrong path would only show up as a panic on a real server.
func TestOpenAPIDocumentIsEmbedded(t *testing.T) {
	embedded, err := openAPIFS.ReadFile("api/openapi.json")
	if err != nil {
		t.Fatalf("the API description is not in the binary: %v", err)
	}
	if len(embedded) == 0 {
		t.Fatal("the embedded API description is empty")
	}

	var document map[string]interface{}
	if err := json.Unmarshal(embedded, &document); err != nil {
		t.Fatalf("the embedded API description is not valid JSON: %v", err)
	}
	if _, ok := document["paths"]; !ok {
		t.Fatal("the embedded API description has no paths")
	}

	onDisk, err := os.ReadFile("api/openapi.json")
	if err != nil {
		t.Fatalf("read the file on disk: %v", err)
	}
	if string(onDisk) != string(embedded) {
		t.Fatal("the embedded description differs from the file on disk")
	}
}

// The dark theme styles ant-design components and h2, but nothing else, so a
// bare <h1>, <h3>, <h4>… keeps the light theme's near-black colour and turns
// invisible on the dark background. Card titles and list-item metas are the
// panel's styled alternatives.
func TestPagesAvoidUnstyledHeadings(t *testing.T) {
	darkThemeStyles, err := fs.ReadFile(assetsFS, "assets/css/custom.css")
	if err != nil {
		t.Fatalf("read the theme stylesheet: %v", err)
	}

	unstyled := []string{}
	for _, tag := range []string{"h1", "h3", "h4", "h5", "h6"} {
		if !strings.Contains(string(darkThemeStyles), ".dark "+tag) {
			unstyled = append(unstyled, tag)
		}
	}
	if len(unstyled) == 0 {
		t.Skip("the dark theme now styles every heading level")
	}

	err = fs.WalkDir(htmlFS, "html", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}
		raw, err := fs.ReadFile(htmlFS, path)
		if err != nil {
			return err
		}
		for _, tag := range unstyled {
			if strings.Contains(string(raw), "<"+tag+">") || strings.Contains(string(raw), "<"+tag+" ") {
				t.Errorf("%s uses a bare <%s>, which the dark theme leaves near-black; "+
					"use a card title slot or a-list-item-meta instead", path, tag)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk templates: %v", err)
	}
}
