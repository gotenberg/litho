package pdf

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"strings"
	"testing"
	"testing/fstest"
)

// TestConvert_MemoryBudget pins [Limits.MaxConvertMemoryBytes] through the
// public API: a conversion the lowered budget cannot hold fails with
// [ErrConvertInputTooLarge], names what it was holding, and writes
// nothing; the same source converts under the default budget.
func TestConvert_MemoryBudget(t *testing.T) {
	t.Parallel()
	dir := convertFontDir(t)
	// One source line is one paragraph, shaped whole: its shaping state is
	// held at once, whatever the pages it fills.
	text := strings.Repeat("words and more words on a line of text ", 1600)
	var cells strings.Builder
	for i := range 5000 {
		fmt.Fprintf(&cells, "%d,w%d,%d,w%d\n", i, i, i*7, i*3)
	}
	// Paragraphs styled alike share one style: each of these has its own.
	var styled strings.Builder
	for i := range 2000 {
		fmt.Fprintf(&styled, "<p style=\"margin-left: %dpx\">a paragraph of words</p>", i)
	}
	var pic bytes.Buffer
	if err := png.Encode(&pic, image.NewNRGBA(image.Rect(0, 0, 1000, 1000))); err != nil {
		t.Fatal(err)
	}
	pictures := fstest.MapFS{"a.png": {Data: pic.Bytes()}}
	docx := budgetDocx(t, 3000)
	for name, tc := range map[string]struct {
		format    testFormat
		src       string
		resources fstest.MapFS
		holds     string
	}{
		"text shaping":    {format: formatText, src: text, holds: "text being shaped"},
		"html styles":     {format: formatHTML, src: styled.String(), holds: "computed styles"},
		"csv cells":       {format: formatCSV, src: cells.String(), holds: "cells"},
		"docx paragraphs": {format: formatDOCX, src: docx, holds: "XML trees"},
		"html picture": {
			format: formatHTML, src: `<img src="a.png">`, holds: "decoded pictures",
			resources: pictures,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			conversion := func(opts ...ConvertOption) *Conversion {
				opts = append([]ConvertOption{WithConvertFontDirs(dir)}, opts...)
				if tc.resources != nil {
					return FromHTML(strings.NewReader(tc.src), append(optionsAs[HTMLOption](opts), WithResources(tc.resources))...)
				}
				return fromFormat(tc.format, strings.NewReader(tc.src), opts...)
			}
			var out bytes.Buffer
			rep, err := conversion(WithConvertLimits(Limits{MaxConvertMemoryBytes: 2 << 20})).WriteTo(context.Background(), &out)
			if !errors.Is(err, ErrConvertInputTooLarge) || rep != nil || !strings.Contains(err.Error(), tc.holds) {
				t.Fatalf("under 2 MiB: %v, %+v, want ErrConvertInputTooLarge naming %s", err, rep, tc.holds)
			}
			if out.Len() != 0 {
				t.Errorf("wrote %d bytes, want none", out.Len())
			}
			if _, err := conversion().WriteTo(context.Background(), &out); err != nil {
				t.Errorf("under the default budget: %v", err)
			}
		})
	}
}

// TestConvert_PagesLeaveTheBudgetOnceWritten pins the streaming of pages
// through the public API: a text whose pages together pass the budget
// converts under it, each page's glyphs given back once the page is
// written, with or without a page selection that holds every page until
// the count is known.
func TestConvert_PagesLeaveTheBudgetOnceWritten(t *testing.T) {
	t.Parallel()
	dir := convertFontDir(t)
	// 4000 lines of 39 characters fill 77 pages and hold about 7 MiB of
	// glyphs at once, and 2.8 MB of structure tree until the file is
	// written.
	text := strings.Repeat("words and more words on a line of text\n", 4000)
	low := []TextOption{WithConvertFontDirs(dir), WithConvertLimits(Limits{MaxConvertMemoryBytes: 4 << 20})}
	var out bytes.Buffer
	rep, err := FromText(strings.NewReader(text), low...).WriteTo(context.Background(), &out)
	if err != nil || rep.Pages != 77 {
		t.Fatalf("under 4 MiB: %v, %+v, want 77 pages", err, rep)
	}
	_, err = FromText(strings.NewReader(text), append(low, WithPageRanges("last,1"))...).WriteTo(context.Background(), &out)
	if !errors.Is(err, ErrConvertInputTooLarge) {
		t.Errorf("a selection that holds every page under 4 MiB: %v, want ErrConvertInputTooLarge", err)
	}
}

