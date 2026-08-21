package web

import (
	"html/template"
	"io/fs"
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

	for _, name := range []string{"inbounds.html", "clientsBulkModal", "clientsModal", "qrcodeModal"} {
		if parsed.Lookup(name) == nil {
			t.Errorf("template %q was not defined", name)
		}
	}
}
