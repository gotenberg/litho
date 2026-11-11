package pdf

import (
	"bytes"
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestConvert_ValidatesEveryOption checks that every option out of range,
// and every pair of options that exclude each other, fails the conversion
// with ErrInvalidConvertOptions before it reads the source or writes a
// byte.
func TestConvert_ValidatesEveryOption(t *testing.T) {
	t.Parallel()
	fonts := WithConvertFontDirs(convertFontDir(t))
	src := func() *strings.Reader { return strings.NewReader("x") }
	for name, c := range map[string]*Conversion{
		"paper side zero":           FromText(src(), fonts, WithPaper(Paper{Width: 0, Height: 10})),
		"paper side past the limit": FromODT(src(), fonts, WithPaper(Paper{Width: 20000, Height: 5})),
		"paper side NaN":            FromPPTX(src(), fonts, WithPaper(Paper{Width: math.NaN(), Height: 5})),
		"default paper":             FromHTML(src(), fonts, WithDefaultPaper(Paper{Width: -1, Height: 5})),
		"orientation":               FromText(src(), fonts, WithOrientation(Orientation(3))),
		"negative orientation":      FromXLSX(src(), fonts, WithOrientation(Orientation(-1))),
		"default language":          FromCSV(src(), fonts, WithDefaultLanguage("en_US")),
		"version":                   FromODP(src(), fonts, WithConvertVersion("1.3")),
		"negative margin":           FromText(src(), fonts, WithMargins(-1, 0, 0, 0)),
		"NaN margin":                FromHTML(src(), fonts, WithMargins(0, math.NaN(), 0, 0)),
		"no room across":            FromMarkdown(src(), fonts, WithMargins(0, 300, 0, 300)),
		"no room down":              FromText(src(), fonts, WithMargins(500, 0, 500, 0)),
		"zero font size":            FromText(src(), fonts, WithFontSize(0)),
		"font size past the limit":  FromText(src(), fonts, WithFontSize(1001)),
		"zero line height":          FromText(src(), fonts, WithLineHeight(0)),
		"line height past limit":    FromText(src(), fonts, WithLineHeight(101)),
		"NaN line height":           FromText(src(), fonts, WithLineHeight(math.NaN())),
		"zero tab width":            FromText(src(), fonts, WithTabWidth(0)),
		"tab width past the limit":  FromText(src(), fonts, WithTabWidth(65)),
		"media":                     FromHTML(src(), fonts, WithMediaType("tv")),
		"scale past the limit":      FromHTML(src(), fonts, WithScale(5)),
		"zero scale":                FromMarkdown(src(), fonts, WithScale(0)),
		"relative base URL":         FromHTML(src(), fonts, WithBaseURL("relative")),
		"media feature case":        FromHTML(src(), fonts, WithMediaFeatures(map[string]string{"Color": "x"})),
		"empty media feature":       FromHTML(src(), fonts, WithMediaFeatures(map[string]string{"": "x"})),
		"single page with ranges":   FromHTML(src(), fonts, WithSinglePage(true), WithPageRanges("1")),
		"empty Markdown template":   FromMarkdown(src(), fonts, WithMarkdownTemplate("")),
		"tracked changes view":      FromDOCX(src(), fonts, WithTrackedChanges(TrackedChanges(3))),
		"recalculation mode":        FromXLSX(src(), fonts, WithFormulaRecalculation(Recalculation(3))),
		"one page with a paper":     FromXLSX(src(), fonts, WithOnePagePerSheet(true), WithPaper(PaperLetter)),
		"one page with orientation": FromODS(src(), fonts, WithOnePagePerSheet(true), WithOrientation(OrientationLandscape)),
		"CSV one page with a paper": FromCSV(src(), fonts, WithOnePagePerSheet(true), WithPaper(PaperA4)),
		"sheets and numbers":        FromXLSX(src(), fonts, WithSheets("A"), WithSheetNumbers(1)),
		"no sheet name":             FromODS(src(), fonts, WithSheets()),
		"no sheet number":           FromXLSX(src(), fonts, WithSheetNumbers()),
		"sheet number zero":         FromXLSX(src(), fonts, WithSheetNumbers(0)),
		"no CSV separator":          FromCSV(src(), fonts, WithCSVSeparators()),
		"too many CSV separators":   FromCSV(src(), fonts, WithCSVSeparators(',', ';', '\t', '|', ':', '!', '#', '$', '%')),
		"quote as CSV separator":    FromCSV(src(), fonts, WithCSVSeparators('"')),
		"line feed as separator":    FromCSV(src(), fonts, WithCSVSeparators('\n')),
		"handouts per page":         FromPPTX(src(), fonts, WithHandouts(5)),
		"handouts with notes":       FromODP(src(), fonts, WithHandouts(2), WithNotesPages(true)),
	} {
		var out bytes.Buffer
		rep, err := c.WriteTo(context.Background(), &out)
		if !errors.Is(err, ErrInvalidConvertOptions) || rep != nil {
			t.Errorf("%s: %v, %+v, want ErrInvalidConvertOptions", name, err, rep)
		}
		if !IsInvalidInput(err) {
			t.Errorf("%s: IsInvalidInput(%v) = false", name, err)
		}
		if out.Len() != 0 {
			t.Errorf("%s: wrote %d bytes, want none", name, out.Len())
		}
	}
}

// TestConvert_ConversionRunsOnce checks that a conversion reads its source
// once: a second run, alone or in a pipeline, fails with
// ErrConversionReused and writes nothing.
func TestConvert_ConversionRunsOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c := FromText(strings.NewReader("A line"), WithConvertFontDirs(convertFontDir(t)))
	var out bytes.Buffer
	if _, err := c.WriteTo(ctx, &out); err != nil {
		t.Fatalf("first WriteTo: %v", err)
	}
	var again bytes.Buffer
	rep, err := c.WriteTo(ctx, &again)
	if !errors.Is(err, ErrConversionReused) || rep != nil || again.Len() != 0 {
		t.Errorf("second WriteTo: %v, %+v, %d bytes; want ErrConversionReused and nothing written", err, rep, again.Len())
	}
	if IsInvalidInput(err) {
		t.Errorf("IsInvalidInput(%v) = true, want false: a reuse is the caller's bug", err)
	}
	if _, err := NewPipeline().Convert(c).WriteTo(ctx, &again); !errors.Is(err, ErrConversionReused) {
		t.Errorf("a pipeline over a used conversion: %v, want ErrConversionReused", err)
	}
}

