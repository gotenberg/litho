package fixture

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

func TestParseOfficeOptions_RefusesWhatTheRouteRefuses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, json, want string
	}{
		{"empty object", `{}`, ""},
		{"every field", `{"password": "foo", "landscape": true, "nativePages": "2-3", "nativeBookmarks": false, "paperWidth": 4, "paperHeight": 6, "singlePage": true, "exportHiddenSlides": true, "exportNotesPages": true}`, ""},
		{"former hidden slides name", `{"hiddenSlides": true}`, "unknown field"},
		{"former single page name", `{"singlePageSheets": true}`, "unknown field"},
		{"unknown field", `{"paperSize": "A4"}`, "unknown field"},
		{"another case", `{"Landscape": true}`, "unknown field"},
		{"trailing data", `{} {}`, "data after"},
		{"not an object", `[]`, "decoding"},
		{"width alone", `{"paperWidth": 4}`, "both or neither"},
		{"zero paper", `{"paperWidth": 0, "paperHeight": 6}`, "paperWidth"},
		{"negative paper", `{"paperWidth": 4, "paperHeight": -6}`, "paperHeight"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseOfficeOptions([]byte(tc.json))
			switch {
			case tc.want == "" && err != nil:
				t.Errorf("ParseOfficeOptions: %v", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Errorf("ParseOfficeOptions error %v, want one naming %q", err, tc.want)
			}
		})
	}
}

func TestLoadOffice_ReadsADirectoryOrADocument(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write(t, dir, "memo.fodt", "<office:document/>")
	write(t, dir, OptionsFile, `{"landscape": true, "paperWidth": 4, "paperHeight": 6, "password": "foo", "nativePages": "1"}`)
	o, err := LoadOffice(dir)
	if err != nil {
		t.Fatal(err)
	}
	if o.Format != pdf.FormatODT || o.Document != filepath.Join(dir, "memo.fodt") || !o.Options.Landscape {
		t.Errorf("source %+v", o)
	}
	if n := len(o.ConvertOptions()); n != 5 {
		t.Errorf("%d convert options, want landscape, outline, pages, password and paper", n)
	}

	doc := filepath.Join(t.TempDir(), "fetched.DOCX")
	if err := os.WriteFile(doc, []byte("PK"), 0o600); err != nil {
		t.Fatal(err)
	}
	o, err = LoadOffice(doc)
	if err != nil {
		t.Fatal(err)
	}
	if o.Format != pdf.FormatDOCX || o.Options != (OfficeOptions{}) || len(o.ConvertOptions()) != 2 {
		t.Errorf("fetched document %+v", o)
	}

	for name, files := range map[string]map[string]string{
		"no document":     {OptionsFile: `{}`},
		"two documents":   {"a.docx": "", "b.odt": ""},
		"another file":    {"a.docx": "", "notes.txt": ""},
		"another format":  {"a.doc": ""},
		"binary workbook": {"a.xls": ""},
		"binary deck":     {"a.ppt": ""},
		"invalid options": {"a.docx": "", OptionsFile: `{"paperHeight": 6}`},
	} {
		d := t.TempDir()
		for f, content := range files {
			write(t, d, f, content)
		}
		if _, err := LoadOffice(d); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
	if _, err := LoadOffice(filepath.Join(dir, OptionsFile)); !errors.Is(err, ErrNoOfficeDocument) {
		t.Errorf("options file as a document: %v", err)
	}
}

func TestLoadOffice_MapsEveryExtension(t *testing.T) {
	t.Parallel()

	cases := map[string]pdf.Format{
		"a.docx": pdf.FormatDOCX, "a.odt": pdf.FormatODT, "a.fodt": pdf.FormatODT,
		"a.xlsx": pdf.FormatXLSX, "a.XLSM": pdf.FormatXLSX, "a.xltx": pdf.FormatXLSX, "a.xltm": pdf.FormatXLSX,
		"a.ods": pdf.FormatODS, "a.fods": pdf.FormatODS, "a.ots": pdf.FormatODS, "a.csv": pdf.FormatCSV,
		"a.pptx": pdf.FormatPPTX, "a.PPTM": pdf.FormatPPTX, "a.potx": pdf.FormatPPTX, "a.ppsx": pdf.FormatPPTX,
		"a.odp": pdf.FormatODP, "a.fodp": pdf.FormatODP, "a.otp": pdf.FormatODP,
	}
	for name, want := range cases {
		dir := t.TempDir()
		write(t, dir, name, "")
		write(t, dir, OptionsFile, `{"singlePage": true}`)
		o, err := LoadOffice(dir)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if o.Format != want || !o.Options.SinglePage || len(o.ConvertOptions()) != 3 {
			t.Errorf("%s: format %s, options %+v, %d convert options, want %s with single page", name, o.Format, o.Options, len(o.ConvertOptions()), want)
		}
	}
}

// TestLibreOfficeScript_ReadsTheSameExtensions checks that lib/libreoffice.sh
// accepts a committed source of every extension this package maps, and no
// other.
func TestLibreOfficeScript_ReadsTheSameExtensions(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "lib", "libreoffice.sh"))
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(string(data), "\nEXTENSIONS='")
	list, _, ok2 := strings.Cut(rest, "'")
	if !ok || !ok2 {
		t.Fatal("libreoffice.sh declares no EXTENSIONS")
	}
	got := strings.Fields(list)
	var want []string
	for ext := range officeFormats {
		want = append(want, strings.TrimPrefix(ext, "."))
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("libreoffice.sh extensions %v, officeFormats %v", got, want)
	}
}

