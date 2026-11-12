package fixture

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// optionCount counts the litho options of o's fields that its format takes.
func optionCount(o *Office) int {
	opts := o.options()
	n := len(opts.common) + len(opts.sheet) + len(opts.slides)
	if o.Format != FormatCSV {
		n += len(opts.office)
	}
	return n
}

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
		{"borrowed reference", `{"landscape": true, "reference_of": "page-landscape"}`, ""},
		{"borrowed reference padded", `{"reference_of": " page-landscape"}`, "want a fixture ID"},
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
	if o.Format != FormatODT || o.Document != filepath.Join(dir, "memo.fodt") || !o.Options.Landscape {
		t.Errorf("source %+v", o)
	}
	// An OpenDocument text takes no default paper: LibreOffice's fallback
	// is litho's own.
	if n := optionCount(o); n != 5 {
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
	if o.Format != FormatDOCX || !reflect.DeepEqual(o.Options, OfficeOptions{}) || optionCount(o) != 2 {
		t.Errorf("fetched document %+v", o)
	}

	for ext, want := range map[string]Format{".doc": "doc", ".xls": "xls", ".ppt": "ppt", ".dotx": FormatDOCX} {
		bin := filepath.Join(t.TempDir(), "fetched"+ext)
		if err := os.WriteFile(bin, []byte{0xd0, 0xcf}, 0o600); err != nil {
			t.Fatal(err)
		}
		if o, err := LoadOffice(bin); err != nil || o.Format != want {
			t.Errorf("%s document: %+v, %v", ext, o, err)
		}
	}

	for name, files := range map[string]map[string]string{
		"no document":     {OptionsFile: `{}`},
		"two documents":   {"a.docx": "", "b.odt": ""},
		"another file":    {"a.docx": "", "notes.txt": ""},
		"another format":  {"a.rtf": ""},
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

	cases := map[string]Format{
		"a.docx": FormatDOCX, "a.odt": FormatODT, "a.fodt": FormatODT,
		"a.xlsx": FormatXLSX, "a.XLSM": FormatXLSX, "a.xltx": FormatXLSX, "a.xltm": FormatXLSX,
		"a.ods": FormatODS, "a.fods": FormatODS, "a.ots": FormatODS, "a.csv": FormatCSV,
		"a.pptx": FormatPPTX, "a.PPTM": FormatPPTX, "a.potx": FormatPPTX, "a.ppsx": FormatPPTX,
		"a.odp": FormatODP, "a.fodp": FormatODP, "a.otp": FormatODP,
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
		// A workbook takes the outline, the default paper of Microsoft's
		// references and one page per sheet; a text document no single
		// page; a presentation no default paper either: its slides carry
		// their size. A format LibreOffice judges takes no default paper:
		// LibreOffice's fallback is litho's own.
		n := 3
		switch want {
		case FormatDOCX, FormatODS, FormatCSV:
			n = 2
		case FormatODT, FormatPPTX, FormatODP:
			n = 1
		}
		if o.Format != want || !o.Options.SinglePage || optionCount(o) != n {
			t.Errorf("%s: format %s, options %+v, %d convert options, want %s with single page", name, o.Format, o.Options, optionCount(o), want)
		}
	}
}

func TestOfficeOptionNames_MatchTheJSONNames(t *testing.T) {
	t.Parallel()

	var tags []string
	ty := reflect.TypeFor[OfficeOptions]()
	for field := range ty.Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		tags = append(tags, name)
	}
	// The route's fields, then litho's own options, then the key that
	// borrows a reference: the last two are no fields of the route's.
	want := append(slices.Clone(OfficeOptionNames), OfficeLithoOptionNames...)
	if want = append(want, ReferenceOfName); !slices.Equal(tags, want) {
		t.Errorf("json names %v, want %v", tags, want)
	}
}

func TestOfficeConversion_MapsThePresentationFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, json string
		want       int
	}{
		{"defaults", `{}`, 1},
		{"hidden slides", `{"exportHiddenSlides": true}`, 2},
		{"notes pages", `{"exportNotesPages": true}`, 2},
		{"both", `{"exportHiddenSlides": true, "exportNotesPages": true}`, 3},
		{"both off", `{"exportHiddenSlides": false, "exportNotesPages": false}`, 1},
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
			if n := optionCount(o); n != tc.want {
				t.Errorf("%d convert options, want %d", n, tc.want)
			}
		})
	}
}

// TestOfficeConversion_TakesEveryField checks that the options of every
// field hold together for every format: the conversion passes validation
// and fails only on the font directory, empty on purpose. A format litho
// does not read fails with ErrUnsupportedFormat.
func TestOfficeConversion_TakesEveryField(t *testing.T) {
	t.Parallel()

	opts, err := ParseOfficeOptions([]byte(`{"password": "foo", "landscape": true, "nativePages": "2-3", "nativeBookmarks": false, "paperWidth": 4, "paperHeight": 6, "singlePage": true, "exportHiddenSlides": true, "exportNotesPages": true}`))
	if err != nil {
		t.Fatal(err)
	}
	for ext, format := range officeFormats {
		o := &Office{Format: format, Options: opts}
		c, err := o.Conversion(strings.NewReader(""), pdf.WithConvertFontDirs(t.TempDir()))
		if format == "doc" || format == "xls" || format == "ppt" {
			if !errors.Is(err, ErrUnsupportedFormat) {
				t.Errorf("%s: %v, want ErrUnsupportedFormat", ext, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", ext, err)
		}
		if _, err := c.WriteTo(context.Background(), &bytes.Buffer{}); !errors.Is(err, pdf.ErrConvertNoFonts) {
			t.Errorf("%s: %v, want ErrConvertNoFonts", ext, err)
		}
	}
}
