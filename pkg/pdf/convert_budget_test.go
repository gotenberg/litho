package pdf

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
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
	// One source line is one paragraph, laid out whole: its glyphs are held
	// at once, whatever the pages they fill.
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
		"text glyph runs": {format: formatText, src: text, holds: "glyph runs"},
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
	// glyphs at once.
	text := strings.Repeat("words and more words on a line of text\n", 4000)
	low := []TextOption{WithConvertFontDirs(dir), WithConvertLimits(Limits{MaxConvertMemoryBytes: 2 << 20})}
	var out bytes.Buffer
	rep, err := FromText(strings.NewReader(text), low...).WriteTo(context.Background(), &out)
	if err != nil || rep.Pages != 77 {
		t.Fatalf("under 2 MiB: %v, %+v, want 77 pages", err, rep)
	}
	_, err = FromText(strings.NewReader(text), append(low, WithPageRanges("last,1"))...).WriteTo(context.Background(), &out)
	if !errors.Is(err, ErrConvertInputTooLarge) {
		t.Errorf("a selection that holds every page under 2 MiB: %v, want ErrConvertInputTooLarge", err)
	}
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
