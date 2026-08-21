package web

import (
	"io/fs"
	"path"
	"sort"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

const referenceLocale = "translate.en_US.toml"

// flatten turns the nested TOML sections into the dotted keys the templates and
// the bot actually ask for, e.g. "pages.settings.telegramProxy".
func flatten(prefix string, node map[string]interface{}, into map[string]string) {
	for key, value := range node {
		full := key
		if prefix != "" {
			full = prefix + "." + key
		}
		switch typed := value.(type) {
		case map[string]interface{}:
			flatten(full, typed, into)
		case string:
			into[full] = typed
		}
	}
}

func loadTranslations(t *testing.T) map[string]map[string]string {
	t.Helper()
	loaded := make(map[string]map[string]string)

	err := fs.WalkDir(i18nFS, "translation", func(filePath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(filePath, ".toml") {
			return nil
		}
		raw, err := fs.ReadFile(i18nFS, filePath)
		if err != nil {
			t.Fatalf("read %s: %v", filePath, err)
		}
		var parsed map[string]interface{}
		if err := toml.Unmarshal(raw, &parsed); err != nil {
			t.Fatalf("%s is not valid TOML: %v", filePath, err)
		}
		keys := make(map[string]string)
		flatten("", parsed, keys)
		loaded[path.Base(filePath)] = keys
		return nil
	})
	if err != nil {
		t.Fatalf("walk translations: %v", err)
	}
	if len(loaded) == 0 {
		t.Fatal("no translation files were found")
	}
	if _, ok := loaded[referenceLocale]; !ok {
		t.Fatalf("the reference locale %s is missing", referenceLocale)
	}
	return loaded
}

func TestTranslationsParse(t *testing.T) {
	loaded := loadTranslations(t)
	for name, keys := range loaded {
		if len(keys) == 0 {
			t.Errorf("%s parsed to no keys at all", name)
		}
	}
}

// Every locale must define the same keys as en_US: a missing one renders as the
// raw key in the panel, and an extra one is almost always a typo.
func TestTranslationsHaveTheSameKeys(t *testing.T) {
	loaded := loadTranslations(t)
	reference := loaded[referenceLocale]

	for name, keys := range loaded {
		if name == referenceLocale {
			continue
		}
		var missing, extra []string
		for key := range reference {
			if _, ok := keys[key]; !ok {
				missing = append(missing, key)
			}
		}
		for key := range keys {
			if _, ok := reference[key]; !ok {
				extra = append(extra, key)
			}
		}
		sort.Strings(missing)
		sort.Strings(extra)
		if len(missing) > 0 {
			t.Errorf("%s is missing %d key(s) present in %s: %v", name, len(missing), referenceLocale, missing)
		}
		if len(extra) > 0 {
			t.Errorf("%s defines %d key(s) absent from %s: %v", name, len(extra), referenceLocale, extra)
		}
	}
}

// Guards the keys added for the new bot settings, so a locale cannot be
// forgotten when the feature ships.
func TestTelegramSettingKeysExist(t *testing.T) {
	loaded := loadTranslations(t)
	for name, keys := range loaded {
		for _, key := range []string{
			"pages.settings.telegramProxy",
			"pages.settings.telegramProxyDesc",
			"pages.settings.telegramNotifyOnly",
			"pages.settings.telegramNotifyOnlyDesc",
		} {
			value, ok := keys[key]
			if !ok {
				t.Errorf("%s has no %q", name, key)
				continue
			}
			if strings.TrimSpace(value) == "" {
				t.Errorf("%s has an empty %q", name, key)
			}
		}
	}
}
