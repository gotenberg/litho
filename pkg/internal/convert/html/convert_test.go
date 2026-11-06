package html

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/text"
)

func TestOptions_RefusesOutOfRange(t *testing.T) {
	t.Parallel()
	for name, o := range map[string]Options{
		"negative width":  {PageWidth: -1, PageHeight: 10},
		"NaN height":      {PageWidth: 10, PageHeight: math.NaN()},
		"too wide":        {PageWidth: maxPageSide + 1, PageHeight: 10},
		"negative margin": {MarginLeft: -1},
		"infinite margin": {MarginTop: math.Inf(1)},
		"no width left":   {MarginLeft: 300, MarginRight: 300},
		"no height left":  {MarginTop: 500, MarginBottom: 500},
		"unknown media":   {Media: "tv"},
		"negative input":  {MaxInputBytes: -1},
		"input past cap":  {MaxInputBytes: limits.MaxConvertInputBytesCeiling + 1},
		"negative pages":  {MaxPages: -1},
		"pages past cap":  {MaxPages: limits.MaxConvertPagesCeiling + 1},
	} {
		if _, err := o.withDefaults(); !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("%s: %v, want ErrInvalidOptions", name, err)
		}
	}
	o, err := Options{}.withDefaults()
	if err != nil || o.PageWidth != DefaultPageWidth || o.Media != MediaPrint || o.MaxPages != DefaultMaxPages || o.MaxInputBytes != DefaultMaxInputBytes {
		t.Errorf("defaults %+v, %v", o, err)
	}
}

