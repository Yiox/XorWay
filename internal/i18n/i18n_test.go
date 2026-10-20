// SPDX-License-Identifier: AGPL-3.0-or-later

package i18n

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"text/template"
)

var placeholderRE = regexp.MustCompile(`{{\s*\.(\w+)\s*}}`)

func placeholders(m message) []string {
	set := map[string]bool{}
	for _, text := range m {
		for _, p := range placeholderRE.FindAllStringSubmatch(text, -1) {
			set[p[1]] = true
		}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Every language must have exactly the English keys, with the same
// placeholders and valid templates.
func TestCatalogsMatchEnglish(t *testing.T) {
	ref := catalogs[Default]
	if len(ref) == 0 {
		t.Fatal("empty English catalog")
	}
	for lang, cat := range catalogs {
		for id, m := range cat {
			if _, ok := ref[id]; !ok {
				t.Errorf("%s: key %q does not exist in %s", lang, id, Default)
			}
			for form, text := range m {
				if _, err := template.New("").Parse(text); err != nil {
					t.Errorf("%s: %s[%s]: invalid template: %v", lang, id, form, err)
				}
			}
			if got, want := placeholders(m), placeholders(ref[id]); strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("%s: %s: placeholders %v, want %v", lang, id, got, want)
			}
		}
		for id := range ref {
			if _, ok := cat[id]; !ok {
				t.Errorf("%s: missing key %q", lang, id)
			}
		}
	}
}

// Every message ID used in the code must exist in the English catalog.
func TestKeysUsedInCode(t *testing.T) {
	callRE := regexp.MustCompile(`i18n\.(?:T|N|NewError|Wrap)\((?:[^"()]*,\s*)?"([a-z0-9_.]+)"`)
	localRE := regexp.MustCompile(`\b(?:T|N|NewError)\("([a-z0-9_.]+)"`) // calls inside this package
	root := filepath.Join("..", "..")
	found := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		re := callRE
		if filepath.Dir(path) == "." {
			re = localRE
		}
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			found++
			if _, ok := catalogs[Default][m[1]]; !ok {
				t.Errorf("%s: unknown message %q", path, m[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found < 40 {
		t.Errorf("only %d message IDs found in the code: is the regexp still right?", found)
	}
}

func TestMatch(t *testing.T) {
	for in, want := range map[string]string{
		"fr_FR.UTF-8":      "fr",
		"fr_CA.UTF-8@euro": "fr",
		"en_US.UTF-8":      "en",
		"fr":               "fr",
		"FR":               "fr",
		"de_DE.UTF-8":      "",
		"C":                "",
		"POSIX":            "",
		"":                 "",
		"auto":             "",
	} {
		if got := match(in); got != want {
			t.Errorf("match(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDetect(t *testing.T) {
	for _, tc := range []struct {
		language, lcAll, lcMessages, lang, want string
	}{
		{"", "", "", "fr_FR.UTF-8", "fr"},
		{"", "", "", "de_DE.UTF-8", "en"},
		{"de:fr:en", "", "", "en_US.UTF-8", "fr"},
		{"", "en_US.UTF-8", "", "fr_FR.UTF-8", "en"},
		{"", "", "fr_FR.UTF-8", "en_US.UTF-8", "fr"},
		{"", "C", "", "", "en"},
	} {
		t.Setenv("LANGUAGE", tc.language)
		t.Setenv("LC_ALL", tc.lcAll)
		t.Setenv("LC_MESSAGES", tc.lcMessages)
		t.Setenv("LANG", tc.lang)
		if got := Detect(); got != tc.want {
			t.Errorf("Detect(%+v) = %q, want %q", tc, got, tc.want)
		}
	}
}

func TestTranslateAndPlural(t *testing.T) {
	defer SetLanguage(Default)

	SetLanguage("en")
	if got := T("menu.quit"); got != "Quit" {
		t.Errorf("en menu.quit = %q", got)
	}
	if got := T("menu.backend", "Name", "KWin"); got != "Backend: KWin" {
		t.Errorf("en menu.backend = %q", got)
	}
	// English: only 1 is singular.
	for n, want := range map[int]string{0: "0 XWayland windows", 1: "1 XWayland window", 2: "2 XWayland windows"} {
		if got := N("tooltip.xwayland_only", n); got != want {
			t.Errorf("en N(%d) = %q, want %q", n, got, want)
		}
	}

	SetLanguage("fr")
	if got := T("menu.quit"); got != "Quitter" {
		t.Errorf("fr menu.quit = %q", got)
	}
	// French: 0 and 1 are singular.
	for n, want := range map[int]string{0: "0 fenêtre XWayland", 1: "1 fenêtre XWayland", 2: "2 fenêtres XWayland"} {
		if got := N("tooltip.xwayland_only", n); got != want {
			t.Errorf("fr N(%d) = %q, want %q", n, got, want)
		}
	}

	if got := T("no.such.key"); got != "no.such.key" {
		t.Errorf("unknown key = %q, want the ID", got)
	}
}

func TestFallbackToEnglish(t *testing.T) {
	defer func() { delete(catalogs["fr"], "test.only_en"); delete(catalogs[Default], "test.only_en") }()
	catalogs[Default]["test.only_en"] = message{"other": "English only"}
	SetLanguage("fr")
	defer SetLanguage(Default)
	if got := T("test.only_en"); got != "English only" {
		t.Errorf("fallback = %q", got)
	}
}

func TestError(t *testing.T) {
	defer SetLanguage(Default)
	sentinel := NewError("error.already_running")
	err := Wrap(errors.New("boom"), "error.session_bus")

	SetLanguage("en")
	if got := err.Error(); got != "cannot connect to the session bus: boom" {
		t.Errorf("en = %q", got)
	}
	SetLanguage("fr")
	if got := err.Error(); got != "connexion au bus de session impossible : boom" {
		t.Errorf("fr = %q", got)
	}
	if !errors.Is(NewError("error.already_running", "Extra", 1), sentinel) {
		t.Error("errors.Is must match on the message ID")
	}
	if errors.Is(err, sentinel) {
		t.Error("errors.Is must not match a different ID")
	}
}

func TestSetLanguage(t *testing.T) {
	defer SetLanguage(Default)
	t.Setenv("LANGUAGE", "")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "fr_FR.UTF-8")
	if got := SetLanguage(""); got != "fr" {
		t.Errorf("auto = %q, want fr", got)
	}
	if got := SetLanguage("en"); got != "en" {
		t.Errorf("explicit = %q, want en", got)
	}
	if got := SetLanguage("klingon"); got != "fr" {
		t.Errorf("unsupported = %q, want the detected language", got)
	}
}

// Every catalog names its language: it is shown in the language submenu.
func TestName(t *testing.T) {
	for _, l := range Available() {
		if _, ok := catalogs[l]["language.name"]; !ok {
			t.Errorf("%s.json: missing \"language.name\"", l)
		}
	}
	if got := Name("fr"); got != "Français" {
		t.Errorf("Name(fr) = %q", got)
	}
	if got := Name("xx"); got != "xx" {
		t.Errorf("Name(xx) = %q, want the code", got)
	}
}
