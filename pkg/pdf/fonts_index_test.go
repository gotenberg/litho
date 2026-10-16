package pdf

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// TestWithFontFiles_EmbedsAFaceOfACollection pins that a face of a TrueType
// Collection is indexed and embedded as a program of its own.
func TestWithFontFiles_EmbedsAFaceOfACollection(t *testing.T) {
	t.Parallel()
	data := unembeddedFontsDoc("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold >>")
	fonts := fstest.MapFS{
		"LiberationSans.ttc": {Data: fontbuild.Collection(
			abProgram("LiberationSans", "Liberation Sans", "Regular", 667, 667),
			abProgram("LiberationSans-Bold", "Liberation Sans", "Bold", 722, 722),
		)},
	}
	report, after := remediateFonts(t, data, WithFontFiles(fonts))
	if after != 0 {
		t.Fatalf("%d fonts stay unembedded: %v", after, report.FontRefusals)
	}
	if !strings.Contains(strings.Join(report.Fixed, "\n"), "embedded LiberationSans-Bold") {
		t.Errorf("want the Bold face embedded, got %v", report.Fixed)
	}
}

func TestMatchFontFile_PrefersRegularAndBoldWeights(t *testing.T) {
	t.Parallel()
	files := []fontFile{
		{path: "light.ttc", face: 12, psName: "helveticalight", family: "helvetica", weight: 300},
		{path: "regular.ttc", face: 40, psName: "helveticaregular", family: "helvetica", weight: 400},
		{path: "black.ttc", face: 68, psName: "helveticablack", family: "helvetica", bold: true, weight: 900},
		{path: "bold.ttc", face: 96, psName: "helveticabold2", family: "helvetica", bold: true, weight: 700},
	}
	for _, tc := range []struct {
		base, want string
	}{
		{"Arial", "regular.ttc"},
		{"Arial,Bold", "bold.ttc"},
		{"Helvetica-Light", "light.ttc"},
	} {
		got := ""
		if f := matchFontFile(files, FontRequest{BaseFont: tc.base, Subtype: "TrueType"}); f != nil {
			got = f.path
		}
		if got != tc.want {
			t.Errorf("%s: matched %q, want %q", tc.base, got, tc.want)
		}
	}
}

// TestMatchFontFile_KeepsTheStyle pins that a face stands for a font only in
// the font's style: the width and the weight its name states, and the italic
// its descriptor's /Flags states. Each case is a name the govdocs corpus shows
// unembedded.
func TestMatchFontFile_KeepsTheStyle(t *testing.T) {
	t.Parallel()
	files := []fontFile{
		{path: "helvetica.ttc", psName: "helvetica", family: "helvetica", weight: 400},
		{path: "helveticabold.ttc", psName: "helveticabold", family: "helvetica", bold: true, weight: 700},
		{path: "arialnarrow.ttf", psName: "arialnarrow", family: "arialnarrow", weight: 400},
		{path: "futura.ttc", psName: "futuramedium", family: "futura", weight: 500},
		{path: "futurabold.ttc", psName: "futurabold", family: "futura", bold: true, weight: 700},
		{path: "futuracondensed.ttc", psName: "futuracondensedextrabold", family: "futuracondensed", bold: true, weight: 800},
		{path: "times.ttf", psName: "timesnewroman", family: "timesnewroman", weight: 400},
		{path: "timesitalic.ttf", psName: "timesnewromanpsitalic", family: "timesnewroman", italic: true, weight: 400},
		{path: "arialblack.ttf", psName: "arialblack", family: "arialblack", bold: true, weight: 900},
	}
	for _, tc := range []struct {
		req  FontRequest
		want string
	}{
		// A width word names another family, here a metric-compatible one.
		{FontRequest{BaseFont: "Helvetica-Narrow", Subtype: "Type1"}, "arialnarrow.ttf"},
		{FontRequest{BaseFont: "Futura-Bold", Subtype: "Type1"}, "futurabold.ttc"},
		{FontRequest{BaseFont: "Futura-CondensedBold", Subtype: "Type1"}, "futuracondensed.ttc"},
		{FontRequest{BaseFont: "Futura", Subtype: "Type1"}, "futura.ttc"},
		// A weight word the family has no face of matches no face.
		{FontRequest{BaseFont: "Helvetica-Black", Subtype: "Type1"}, ""},
		{FontRequest{BaseFont: "Futura-Light", Subtype: "Type1"}, ""},
		{FontRequest{BaseFont: "Futura-Book", Subtype: "Type1"}, ""},
		{FontRequest{BaseFont: "Futura-Heavy", Subtype: "Type1"}, ""},
		// The Italic flag asks for the italic face, and never removes the
		// italic a name states.
		{FontRequest{BaseFont: "TimesNewRoman", Subtype: "TrueType", Flags: fontFlagItalic}, "timesitalic.ttf"},
		{FontRequest{BaseFont: "TimesNewRoman,Italic", Subtype: "TrueType"}, "timesitalic.ttf"},
		{FontRequest{BaseFont: "TimesNewRoman", Subtype: "TrueType"}, "times.ttf"},
		// The PostScript name still matches whatever the weight.
		{FontRequest{BaseFont: "ArialBlack", Subtype: "TrueType"}, "arialblack.ttf"},
	} {
		got := ""
		if f := matchFontFile(files, tc.req); f != nil {
			got = f.path
		}
		if got != tc.want {
			t.Errorf("%s (flags %d): matched %q, want %q", tc.req.BaseFont, tc.req.Flags, got, tc.want)
		}
	}
}

