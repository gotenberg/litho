package fixture

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestOfficeLithoOptions_NamesWhatTheConversionApplies checks that an Office
// source names the litho-only keys its format takes and its options set away
// from their default, and no other.
func TestOfficeLithoOptions_NamesWhatTheConversionApplies(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		format  Format
		options string
		want    []string
	}{
		{"no options", FormatDOCX, `{}`, nil},
		{"route options only", FormatDOCX, `{"landscape": true, "nativePages": "1"}`, nil},
		{"tracked changes marked", FormatDOCX, `{"trackedChanges": "marked"}`, []string{"trackedChanges"}},
		{"tracked changes at the default", FormatODT, `{"trackedChanges": "accepted"}`, nil},
		{"tracked changes on a workbook", FormatXLSX, `{"trackedChanges": "rejected"}`, nil},
		{"sheet options", FormatXLSX, `{"sheets": ["Data"], "hiddenSheets": true, "recalculation": "never"}`, []string{"sheets", "hiddenSheets", "recalculation"}},
		{"sheet numbers", FormatODS, `{"sheetNumbers": [2]}`, []string{"sheetNumbers"}},
		{"sheet options at the default", FormatODS, `{"hiddenSheets": false, "recalculation": "asNeeded"}`, nil},
		{"sheet options on a CSV file", FormatCSV, `{"sheets": ["Data"]}`, nil},
		{"CSV separators", FormatCSV, `{"csvSeparators": ";"}`, []string{"csvSeparators"}},
		{"CSV separators at the default, reordered", FormatCSV, "{\"csvSeparators\": \"\\t;,\"}", nil},
		{"CSV type detection off", FormatCSV, `{"csvTypeDetection": false}`, []string{"csvTypeDetection"}},
		{"CSV type detection at the default", FormatCSV, `{"csvTypeDetection": true}`, nil},
		{"handouts", FormatPPTX, `{"handouts": 3}`, []string{"handouts"}},
		{"handouts at the default", FormatODP, `{"handouts": 0}`, nil},
		{"handouts on a text document", FormatDOCX, `{"handouts": 3}`, nil},
		{"date", FormatODT, `{"date": "2026-11-11T10:00:00Z"}`, []string{"date"}},
		{"date and tracked changes", FormatDOCX, `{"date": "2026-11-11T10:00:00Z", "trackedChanges": "rejected"}`, []string{"trackedChanges", "date"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts, err := ParseOfficeOptions([]byte(tc.options))
			if err != nil {
				t.Fatal(err)
			}
			o := &Office{Format: tc.format, Options: opts}
			if got := o.LithoOptions(); !slices.Equal(got, tc.want) {
				t.Errorf("LithoOptions() = %q, want %q", got, tc.want)
			}
			for _, key := range o.LithoOptions() {
				if !slices.Contains(OfficeLithoOptionNames, key) {
					t.Errorf("%q is not among OfficeLithoOptionNames", key)
				}
			}
		})
	}
}

// TestSourceLithoOptions_NamesWhatTheConversionApplies checks that an HTML
// or Markdown source names a user stylesheet, and a template only for a
// Markdown document, the one format that renders into it.
func TestSourceLithoOptions_NamesWhatTheConversionApplies(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		document string
		options  string
		want     []string
	}{
		{"no options", HTMLDocument, `{}`, nil},
		{"route options only", HTMLDocument, `{"landscape": true}`, nil},
		{"user stylesheets", HTMLDocument, `{"userStylesheets": ["user.css"]}`, []string{"userStylesheets"}},
		{"template of an HTML page", HTMLDocument, `{"markdownTemplate": "page.tmpl"}`, nil},
		{"template of a Markdown page", MarkdownDocument, `{"markdownTemplate": "page.tmpl", "userStylesheets": ["user.css"]}`, []string{"userStylesheets", "markdownTemplate"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			write(t, dir, tc.document, "<p>x</p>")
			write(t, dir, "user.css", "p { color: red }")
			write(t, dir, "page.tmpl", "{{ toHTML }}")
			write(t, dir, OptionsFile, tc.options)
			src, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if got := src.LithoOptions(); !slices.Equal(got, tc.want) {
				t.Errorf("LithoOptions() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestLoadReference_ReadsEitherKindOfSource checks that LoadReference reads
// the reference_of and the litho-only options of an HTML, a Markdown and an
// Office source directory and of a bare document, and fails as the loaders
// do.
func TestLoadReference_ReadsEitherKindOfSource(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dirs := map[string]map[string]string{
		"html":     {HTMLDocument: "<p>x</p>", "user.css": "p {}", OptionsFile: `{"userStylesheets": ["user.css"]}`},
		"markdown": {MarkdownDocument: "# x", OptionsFile: `{"landscape": true, "reference_of": "html"}`},
		"office":   {"memo.docx": "PK", OptionsFile: `{"trackedChanges": "marked", "reference_of": "memo-marked"}`},
		"csv":      {"table.csv": "a,b", OptionsFile: `{"csvSeparators": ";"}`},
		"plain":    {"memo.docx": "PK"},
		"broken":   {HTMLDocument: "<p>x</p>", OptionsFile: `{"paperSize": "A4"}`},
		"neither":  {"notes.txt": "x"},
	}
	for name, files := range dirs {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
		for file, content := range files {
			write(t, filepath.Join(root, name), file, content)
		}
	}
	cases := []struct {
		path       string
		want       Reference
		fails      bool
		referenced bool
	}{
		{"html", Reference{LithoOptions: []string{"userStylesheets"}}, false, false},
		{"markdown", Reference{Of: "html"}, false, true},
		{"office", Reference{Of: "memo-marked", LithoOptions: []string{"trackedChanges"}}, false, true},
		{"csv", Reference{LithoOptions: []string{"csvSeparators"}}, false, false},
		{"plain", Reference{}, false, true},
		{"plain/memo.docx", Reference{}, false, true},
		{"broken", Reference{}, true, false},
		{"neither", Reference{}, true, false},
	}
	for _, tc := range cases {
		got, err := LoadReference(filepath.Join(root, filepath.FromSlash(tc.path)))
		if (err != nil) != tc.fails {
			t.Errorf("%s: error %v, want failure %v", tc.path, err, tc.fails)
			continue
		}
		if got.Of != tc.want.Of || !slices.Equal(got.LithoOptions, tc.want.LithoOptions) {
			t.Errorf("%s: %+v, want %+v", tc.path, got, tc.want)
		}
		if !tc.fails && got.Unreferenced() == tc.referenced {
			t.Errorf("%s: Unreferenced() = %v", tc.path, got.Unreferenced())
		}
	}
}