func TestOfficeOptionNames_MatchTheJSONNames(t *testing.T) {
	t.Parallel()

	var tags []string
	ty := reflect.TypeFor[OfficeOptions]()
	for i := range ty.NumField() {
		name, _, _ := strings.Cut(ty.Field(i).Tag.Get("json"), ",")
		tags = append(tags, name)
	}
	if !slices.Equal(tags, officeOptionNames) {
		t.Errorf("json names %v, officeOptionNames %v", tags, officeOptionNames)
	}
}

// TestLibreOfficeScript_ReadsTheSameSchema checks that lib/libreoffice.sh,
// which reads options.json with jq, accepts the keys this package accepts
// and posts each of them.
func TestLibreOfficeScript_ReadsTheSameSchema(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "lib", "libreoffice.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	_, rest, ok := strings.Cut(script, "\nKEYS='")
	keys, _, ok2 := strings.Cut(rest, "'")
	if !ok || !ok2 {
		t.Fatal("libreoffice.sh declares no KEYS")
	}
	var got []string
	if err := json.Unmarshal([]byte(keys), &got); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, officeOptionNames) {
		t.Errorf("libreoffice.sh keys %v, officeOptionNames %v", got, officeOptionNames)
	}
	if !strings.Contains(script, `for key in $(jq -r '.[]' <<< "$KEYS"); do`) {
		t.Error("libreoffice.sh does not post every key of KEYS")
	}
}

func TestOfficeConvertOptions_MapsThePresentationFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, json string
		want       int
	}{
		{"defaults", `{}`, 2},
		{"hidden slides", `{"exportHiddenSlides": true}`, 3},
		{"notes pages", `{"exportNotesPages": true}`, 3},
		{"both", `{"exportHiddenSlides": true, "exportNotesPages": true}`, 4},
		{"both off", `{"exportHiddenSlides": false, "exportNotesPages": false}`, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			write(t, dir, "deck.pptx", "")
			write(t, dir, OptionsFile, tc.json)
			o, err := LoadOffice(dir)
			if err != nil {
				t.Fatal(err)
			}
			if n := len(o.ConvertOptions()); n != tc.want {
				t.Errorf("%d convert options, want %d", n, tc.want)
			}
		})
	}
}