// TestConvert_RefusesANilSourceOrDestination checks the arguments a
// conversion cannot run without.
func TestConvert_RefusesANilSourceOrDestination(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var out bytes.Buffer
	if _, err := FromText(nil).WriteTo(ctx, &out); !errors.Is(err, ErrNilSource) {
		t.Errorf("nil source: %v, want ErrNilSource", err)
	}
	if _, err := FromText(strings.NewReader("A")).WriteTo(ctx, nil); !errors.Is(err, ErrInvalidConvertOptions) {
		t.Errorf("nil destination: %v, want ErrInvalidConvertOptions", err)
	}
	var c *Conversion
	if _, err := c.WriteTo(ctx, &out); !errors.Is(err, ErrInvalidConvertOptions) {
		t.Errorf("nil conversion: %v, want ErrInvalidConvertOptions", err)
	}
}

// TestConvert_LineHeightSetsTheBaselineAdvance checks that WithLineHeight
// sets the distance between the baselines of a text source, as a multiple
// of the font size, 1.15 by default.
func TestConvert_LineHeightSetsTheBaselineAdvance(t *testing.T) {
	t.Parallel()
	dir := convertFontDir(t)
	for _, tc := range []struct {
		opts    []TextOption
		advance float64
	}{
		{nil, 12 * 1.15},
		{[]TextOption{WithLineHeight(2)}, 24},
		{[]TextOption{WithLineHeight(1), WithFontSize(20)}, 20},
	} {
		data, _ := convertText(t, dir, "A\nB", tc.opts...)
		a, b := textElementAt(t, data, "A"), textElementAt(t, data, "B")
		if got := a.Y - b.Y; math.Abs(got-tc.advance) > 0.01 {
			t.Errorf("%d options: baselines %v apart, want %v", len(tc.opts), got, tc.advance)
		}
	}
}

// TestConvert_TabWidthSetsTheTabStops checks that WithTabWidth sets the
// distance between the tab stops of a text source, in characters of the
// font, 8 by default: the test face draws every character 6 points wide at
// 12 points.
func TestConvert_TabWidthSetsTheTabStops(t *testing.T) {
	t.Parallel()
	dir := convertFontDir(t)
	for _, tc := range []struct {
		opts    []TextOption
		columns float64
	}{
		{nil, 8},
		{[]TextOption{WithTabWidth(4)}, 4},
		{[]TextOption{WithTabWidth(1)}, 1},
	} {
		data, _ := convertText(t, dir, "\tB", tc.opts...)
		// The tab draws as spaces in the run that ends with B.
		e := textElementAt(t, data, "B")
		if got, want := e.X+e.Width-6, 56.693+6*tc.columns; math.Abs(got-want) > 0.01 {
			t.Errorf("tab width %v: B at x %v, want %v", tc.columns, got, want)
		}
	}
}

