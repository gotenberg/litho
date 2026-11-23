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
	// borrows a reference, the one that installs fixture fonts, the one
	// that derives descriptions, the one that writes comments and the
	// locale: the last five are no fields of the pinned route's.
	want := append(slices.Clone(OfficeOptionNames), OfficeLithoOptionNames...)
	if want = append(want, ReferenceOfName, InstalledFontsName, DerivedAltTextName, CommentsName, LocaleName, ExpectedTextName); !slices.Equal(tags, want) {
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
// and fails only on the font directory, empty on purpose, or, for an image,
// which draws no text, on its empty source. A format litho does not read
// fails with ErrUnsupportedFormat.
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
		want := pdf.ErrConvertNoFonts
		if format == FormatImage {
			want = pdf.ErrConvertMalformed
		}
		if _, err := c.WriteTo(context.Background(), &bytes.Buffer{}); !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", ext, err, want)
		}
	}
}

func TestInstalledFonts_ValidatedAndCountedAsLithosOwnForMicrosoft(t *testing.T) {
	t.Parallel()

	for _, bad := range []string{`[""]`, `["../x"]`, `["a/b"]`, `["."]`, `["gotenberg"]`, `["a", "a"]`, `[" a"]`} {
		data := []byte(`{"installedFonts": ` + bad + `}`)
		if _, err := ParseOptions(data); err == nil {
			t.Errorf("html %s: accepted", bad)
		}
		if _, err := ParseOfficeOptions(data); err == nil {
			t.Errorf("office %s: accepted", bad)
		}
	}
	data := []byte(`{"installedFonts": ["roboto-flex", "source-sans-3"]}`)
	html, err := ParseOptions(data)
	if err != nil || !slices.Equal(html.InstalledFonts, []string{"roboto-flex", "source-sans-3"}) {
		t.Fatalf("html: %v, %v", html.InstalledFonts, err)
	}
	if got := InstalledFontDirs("root", html.InstalledFonts); !slices.Equal(got, []string{filepath.Join("root", "roboto-flex"), filepath.Join("root", "source-sans-3")}) {
		t.Errorf("InstalledFontDirs = %v", got)
	}
	office, err := ParseOfficeOptions(data)
	if err != nil {
		t.Fatal(err)
	}
	// LibreOffice installs the fonts, Office for the web cannot.
	for format, want := range map[Format][]string{
		FormatODT:  nil,
		FormatODS:  nil,
		FormatDOCX: {InstalledFontsName},
		FormatXLSX: {InstalledFontsName},
		FormatPPTX: {InstalledFontsName},
	} {
		o := &Office{Format: format, Options: office}
		if got := o.LithoOptions(); !slices.Equal(got, want) {
			t.Errorf("%s: LithoOptions = %v, want %v", format, got, want)
		}
	}

	dir := t.TempDir()
	write(t, dir, HTMLDocument, "<p>x</p>")
	write(t, dir, OptionsFile, string(data))
	ref, err := LoadReference(dir)
	if err != nil || !slices.Equal(ref.InstalledFonts, html.InstalledFonts) || ref.Unreferenced() {
		t.Errorf("LoadReference = %+v, %v", ref, err)
	}
}

// TestDerivedAltText_ConvertsWithTheOptionAndKeepsTheReference holds the
// derivedAltText key: an HTML or Office source that sets it converts with
// pdf.WithDerivedAltText, and, since the option draws nothing, counts as
// setting no litho option: it keeps a reference of its own and needs no
// twin, in every format.
func TestDerivedAltText_ConvertsWithTheOptionAndKeepsTheReference(t *testing.T) {
	t.Parallel()

	data := []byte(`{"derivedAltText": true}`)
	html, err := ParseOptions(data)
	if err != nil || !html.DerivedAltText {
		t.Fatalf("html: %+v, %v", html, err)
	}
	office, err := ParseOfficeOptions(data)
	if err != nil || !office.DerivedAltText {
		t.Fatalf("office: %+v, %v", office, err)
	}
	for _, format := range []Format{FormatDOCX, FormatODT, FormatXLSX, FormatODS, FormatPPTX, FormatODP} {
		o := &Office{Format: format, Options: office}
		if got := o.LithoOptions(); len(got) != 0 {
			t.Errorf("%s: LithoOptions = %v, want none", format, got)
		}
		if n, m := len(o.options().common), len((&Office{Format: format}).options().common); n != m+1 {
			t.Errorf("%s: %d common options, want the %d without the key and one more", format, n, m)
		}
	}
	dir := t.TempDir()
	write(t, dir, HTMLDocument, "<p>x</p>")
	write(t, dir, OptionsFile, string(data))
	src, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := src.LithoOptions(); len(got) != 0 {
		t.Errorf("html: LithoOptions = %v, want none", got)
	}
	if n, m := len(src.PrintOptions()), len((&Source{}).PrintOptions()); n != m+1 {
		t.Errorf("html: %d print options, want %d and one more", n, m)
	}
	ref, err := LoadReference(dir)
	if err != nil || ref.Unreferenced() {
		t.Errorf("LoadReference = %+v, %v; want a reference of its own", ref, err)
	}
}