// TestConvert_JustOverTheBudget pins the refusal of the smallest document
// a lowered [Limits.MaxConvertMemoryBytes] cannot hold, for the
// converters whose memory grows with what they keep until the file is
// written: the document one unit smaller converts, the smallest one past
// it fails with [ErrConvertInputTooLarge] from the memory budget, whose
// refusal names the memory it would pass the limit with.
func TestConvert_JustOverTheBudget(t *testing.T) {
	t.Parallel()
	dir := convertFontDir(t)
	for name, tc := range map[string]struct {
		format testFormat
		limit  int64
		src    func(n int) string
		max    int
	}{
		"text lines": {
			format: formatText, limit: 1 << 20, max: 1 << 12,
			src: func(n int) string { return strings.Repeat("words and more words on a line of text\n", n) },
		},
		"pptx slides": {
			format: formatPPTX, limit: 1 << 20, max: 1 << 10,
			src: func(n int) string { return string(slidesPPTX(t, n)) },
		},
		"odp slides": {
			format: formatODP, limit: 1 << 20, max: 1 << 11,
			src: func(n int) string { return budgetODP(n) },
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			convert := func(n int) error {
				opts := []ConvertOption{WithConvertFontDirs(dir), WithConvertLimits(Limits{MaxConvertMemoryBytes: tc.limit})}
				_, err := fromFormat(tc.format, strings.NewReader(tc.src(n)), opts...).WriteTo(context.Background(), io.Discard)
				return err
			}
			if err := convert(tc.max); !errors.Is(err, ErrConvertInputTooLarge) {
				t.Fatalf("%d units: %v, want ErrConvertInputTooLarge", tc.max, err)
			}
			// The smallest count refused: every smaller one converts.
			lo, hi := 1, tc.max
			for lo < hi {
				mid := (lo + hi) / 2
				if convert(mid) != nil {
					hi = mid
				} else {
					lo = mid + 1
				}
			}
			if lo == 1 {
				t.Fatal("a document of one unit is refused")
			}
			if err := convert(lo - 1); err != nil {
				t.Errorf("%d units: %v, want a conversion", lo-1, err)
			}
			err := convert(lo)
			t.Logf("refused from %d units: %v", lo, err)
			if !errors.Is(err, ErrConvertInputTooLarge) || !strings.Contains(err.Error(), "memory budget exceeded") {
				t.Errorf("%d units: %v, want ErrConvertInputTooLarge from the memory budget", lo, err)
			}
		})
	}
}

// budgetODP returns a flat ODP of n slides, each a title and a list of
// three items.
func budgetODP(n int) string {
	var body strings.Builder
	for i := range n {
		fmt.Fprintf(&body, `<draw:page draw:name="S%d" draw:master-page-name="Default">`+
			`<draw:frame svg:x="1cm" svg:y="1cm" svg:width="20cm" svg:height="3cm"><draw:text-box><text:p>Slide %d</text:p></draw:text-box></draw:frame>`+
			`<draw:frame svg:x="1cm" svg:y="5cm" svg:width="20cm" svg:height="10cm"><draw:text-box><text:list>`+
			`<text:list-item><text:p>First point</text:p></text:list-item><text:list-item><text:p>Second point</text:p></text:list-item>`+
			`<text:list-item><text:p>Third point</text:p></text:list-item></text:list></draw:text-box></draw:frame></draw:page>`, i, i)
	}
	return flatODF("presentation", pageLayout("PM1", "28cm", "21cm", ""),
		`<style:master-page style:name="Default" style:page-layout-name="PM1"/>`,
		`<office:presentation>`+body.String()+`</office:presentation>`)
}

// budgetDocx returns a WordprocessingML document of n short paragraphs.
func budgetDocx(t *testing.T, n int) string {
	t.Helper()
	var body strings.Builder
	for i := range n {
		fmt.Fprintf(&body, `<w:p><w:r><w:t>Paragraph %d of words</w:t></w:r></w:p>`, i)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, part := range []struct{ name, data string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`},
		{"word/document.xml", `<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + body.String() + `</w:body></w:document>`},
	} {
		w, err := zw.Create(part.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(part.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}