func TestParseFontStyle(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		family string
		want   fontStyle
	}{
		{"Arial,BoldItalic", "arial", fontStyle{bold: true, italic: true}},
		{"TimesNewRomanPS-BoldMT", "timesnewroman", fontStyle{bold: true}},
		{"Times-Roman", "times", fontStyle{}},
		{"Helvetica-Narrow", "helveticanarrow", fontStyle{width: "narrow"}},
		{"ArialNarrow-Bold", "arialnarrow", fontStyle{bold: true}},
		{"Univers-CondensedBold", "universcondensed", fontStyle{bold: true, width: "condensed"}},
		{"Futura-CondensedLight", "futuracondensed", fontStyle{weight: 300, width: "condensed"}},
		{"StoneSerif-SemiboldItalic", "stoneserif", fontStyle{bold: true, italic: true, weight: 600}},
		{"AvantGarde-BookOblique", "avantgarde", fontStyle{italic: true, weight: 400}},
		{"Helvetica-Black", "helvetica", fontStyle{bold: true, weight: 900}},
		{"AmericanTypewriter-Medium", "americantypewriter", fontStyle{weight: 500}},
	} {
		family, style := splitFontName(tc.name)
		if family != tc.family || style != tc.want {
			t.Errorf("%s: got %q %+v, want %q %+v", tc.name, family, style, tc.family, tc.want)
		}
	}
}

// TestFontFiles_Bounds pins the index's handling of what it cannot or may not
// read: a file past the byte budget ends the index with the files before it,
// an unreadable file aborts a strict index and is skipped by a lenient one,
// and a file no reader accepts is skipped by both.
func TestFontFiles_Bounds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fsys := fstest.MapFS{
		"a.ttf":   {Data: abProgram("TestSans", "Test Sans", "Regular", 667, 667)},
		"b.ttf":   {Data: []byte("not a font program at all")},
		"c.ttc":   {Data: fontbuild.Collection(abProgram("TestSerif", "Test Serif", "Regular", 667, 667))},
		"d.ttf":   {Data: abProgram("TestMono", "Test Mono", "Regular", 600, 600)},
		"unread/": {Mode: fs.ModeDir},
	}
	files := func(lenient bool, fail string) *fontFiles {
		return &fontFiles{
			list: func(context.Context, *slog.Logger) ([]string, error) {
				return []string{"a.ttf", "b.ttf", "c.ttc", "d.ttf"}, nil
			},
			open: func(name string) (fs.File, error) {
				if name == fail {
					return nil, fs.ErrPermission
				}
				return fsys.Open(name)
			},
			lenient: lenient,
		}
	}

	got, err := files(false, "").build(ctx, slog.New(slog.DiscardHandler))
	if err != nil || len(got) != 3 || got[1].psName != "testserif" || got[1].face == 0 {
		t.Fatalf("index = %+v, %v, want three faces, the collection's at a non-zero offset", got, err)
	}

	if _, err := files(false, "c.ttc").build(ctx, slog.New(slog.DiscardHandler)); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("strict index over an unreadable file: err = %v, want fs.ErrPermission", err)
	}
	got, err = files(true, "c.ttc").build(ctx, slog.New(slog.DiscardHandler))
	if err != nil || len(got) != 2 {
		t.Errorf("lenient index over an unreadable file = %+v, %v, want the two others", got, err)
	}

	budget := int64(12)
	if _, err := files(false, "").describe("a.ttf", &budget); !errors.Is(err, errFontIndexBudget) {
		t.Errorf("describe past the budget: err = %v, want errFontIndexBudget", err)
	}
}

// TestFontFiles_CancelledIndexIsNotKept pins that an index a cancelled ctx
// stopped is built again by the next request.
func TestFontFiles_CancelledIndexIsNotKept(t *testing.T) {
	t.Parallel()
	f := newFontFiles(fstest.MapFS{"a.ttf": {Data: abProgram("TestSans", "Test Sans", "Regular", 667, 667)}})
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.FontProgram(cancelled, FontRequest{BaseFont: "TestSans"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("FontProgram under a cancelled ctx: err = %v, want context.Canceled", err)
	}
	program, err := f.FontProgram(context.Background(), FontRequest{BaseFont: "TestSans", Subtype: "TrueType"})
	if err != nil || program == nil {
		t.Errorf("FontProgram after a cancelled index = %d bytes, %v, want the program", len(program), err)
	}
}