// TestComments_ConvertsWithTheOptionAndKeepsTheReference holds the
// comments key: a DOCX or ODT source that sets it to annotations converts
// with pdf.WithComments, and, since the comments draw nothing, counts as
// setting no litho option: it keeps a reference of its own and needs no
// twin. Another format ignores it, and a value other than hidden or
// annotations is refused.
func TestComments_ConvertsWithTheOptionAndKeepsTheReference(t *testing.T) {
	t.Parallel()

	opts, err := ParseOfficeOptions([]byte(`{"comments": "annotations"}`))
	if err != nil || opts.Comments != "annotations" {
		t.Fatalf("options: %+v, %v", opts, err)
	}
	for _, format := range []Format{FormatDOCX, FormatODT, FormatXLSX, FormatPPTX} {
		o := &Office{Format: format, Options: opts}
		if got := o.LithoOptions(); len(got) != 0 {
			t.Errorf("%s: LithoOptions = %v, want none", format, got)
		}
		want := len((&Office{Format: format}).options().word)
		if format == FormatDOCX || format == FormatODT {
			want++
		}
		if n := len(o.options().word); n != want {
			t.Errorf("%s: %d word processing options, want %d", format, n, want)
		}
	}
	for _, v := range []string{`"hidden"`, `""`} {
		if _, err := ParseOfficeOptions([]byte(`{"comments": ` + v + `}`)); err != nil {
			t.Errorf("%s: %v", v, err)
		}
	}
	if _, err := ParseOfficeOptions([]byte(`{"comments": "margin"}`)); err == nil {
		t.Error("an unknown view is read")
	}
}

// TestSinglePage_TextDocumentsTakeItAsLithosOwn holds the singlePage key:
// a text document converts with pdf.WithSinglePage, its page ranges set
// aside, and counts it as litho's own option, since no engine prints a
// text document on one page; a spreadsheet takes it as the route's field.
func TestSinglePage_TextDocumentsTakeItAsLithosOwn(t *testing.T) {
	t.Parallel()
	opts, err := ParseOfficeOptions([]byte(`{"singlePage": true, "nativePages": "2"}`))
	if err != nil {
		t.Fatal(err)
	}
	for format, want := range map[Format][]string{
		FormatDOCX: {SinglePageName},
		FormatODT:  {SinglePageName},
		FormatXLSX: nil,
		FormatODS:  nil,
		FormatPPTX: nil,
	} {
		o := &Office{Format: format, Options: opts}
		if got := o.LithoOptions(); !slices.Equal(got, want) {
			t.Errorf("%s: LithoOptions = %v, want %v", format, got, want)
		}
		if isText(format) {
			c, plain := o.options(), (&Office{Format: format}).options()
			if len(c.word) != len(plain.word)+1 || len(c.common) != len(plain.common) {
				t.Errorf("%s: %d text options and %d common ones, want the single page and no page range", format, len(c.word), len(c.common))
			}
		}
	}
}

// TestLocale_ConvertsWithTheOptionAndBuildsTheReferenceUnderIt holds the
// locale key: a tag of the Locale constants' shape is read, any other
// refused; a sheet source converts with pdf.WithLocale; LibreOffice's
// references are built under it, so it counts as litho's own only where
// Microsoft's judge.
func TestLocale_ConvertsWithTheOptionAndBuildsTheReferenceUnderIt(t *testing.T) {
	t.Parallel()

	for _, bad := range []string{`"de_DE"`, `"DE-de"`, `"de-DE "`, `"x"`} {
		if _, err := ParseOfficeOptions([]byte(`{"locale": ` + bad + `}`)); err == nil {
			t.Errorf("%s: accepted", bad)
		}
	}
	for _, good := range []string{"de-DE", "sr-Latn-RS", "ca-ES-valencia", "eo"} {
		if _, err := ParseOfficeOptions([]byte(`{"locale": "` + good + `"}`)); err != nil {
			t.Errorf("%s: %v", good, err)
		}
	}
	office, err := ParseOfficeOptions([]byte(`{"locale": "de-DE"}`))
	if err != nil {
		t.Fatal(err)
	}
	for format, want := range map[Format][]string{
		FormatODS:  nil,
		FormatCSV:  nil,
		FormatXLSX: {LocaleName},
		FormatODT:  nil,
	} {
		o := &Office{Format: format, Options: office}
		if got := o.LithoOptions(); !slices.Equal(got, want) {
			t.Errorf("%s: LithoOptions = %v, want %v", format, got, want)
		}
	}
	if opts := (&Office{Format: FormatCSV, Options: office}).options(); len(opts.sheet) != 1 {
		t.Errorf("csv sheet options %d, want the locale", len(opts.sheet))
	}
	dir := t.TempDir()
	write(t, dir, "s.ods", "x")
	write(t, dir, OptionsFile, `{"locale": "de-DE"}`)
	if ref, err := LoadReference(dir); err != nil || ref.Locale != "de-DE" || ref.Unreferenced() {
		t.Errorf("LoadReference = %+v, %v", ref, err)
	}
}

