package web

import (
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	inlineScript  = regexp.MustCompile(`(?s)<script(?:\s[^>]*)?>(.*?)</script>`)
	srcAttribute  = regexp.MustCompile(`(?s)<script[^>]*\ssrc=`)
	bindingStart  = regexp.MustCompile(`^(?:const|let|var)\s+([A-Za-z_$][\w$]*)`)
	defineBlock   = regexp.MustCompile(`(?s)\{\{\s*define\s+"([^"]+)"\s*\}\}(.*?)\{\{\s*end\s*\}\}`)
	includeInPage = regexp.MustCompile(`\{\{\s*template\s+"([^"]+)"`)
	identifierUse = regexp.MustCompile(`[A-Za-z_$][\w$]*`)
)

// topLevelBindings returns the names a script introduces into the page's shared
// scope. Only bindings outside every brace count: a `const msg` inside a
// function is private, and treating it as a global produced false alarms on
// half the panel.
func topLevelBindings(script string) []string {
	var names []string
	depth := 0
	runes := []rune(script)

	for i := 0; i < len(runes); i++ {
		switch runes[i] {
		case '/':
			if i+1 < len(runes) && runes[i+1] == '/' {
				for i < len(runes) && runes[i] != '\n' {
					i++
				}
				continue
			}
			if i+1 < len(runes) && runes[i+1] == '*' {
				i += 2
				for i+1 < len(runes) && !(runes[i] == '*' && runes[i+1] == '/') {
					i++
				}
				i++
				continue
			}
		case '\'', '"', '`':
			quote := runes[i]
			i++
			for i < len(runes) && runes[i] != quote {
				if runes[i] == '\\' {
					i++
				}
				i++
			}
			continue
		case '{':
			depth++
			continue
		case '}':
			if depth > 0 {
				depth--
			}
			continue
		}

		if depth != 0 {
			continue
		}
		// A declaration only starts at a word boundary.
		if i > 0 {
			previous := runes[i-1]
			if previous == '_' || previous == '$' || previous >= 'a' && previous <= 'z' ||
				previous >= 'A' && previous <= 'Z' || previous >= '0' && previous <= '9' {
				continue
			}
		}
		if match := bindingStart.FindStringSubmatch(string(runes[i:min(i+40, len(runes))])); match != nil {
			names = append(names, match[1])
			i += len(match[0]) - 1
		}
	}
	return names
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// inlineScriptsOf returns the page's own script bodies, skipping <script src=…>.
func inlineScriptsOf(t *testing.T, path string) []string {
	t.Helper()
	raw, err := fs.ReadFile(htmlFS, path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var bodies []string
	for _, match := range inlineScript.FindAllStringSubmatch(string(raw), -1) {
		if srcAttribute.MatchString(match[0]) {
			continue
		}
		bodies = append(bodies, match[1])
	}
	return bodies
}

func globalsOf(t *testing.T, path string) []string {
	t.Helper()
	var names []string
	for _, script := range inlineScriptsOf(t, path) {
		names = append(names, topLevelBindings(script)...)
	}
	return names
}

// eachPage visits every template that is rendered on its own, which is the only
// place a script error can blank the screen.
func eachPage(t *testing.T, visit func(path, contents string)) {
	t.Helper()
	err := fs.WalkDir(htmlFS, "html", func(path string, entry fs.DirEntry, err error) error {
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
		if !strings.Contains(string(raw), `{{template "js"`) {
			return nil
		}
		visit(path, string(raw))
		return nil
	})
	if err != nil {
		t.Fatalf("walk templates: %v", err)
	}
}

// Every page pulls in common/js.html, so a page that declares a name that file
// already binds produces "Identifier has already been declared" and renders a
// blank screen. The template still parses, so this is the only place it can be
// caught.
func TestPagesDoNotRedeclareSharedGlobals(t *testing.T) {
	shared := make(map[string]string)
	for _, name := range globalsOf(t, "html/common/js.html") {
		shared[name] = "html/common/js.html"
	}
	if len(shared) == 0 {
		t.Fatal("no shared globals were found; the scan is not working")
	}

	eachPage(t, func(path, _ string) {
		for _, name := range globalsOf(t, path) {
			if origin, clash := shared[name]; clash {
				t.Errorf("%s redeclares %q, which %s already binds; the page will fail to load",
					path, name, origin)
			}
		}
	})
}

// The same collision can happen between two inline blocks of one page.
func TestPagesDoNotRedeclareTheirOwnGlobals(t *testing.T) {
	eachPage(t, func(path, _ string) {
		seen := make(map[string]int)
		for _, name := range globalsOf(t, path) {
			seen[name]++
		}
		var repeated []string
		for name, count := range seen {
			if count > 1 {
				repeated = append(repeated, fmt.Sprintf("%s (%d times)", name, count))
			}
		}
		sort.Strings(repeated)
		if len(repeated) > 0 {
			t.Errorf("%s declares the same global more than once: %s", path, strings.Join(repeated, ", "))
		}
	})
}

// componentGlobals maps a global defined inside a {{define}} block to the
// template that defines it, e.g. themeSwitcher -> component/themeSwitcher.
func componentGlobals(t *testing.T) map[string]string {
	t.Helper()
	owners := make(map[string]string)

	err := fs.WalkDir(htmlFS, "html", func(path string, entry fs.DirEntry, err error) error {
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
		for _, block := range defineBlock.FindAllStringSubmatch(string(raw), -1) {
			name, contents := block[1], block[2]
			for _, script := range inlineScript.FindAllStringSubmatch(contents, -1) {
				if srcAttribute.MatchString(script[0]) {
					continue
				}
				for _, global := range topLevelBindings(script[1]) {
					owners[global] = name
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk templates: %v", err)
	}
	return owners
}

// A page that uses a component's global without including that component gets a
// ReferenceError and renders blank, which is how the API reference page first
// shipped: it read themeSwitcher without pulling in component/themeSwitcher.
func TestPagesIncludeTheComponentsTheyUse(t *testing.T) {
	owners := componentGlobals(t)
	if _, ok := owners["themeSwitcher"]; !ok {
		t.Fatal("themeSwitcher was not detected as a component global; the scan is not working")
	}

	eachPage(t, func(path, contents string) {
		included := make(map[string]bool)
		for _, match := range includeInPage.FindAllStringSubmatch(contents, -1) {
			included[match[1]] = true
		}
		declared := make(map[string]bool)
		for _, name := range globalsOf(t, path) {
			declared[name] = true
		}

		used := make(map[string]bool)
		for _, script := range inlineScriptsOf(t, path) {
			for _, identifier := range identifierUse.FindAllString(script, -1) {
				used[identifier] = true
			}
		}

		var missing []string
		for name, owner := range owners {
			if !used[name] || declared[name] || included[owner] {
				continue
			}
			missing = append(missing, fmt.Sprintf("%s (defined by %s)", name, owner))
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			t.Errorf("%s uses globals whose component it never includes: %s",
				path, strings.Join(missing, ", "))
		}
	})
}
