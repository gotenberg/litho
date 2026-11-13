package fixture

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestParseOptions_RefusesWhatTheRouteRefuses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, json, want string
	}{
		{"empty object", `{}`, ""},
		{"every field", `{"paperWidth": 8.5, "paperHeight": 11, "marginTop": 0, "marginRight": 0.5, "marginBottom": 1, "marginLeft": 0.39,
			"landscape": true, "scale": 0.75, "singlePage": true, "nativePages": "2-3", "preferCssPageSize": false, "printBackground": false,
			"nativeBookmarks": true, "emulatedMediaType": "screen", "emulatedMediaFeatures": {"prefers-color-scheme": "dark"},
			"header": "header.html", "footer": "footer.html", "userStylesheets": ["u.css"], "markdownTemplate": "t.html",
			"bands": [{"edge": "header", "html": "h.html", "css": "h.css", "pages": ["first", "last", "left", "right", "blank", "2-3", "4-", "page:x"],
			"room": "content", "isolated": true}]}`, ""},
		{"template without bands", `{"header": "header.html"}`, "want the bands"},
		{"unknown field", `{"paperwidth": 8.5}`, "unknown field"},
		{"trailing data", `{} {}`, "data after"},
		{"not an object", `[]`, "decoding"},
		{"zero paper", `{"paperWidth": 0}`, "paperWidth"},
		{"negative margin", `{"marginLeft": -1}`, "marginLeft"},
		{"scale too small", `{"scale": 0.05}`, "scale"},
		{"scale too large", `{"scale": 2.5}`, "scale"},
		{"unknown media", `{"emulatedMediaType": "tv"}`, "emulatedMediaType"},
		{"template path", `{"header": "../header.html"}`, "template"},
		{"template dot dot", `{"footer": ".."}`, "template"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseOptions([]byte(tc.json))
			switch {
			case tc.want == "" && err != nil:
				t.Errorf("ParseOptions: %v", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Errorf("ParseOptions error %v, want one naming %q", err, tc.want)
			}
		})
	}
}

func TestLoad_DefaultsWithoutAnOptionsFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write(t, dir, HTMLDocument, "<p>x")
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Format != FormatHTML || s.Document != filepath.Join(dir, HTMLDocument) {
		t.Errorf("document %s in %s", s.Document, s.Format)
	}
	if w, h := s.Paper(); w != DefaultPaperWidth || h != DefaultPaperHeight {
		t.Errorf("paper %v by %v", w, h)
	}
	if top, r, b, l := s.Margins(); top != DefaultMargin || r != DefaultMargin || b != DefaultMargin || l != DefaultMargin {
		t.Errorf("margins %v %v %v %v", top, r, b, l)
	}
	if s.MediaType() != "print" || !s.PreferCSSPageSize() {
		t.Errorf("media %q, prefer CSS page size %v", s.MediaType(), s.PreferCSSPageSize())
	}
	if n := len(s.PrintOptions()); n != 3 {
		t.Errorf("%d print options, want the three fixed ones", n)
	}
}

func TestLoad_ReadsTheOptionsAndTemplates(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write(t, dir, MarkdownDocument, "# x")
	write(t, dir, "top.html", "<span class=title></span>")
	write(t, dir, "band.html", "<b>head</b>")
	write(t, dir, "band.css", "b { color: red }")
	write(t, dir, OptionsFile, `{"paperWidth": 8.5, "paperHeight": 11, "marginTop": 0.5, "scale": 0.5, "nativePages": "1",
		"preferCssPageSize": false, "emulatedMediaType": "screen", "emulatedMediaFeatures": {"prefers-color-scheme": "dark"}, "header": "top.html",
		"bands": [{"edge": "header", "html": "band.html", "css": "band.css", "pages": ["first", "3-", "page:x"], "room": "content", "isolated": true},
			{"edge": "footer", "pages": ["left"]}]}`)
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Format != FormatMarkdown {
		t.Errorf("format %s", s.Format)
	}
	if w, h := s.Paper(); w != 612 || h != 792 {
		t.Errorf("paper %v by %v", w, h)
	}
	if top, _, _, l := s.Margins(); top != 36 || l != DefaultMargin {
		t.Errorf("margins top %v left %v", top, l)
	}
	if s.MediaType() != "screen" || s.PreferCSSPageSize() {
		t.Errorf("media %q, prefer CSS page size %v", s.MediaType(), s.PreferCSSPageSize())
	}
	if len(s.Bands) != 2 {
		t.Fatalf("%d bands, want 2", len(s.Bands))
	}
	if b := s.Bands[0]; b.Footer || b.Band.HTML != "<b>head</b>" || b.Band.CSS != "b { color: red }" || !b.Band.GrowMargin || !b.Band.IgnoreDocumentStyles {
		t.Errorf("header band %+v", b)
	}
	if b := s.Bands[1]; !b.Footer || b.Band.HTML != "" {
		t.Errorf("footer band %+v", b)
	}
	if n := len(s.PrintOptions()); n != 8 {
		t.Errorf("%d print options, want 8", n)
	}
	// The Chromium template stands for the bands.
	if keys := s.LithoOptions(); len(keys) != 0 {
		t.Errorf("litho options %q, want none", keys)
	}
}

