// SPDX-License-Identifier: AGPL-3.0-or-later

// Package i18n translates user-facing text.
//
// Catalogs live in locales/<lang>.json and use the go-i18n v2 JSON format, so
// they can be handled by translation platforms (e.g. Weblate) and later
// loaded by github.com/nicksnyder/go-i18n without changes:
//
//	{
//	  "menu.quit": "Quit",
//	  "menu.xwayland": {"one": "XWayland ({{.Count}} window)", "other": "XWayland ({{.Count}} windows)"}
//	}
//
// English is the reference language: a message missing from the current
// language falls back to English, then to its ID.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"text/template"

	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"
)

// Default is the reference language.
const Default = "en"

//go:embed locales/*.json
var localesFS embed.FS

// message maps a CLDR plural form ("one", "other", …) to its text. A message
// without plural forms only has "other".
type message map[string]string

type catalog map[string]message

var (
	catalogs = mustLoad()

	mu      sync.RWMutex
	current = Default
	tmpls   = map[string]*template.Template{}
)

func mustLoad() map[string]catalog {
	out, err := load(localesFS)
	if err != nil {
		panic(err) // embedded catalogs are checked by the tests
	}
	return out
}

func load(fsys fs.FS) (map[string]catalog, error) {
	files, err := fs.Glob(fsys, "locales/*.json")
	if err != nil {
		return nil, err
	}
	out := map[string]catalog{}
	for _, f := range files {
		data, err := fs.ReadFile(fsys, f)
		if err != nil {
			return nil, err
		}
		cat, err := parse(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		out[strings.TrimSuffix(path.Base(f), ".json")] = cat
	}
	return out, nil
}

// parse reads a go-i18n v2 JSON catalog.
func parse(data []byte) (catalog, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	cat := catalog{}
	for id, v := range raw {
		var s string
		if json.Unmarshal(v, &s) == nil {
			cat[id] = message{"other": s}
			continue
		}
		var m map[string]string
		if err := json.Unmarshal(v, &m); err != nil {
			return nil, fmt.Errorf("message %q: %w", id, err)
		}
		delete(m, "description") // go-i18n metadata, not text
		delete(m, "hash")
		if m["other"] == "" {
			return nil, fmt.Errorf("message %q: missing \"other\" form", id)
		}
		cat[id] = message(m)
	}
	return cat, nil
}

// Available returns the supported languages, sorted.
func Available() []string {
	langs := make([]string, 0, len(catalogs))
	for l := range catalogs {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	return langs
}

// Name returns the name of a language in that language ("Français"), taken
// from its "language.name" message.
func Name(lang string) string {
	if m, ok := catalogs[lang]["language.name"]; ok {
		return m["other"]
	}
	return lang
}

// Language returns the current language.
func Language() string {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// SetLanguage selects the language. An empty value or "auto" detects it from
// the environment; an unsupported value falls back to the default. It returns
// the language actually used.
func SetLanguage(lang string) string {
	l := match(lang)
	if l == "" {
		l = Detect()
	}
	mu.Lock()
	current = l
	tmpls = map[string]*template.Template{}
	mu.Unlock()
	return l
}

// Detect picks the first supported language from LANGUAGE, LC_ALL,
// LC_MESSAGES then LANG, like gettext does.
func Detect() string {
	var candidates []string
	candidates = append(candidates, strings.Split(os.Getenv("LANGUAGE"), ":")...)
	for _, v := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		candidates = append(candidates, os.Getenv(v))
	}
	for _, c := range candidates {
		if l := match(c); l != "" {
			return l
		}
	}
	return Default
}

// match turns a locale such as "fr_FR.UTF-8@euro" into a supported language,
// or "" if none matches.
func match(locale string) string {
	if i := strings.IndexAny(locale, ".@"); i >= 0 {
		locale = locale[:i]
	}
	locale = strings.ReplaceAll(locale, "_", "-")
	if locale == "" || locale == "auto" || locale == "C" || locale == "POSIX" {
		return ""
	}
	if _, ok := catalogs[locale]; ok {
		return locale
	}
	base, _, _ := strings.Cut(locale, "-")
	base = strings.ToLower(base)
	if _, ok := catalogs[base]; ok {
		return base
	}
	return ""
}

// T returns the translated message id. Arguments are key/value pairs used by
// the {{.Key}} placeholders, e.g. T("error.read", "Path", p, "Err", err).
func T(id string, kv ...any) string {
	return render(id, "other", kv)
}

// N is T for messages with plural forms: count selects the form and is also
// available as {{.Count}}.
func N(id string, count int, kv ...any) string {
	return render(id, pluralForm(Language(), count), append(kv, "Count", count))
}

func pluralForm(lang string, count int) string {
	tag, err := language.Parse(lang)
	if err != nil {
		return "other"
	}
	n := count
	if n < 0 {
		n = -n
	}
	switch plural.Cardinal.MatchPlural(tag, n, 0, 0, 0, 0) {
	case plural.Zero:
		return "zero"
	case plural.One:
		return "one"
	case plural.Two:
		return "two"
	case plural.Few:
		return "few"
	case plural.Many:
		return "many"
	default:
		return "other"
	}
}

// lookup finds the text of a message, falling back to English then to id.
func lookup(lang, id, form string) (text, key string) {
	for _, l := range []string{lang, Default} {
		if m, ok := catalogs[l][id]; ok {
			if s, ok := m[form]; ok {
				return s, l + "/" + id + "/" + form
			}
			return m["other"], l + "/" + id + "/other"
		}
	}
	return id, ""
}

func render(id, form string, kv []any) string {
	text, key := lookup(Language(), id, form)
	if key == "" || !strings.Contains(text, "{{") {
		return text
	}

	mu.Lock()
	t, ok := tmpls[key]
	if !ok {
		var err error
		if t, err = template.New(key).Option("missingkey=zero").Parse(text); err != nil {
			t = nil
		}
		tmpls[key] = t
	}
	mu.Unlock()
	if t == nil {
		return text
	}

	data := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		if k, ok := kv[i].(string); ok {
			data[k] = kv[i+1]
		}
	}
	var b strings.Builder
	if err := t.Execute(&b, data); err != nil {
		return text
	}
	return b.String()
}

// Error is an error whose message is translated when displayed.
type Error struct {
	ID  string
	KV  []any // placeholder values
	Err error // wrapped error, available as {{.Err}}
}

// NewError returns a translatable error.
func NewError(id string, kv ...any) error { return &Error{ID: id, KV: kv} }

// Wrap returns a translatable error wrapping err.
func Wrap(err error, id string, kv ...any) error { return &Error{ID: id, KV: kv, Err: err} }

func (e *Error) Error() string {
	kv := e.KV
	if e.Err != nil {
		kv = append(kv[:len(kv):len(kv)], "Err", e.Err.Error())
	}
	return T(e.ID, kv...)
}

func (e *Error) Unwrap() error { return e.Err }

// Is lets errors.Is match two errors with the same message id.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.ID == e.ID && t.Err == nil && len(t.KV) == 0
}