func TestConvert_RefusesBadInput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fonts := testFonts(t)
	if _, err := convert(ctx, strings.NewReader("<p>x"), nil, Options{}, stubLayout); !errors.Is(err, ErrInvalidOptions) {
		t.Errorf("nil fonts: %v", err)
	}
	empty, err := text.NewFonts(ctx, nil, []string{t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := convert(ctx, strings.NewReader("<p>x"), empty, Options{}, stubLayout); !errors.Is(err, text.ErrNoFonts) {
		t.Errorf("no faces: %v", err)
	}
	if _, err := convert(ctx, strings.NewReader("<p>xx"), fonts, Options{MaxInputBytes: 4}, stubLayout); !errors.Is(err, ErrInputTooLarge) {
		t.Errorf("past the input cap: %v", err)
	}
	if _, err := convert(ctx, strings.NewReader("<p>x"), fonts, Options{MaxInputBytes: 4}, stubLayout); err != nil {
		t.Errorf("at the input cap: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := convert(cancelled, strings.NewReader("<p>x"), fonts, Options{}, stubLayout); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
	tooMany := func(context.Context, *layout.Node, *text.Fonts, layout.Options) (*layout.Result, error) {
		return nil, fmt.Errorf("wrapped: %w", layout.ErrTooManyPages)
	}
	if _, err := convert(ctx, strings.NewReader("<p>x"), fonts, Options{}, tooMany); !errors.Is(err, ErrTooManyPages) {
		t.Errorf("too many pages: %v", err)
	}
}

func TestConvert_PassesTheOptionsToTheEngine(t *testing.T) {
	t.Parallel()
	var got layout.Options
	spy := func(ctx context.Context, root *layout.Node, f *text.Fonts, o layout.Options) (*layout.Result, error) {
		got = o
		return stubLayout(ctx, root, f, o)
	}
	o := Options{PageWidth: 500, PageHeight: 700, MarginTop: 1, MarginRight: 2, MarginBottom: 3, MarginLeft: 4, MaxPages: 7}
	if _, err := convert(context.Background(), strings.NewReader(`<html lang=nl><p>x`), testFonts(t), o, spy); err != nil {
		t.Fatal(err)
	}
	if got.Page != (layout.PageBox{Width: 500, Height: 700, Margin: [4]float64{1, 2, 3, 4}}) || got.Lang != "nl" || got.MaxPages != 7 || got.Image == nil || got.Logger == nil {
		t.Errorf("layout options %+v", got)
	}
}

func TestConvert_AssemblesMetadataWarningsAndOutline(t *testing.T) {
	t.Parallel()
	src := `<html lang="en-GB"><head><title>  The
 Title </title><meta name=Author content="Ada Lovelace"><meta name="description" content="About things">
<meta name=keywords content="a, b"><meta name=author content="Second"></head>
<body><p style="frobnicate: 1">x</p>
<h1 id=intro>Intro</h1><h2>Part <em>one</em></h2><h3>Detail</h3><h2 id=litho-heading-1>Part two</h2>
<h1>Next<br>line</h1><h4>Deep</h4><h2 style="display:none">Hidden</h2><h3></h3>
<img src="missing.png" alt="m"></body>`
	doc, err := convert(context.Background(), strings.NewReader(src), testFonts(t), Options{}, stubLayout)
	if err != nil {
		t.Fatal(err)
	}
	want := compose.Metadata{Title: "The Title", Author: "Ada Lovelace", Subject: "About things", Keywords: "a, b", Lang: "en-GB"}
	if doc.Metadata != want {
		t.Errorf("metadata %+v, want %+v", doc.Metadata, want)
	}
	if got := strings.Join(codes(doc.Warnings), ","); got != "unsupported-css-property,resource-refused,layout-stub" {
		t.Errorf("warning codes %s, want the cascade's, the converter's, then the layout's", got)
	}
	var lines []string
	var walkOutline func([]compose.Outline, string)
	walkOutline = func(os []compose.Outline, indent string) {
		for _, o := range os {
			lines = append(lines, indent+o.Title)
			walkOutline(o.Kids, indent+"  ")
		}
	}
	walkOutline(doc.Outline, "")
	wantOutline := "Intro\n  Part one\n    Detail\n  Part two\nNext line\n  Deep"
	if got := strings.Join(lines, "\n"); got != wantOutline {
		t.Errorf("outline\n%s\nwant\n%s", got, wantOutline)
	}
	if doc.Outline[0].Dest.Page != 1 {
		t.Errorf("outline destination %+v", doc.Outline[0].Dest)
	}
	// Each heading has its own anchor: the synthetic IDs skip the one the
	// document already carries.
	seen := map[compose.Dest]bool{}
	var collect func([]compose.Outline)
	collect = func(os []compose.Outline) {
		for _, o := range os {
			if seen[o.Dest] {
				t.Errorf("two headings share the destination %+v", o.Dest)
			}
			seen[o.Dest] = true
			collect(o.Kids)
		}
	}
	collect(doc.Outline)
}

func TestHeadingTitle_CapsItsLength(t *testing.T) {
	t.Parallel()
	n := &layout.Node{Kind: layout.Element, Children: []*layout.Node{{Kind: layout.Text, Text: strings.Repeat("word ", 1000)}}}
	if got := []rune(headingTitle(n)); len(got) > maxOutlineTitleRunes || len(got) < maxOutlineTitleRunes-5 {
		t.Errorf("title of %d characters", len(got))
	}
}

func TestConvert_IsDeterministic(t *testing.T) {
	t.Parallel()
	src := `<html lang=en><style>p { margin: 4pt } .b { font-weight: bold }</style>` +
		`<h1>Title</h1><p>Some <a href="#t">text</a> and <span class=b>more</span>.</p>` +
		`<ul><li>one<li>two</ul><table><tr><td>a<td>b</table><p id=t>target</p>`
	fonts := testFonts(t)
	var outs [][]byte
	for range 3 {
		doc, err := Convert(context.Background(), strings.NewReader(src), fonts, Options{})
		if err != nil {
			t.Fatalf("Convert: %v", err)
		}
		var b bytes.Buffer
		if err := compose.Write(context.Background(), doc, &b, compose.Options{}); err != nil {
			t.Fatalf("compose.Write: %v", err)
		}
		outs = append(outs, b.Bytes())
	}
	if !bytes.Equal(outs[0], outs[1]) || !bytes.Equal(outs[0], outs[2]) {
		t.Errorf("three conversions wrote different bytes")
	}
}