func TestLoad_Refuses(t *testing.T) {
	t.Parallel()

	empty := t.TempDir()
	if _, err := Load(empty); !errors.Is(err, ErrNoDocument) {
		t.Errorf("no document: %v", err)
	}
	missing := t.TempDir()
	write(t, missing, HTMLDocument, "<p>")
	write(t, missing, OptionsFile, `{"footer": "absent.html", "bands": [{"edge": "footer"}]}`)
	if _, err := Load(missing); err == nil {
		t.Error("a missing template loaded")
	}
	for _, opts := range []string{
		`{"header": "h.html"}`,
		`{"bands": [{"edge": "side"}]}`,
		`{"bands": [{"edge": "header", "room": "page"}]}`,
		`{"bands": [{"edge": "header", "pages": ["0-2"]}]}`,
		`{"bands": [{"edge": "header", "pages": ["3-2"]}]}`,
		`{"bands": [{"edge": "header", "pages": ["page:"]}]}`,
		`{"bands": [{"edge": "header", "isolate": true}]}`,
		`{"bands": [{"edge": "header", "html": "../h.html"}]}`,
	} {
		dir := t.TempDir()
		write(t, dir, HTMLDocument, "<p>")
		write(t, dir, "h.html", "<p>")
		write(t, dir, OptionsFile, opts)
		if _, err := Load(dir); err == nil {
			t.Errorf("%s loaded", opts)
		}
	}
	big := t.TempDir()
	write(t, big, HTMLDocument, "<p>")
	write(t, big, "f.html", strings.Repeat("x", maxTemplateBytes+1))
	write(t, big, OptionsFile, `{"footer": "f.html", "bands": [{"edge": "footer"}]}`)
	if _, err := Load(big); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("an oversized template: %v", err)
	}
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestOptionNames_AreTheJSONNamesOfOptions(t *testing.T) {
	t.Parallel()

	typ := reflect.TypeFor[Options]()
	var tags []string
	for field := range typ.Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		tags = append(tags, name)
	}
	// The route's fields, then litho's own options, then the key that
	// borrows a reference and the one that installs fixture fonts: the
	// last three are no fields of the route's.
	want := append(slices.Clone(OptionNames), LithoOptionNames...)
	if want = append(want, ReferenceOfName, InstalledFontsName); !slices.Equal(tags, want) {
		t.Errorf("json names %v, want %v", tags, want)
	}
}

// TestSources_LoadAndReadThroughDirFS loads every committed source and reads
// each of its files through os.DirFS, the file system the converter resolves
// relative URLs in: a symbolic link, such as font-face's ahem.ttf, must
// resolve to a file.
func TestSources_LoadAndReadThroughDirFS(t *testing.T) {
	t.Parallel()

	var dirs []string
	root := filepath.Join("..", "corpus", "litho", "html", "sources")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			t.Errorf("%s is not a source directory", filepath.Join(root, e.Name()))
			continue
		}
		dirs = append(dirs, filepath.Join(root, e.Name()))
	}
	for _, dir := range dirs {
		if _, err := Load(dir); err != nil {
			t.Errorf("%s: %v", dir, err)
		}
		fsys := os.DirFS(dir)
		entries, err := fs.ReadDir(fsys, ".")
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if data, err := fs.ReadFile(fsys, e.Name()); err != nil || len(data) == 0 {
				t.Errorf("%s/%s reads %d bytes: %v", dir, e.Name(), len(data), err)
			}
		}
	}
}

func TestChromiumMargin_MeasuredOnChromium(t *testing.T) {
	// The content box offsets Chromium's PDFs show for each margin.
	tests := []struct{ inches, points float64 }{
		{0, 0},
		{0.0104, 0},
		{0.1, 6.75},
		{0.2, 14.25},
		{0.3, 21},
		{0.35, 24.75},
		{0.37, 26.25},
		{0.39, DefaultMargin},
		{0.42917, 30},
		{0.5, 36},
		{0.6, 42.75},
	}
	for _, tt := range tests {
		if got := chromiumMargin(tt.inches); got != tt.points {
			t.Errorf("chromiumMargin(%v) = %v, want %v", tt.inches, got, tt.points)
		}
	}
}