// textElementAt returns the first element of page 1 of data whose text,
// leading spaces aside, is text.
func textElementAt(t *testing.T, data []byte, text string) models.TextElement {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close() //nolint:errcheck // test reader
	elems, err := r.ExtractTextElements(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractTextElements: %v", err)
	}
	for _, e := range elems {
		if strings.TrimLeft(e.Text, " ") == text {
			return e
		}
	}
	t.Fatalf("no element draws %q in %+v", text, elems)
	return models.TextElement{}
}

// TestPipeline_ConvertOpensThePipeline checks that a Convert stage opens a
// pipeline on the PDF the conversion writes: a later Apply stage edits it,
// and the stage reports the conversion.
func TestPipeline_ConvertOpensThePipeline(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c := FromText(strings.NewReader("First page\fSecond page"), WithConvertFontDirs(convertFontDir(t)))
	var out bytes.Buffer
	res, err := NewPipeline().Convert(c).Apply(func(_ context.Context, w *Writer) error {
		w.SetMetadata(Metadata{Title: "Converted"})
		return nil
	}).WriteTo(ctx, &out)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if len(res.Stages) == 0 || res.Stages[0].Name != "convert" {
		t.Fatalf("stages %+v, want a convert stage first", res.Stages)
	}
	if rep := res.Stages[0].Conversion; rep == nil || rep.Pages != 2 {
		t.Errorf("StageReport.Conversion = %+v, want 2 pages", rep)
	}
	for _, s := range res.Stages[1:] {
		if s.Conversion != nil {
			t.Errorf("stage %q reports a conversion", s.Name)
		}
	}
	data := out.Bytes()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close() //nolint:errcheck // test reader
	if n, err := r.PageCount(ctx); err != nil || n != 2 {
		t.Errorf("PageCount = %d, %v, want 2", n, err)
	}
	if meta, err := r.Metadata(ctx); err != nil || meta["Title"] != "Converted" {
		t.Errorf("metadata %v, %v; want the title the Apply stage set", meta, err)
	}
	if got, err := r.ExtractText(ctx, 2); err != nil || !strings.Contains(got, "Second page") {
		t.Errorf("page 2 text %q, %v", got, err)
	}
}

// TestPipeline_ConvertRefusals checks that Convert must be the first stage,
// that a nil conversion is refused before any stage runs, and that a
// conversion's own error fails the stage.
func TestPipeline_ConvertRefusals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := convertFontDir(t)
	var src bytes.Buffer
	if _, err := FromText(strings.NewReader("A"), WithConvertFontDirs(dir)).WriteTo(ctx, &src); err != nil {
		t.Fatal(err)
	}
	r, err := Open(ctx, bytes.NewReader(src.Bytes()), int64(src.Len()))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close() //nolint:errcheck // test reader

	var out bytes.Buffer
	_, err = NewPipeline().Open(r).Convert(FromText(strings.NewReader("B"), WithConvertFontDirs(dir))).WriteTo(ctx, &out)
	var stageErr *PipelineStageError
	if !errors.Is(err, ErrPipelineNoSource) || !errors.As(err, &stageErr) || stageErr.Index != 1 || stageErr.Name != "convert" {
		t.Errorf("Convert after Open: %v, want ErrPipelineNoSource at stage 1", err)
	}
	if _, err := NewPipeline().Convert(nil).WriteTo(ctx, &out); !errors.Is(err, ErrInvalidConvertOptions) {
		t.Errorf("nil conversion: %v, want ErrInvalidConvertOptions", err)
	}
	if err := NewPipeline().Convert(nil).Validate(); !errors.Is(err, ErrInvalidConvertOptions) {
		t.Errorf("Validate with a nil conversion: %v, want ErrInvalidConvertOptions", err)
	}
	bad := FromText(strings.NewReader("A"), WithConvertFontDirs(dir), WithFontSize(0))
	if _, err := NewPipeline().Convert(bad).WriteTo(ctx, &out); !errors.Is(err, ErrInvalidConvertOptions) || !errors.As(err, &stageErr) || stageErr.Name != "convert" {
		t.Errorf("an invalid conversion: %v, want ErrInvalidConvertOptions from the convert stage", err)
	}
	if out.Len() != 0 {
		t.Errorf("%d bytes written by refused pipelines", out.Len())
	}
}
