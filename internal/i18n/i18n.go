// Package i18n owns application UI catalogs shared by Go and the renderer.
// It never translates user content, model output, protocol values or prompts.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"path"
	"regexp"
	"strings"
)

//go:embed locales/*/*.json
var files embed.FS

// Locale is a supported display language, independent of execution settings.
type Locale string

const (
	English Locale = "en"
	Chinese Locale = "zh-CN"
)

// ValidPreference accepts only persisted values understood by both surfaces.
func ValidPreference(value string) bool {
	return value == "system" || value == string(English) || value == string(Chinese)
}

// Resolve selects the first supported language in the OS preference list.
// Chinese variants currently use Simplified Chinese. Other languages fall back
// to English; this is a language fallback, never a timezone selection.
func Resolve(preference string, languages []string) Locale {
	if preference == string(English) || preference == string(Chinese) {
		return Locale(preference)
	}
	for _, value := range languages {
		base := strings.Split(strings.ToLower(strings.ReplaceAll(value, "_", "-")), "-")[0]
		if base == "zh" {
			return Chinese
		}
		if base == "en" {
			return English
		}
	}
	return English
}

type message struct {
	text  string
	forms map[string]string
}

var catalogs = load()
var placeholder = regexp.MustCompile(`\{([a-zA-Z][a-zA-Z0-9_]*)\}`)

func load() map[Locale]map[string]message {
	catalogs := make(map[Locale]map[string]message)
	names, _ := fs.Glob(files, "locales/*/*.json")
	for _, name := range names {
		parts := strings.Split(name, "/")
		locale := Locale(parts[1])
		namespace := strings.TrimSuffix(path.Base(name), ".json")
		data, err := files.ReadFile(name)
		if err != nil {
			panic(err)
		}
		var entries map[string]json.RawMessage
		if err := json.Unmarshal(data, &entries); err != nil {
			panic(err)
		}
		if catalogs[locale] == nil {
			catalogs[locale] = make(map[string]message)
		}
		for key, raw := range entries {
			var m message
			if err := json.Unmarshal(raw, &m.text); err != nil {
				if err := json.Unmarshal(raw, &m.forms); err != nil || m.forms["other"] == "" {
					panic("invalid i18n message: " + name + ":" + key)
				}
			}
			catalogs[locale][namespace+"."+key] = m
		}
	}
	return catalogs
}

// Text formats a UI key with named {parameters}. Missing keys fall back to
// English, then the key. Values are substituted once, never parsed as markup.
// Cardinal plurals use raw numeric count; en and zh-CN need one/other only.
func Text(locale Locale, key string, args map[string]any) string {
	m, ok := catalogs[locale][key]
	if !ok {
		m, ok = catalogs[English][key]
	}
	if !ok {
		return key
	}
	text := m.text
	if m.forms != nil {
		text = m.forms["other"]
		count, _ := json.Marshal(args["count"])
		var number float64
		numeric := json.Unmarshal(count, &number) == nil
		if locale != Chinese && numeric && math.Abs(number) == 1 && m.forms["one"] != "" {
			text = m.forms["one"]
		}
	}
	return placeholder.ReplaceAllStringFunc(text, func(token string) string {
		if value, ok := args[token[1:len(token)-1]]; ok {
			return fmt.Sprint(value)
		}
		return token
	})
}

// Namespace supplies translated strings for a native menu bridge. Returned maps
// are independent; callers must not turn catalog keys into execution authority.
func Namespace(locale Locale, namespace string) map[string]string {
	result := make(map[string]string)
	for key := range catalogs[English] {
		if suffix, ok := strings.CutPrefix(key, namespace+"."); ok {
			result[suffix] = Text(locale, key, nil)
		}
	}
	return result
}