// TestLoadOffice_ReadsTheExpectedText pins the expectedText key of an Office
// source as Load reads it for an HTML one: the file it names sits beside the
// document and is read whole; the key is no route field, no litho option
// and no reason to borrow or lack a reference; a name that is no text file
// of the directory, a missing or blank file, and any other file beside the
// document fail.
func TestLoadOffice_ReadsTheExpectedText(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write(t, dir, "deck.pptx", "pk")
	write(t, dir, "expected-text.txt", "Vertical it should be!\n")
	write(t, dir, OptionsFile, `{"expectedText": "expected-text.txt"}`)
	o, err := LoadOffice(dir)
	if err != nil {
		t.Fatal(err)
	}
	if o.Document != filepath.Join(dir, "deck.pptx") || o.Options.ExpectedText != "expected-text.txt" || o.ExpectedText != "Vertical it should be!\n" {
		t.Errorf("document %q, expected text %q from %q", o.Document, o.ExpectedText, o.Options.ExpectedText)
	}
	if keys := o.LithoOptions(); len(keys) != 0 {
		t.Errorf("litho options %q, want none", keys)
	}
	if n := optionCount(o); n != optionCount(&Office{Format: FormatPPTX}) {
		t.Errorf("%d conversion options, want those of a source without options", n)
	}
	ref, err := LoadReference(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Unreferenced() || ref.Of != "" {
		t.Errorf("reference %+v, want the source's own", ref)
	}

	for name, files := range map[string]map[string]string{
		"outside":  {OptionsFile: `{"expectedText": "../text.txt"}`},
		"options":  {OptionsFile: `{"expectedText": "options.json"}`},
		"document": {OptionsFile: `{"expectedText": "other.docx"}`, "other.docx": "pk"},
		"absent":   {OptionsFile: `{"expectedText": "absent.txt"}`},
		"blank":    {OptionsFile: `{"expectedText": "blank.txt"}`, "blank.txt": " \n"},
		"unnamed":  {"stray.txt": "text"},
	} {
		d := t.TempDir()
		write(t, d, "memo.docx", "pk")
		for n, c := range files {
			write(t, d, n, c)
		}
		if _, err := LoadOffice(d); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
}

// TestLoadImportedOffice_ReadsTheExpectedTextAlone pins the options a
// corpus keeps for a fetched document: they name its expected text and
// nothing else, so the document converts as its reference engine prints
// it, and the directory holds those two files alone.
func TestLoadImportedOffice_ReadsTheExpectedTextAlone(t *testing.T) {
	t.Parallel()

	docs := t.TempDir()
	write(t, docs, "tdf1.pptx", "pk")
	doc := filepath.Join(docs, "tdf1.pptx")
	opts := t.TempDir()
	write(t, opts, OptionsFile, `{"expectedText": "expected-text.txt"}`)
	write(t, opts, "expected-text.txt", "Hello!\n")
	o, err := LoadImportedOffice(doc, opts)
	if err != nil {
		t.Fatal(err)
	}
	if o.Document != doc || o.Format != FormatPPTX || o.ExpectedText != "Hello!\n" || optionCount(o) != optionCount(&Office{Format: FormatPPTX}) {
		t.Errorf("document %q, format %s, expected text %q, %d options", o.Document, o.Format, o.ExpectedText, optionCount(o))
	}
	for name, files := range map[string]map[string]string{
		"route field":  {OptionsFile: `{"expectedText": "t.txt", "landscape": true}`, "t.txt": "x"},
		"litho option": {OptionsFile: `{"expectedText": "t.txt", "date": "2026-01-01T00:00:00Z"}`, "t.txt": "x"},
		"stray file":   {OptionsFile: `{"expectedText": "t.txt"}`, "t.txt": "x", "notes.md": "x"},
		"no options":   {"t.txt": "x"},
		"blank":        {OptionsFile: `{"expectedText": "t.txt"}`, "t.txt": ""},
	} {
		d := t.TempDir()
		for n, c := range files {
			write(t, d, n, c)
		}
		if _, err := LoadImportedOffice(doc, d); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
	if _, err := LoadImportedOffice(docs, opts); err == nil {
		t.Error("a source directory loaded as an imported document")
	}
}
