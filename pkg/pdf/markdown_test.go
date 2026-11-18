package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
)

// taggedMarkdownPDF is a tagged page holding a heading, a paragraph with
// strong text a link covers, a numbered list, a table, a figure with
// alternative text, and a running head marked as an artifact.
func taggedMarkdownPDF() []byte {
	const cs = "/Artifact BMC BT /F1 9 Tf 72 760 Td (Running head) Tj ET EMC\n" +
		"/H1 <</MCID 0>> BDC BT /F1 24 Tf 72 700 Td (Annual report) Tj ET EMC\n" +
		"BT /F1 12 Tf 72 670 Td /P <</MCID 1>> BDC (Read the ) Tj EMC " +
		"/Strong <</MCID 2>> BDC (docs) Tj EMC /P <</MCID 3>> BDC ( today.) Tj EMC ET\n" +
		"/Lbl <</MCID 4>> BDC BT /F1 12 Tf 72 640 Td (1.) Tj ET EMC\n" +
		"/LBody <</MCID 5>> BDC BT /F1 12 Tf 90 640 Td (Install) Tj ET EMC\n" +
		"/Lbl <</MCID 6>> BDC BT /F1 12 Tf 72 625 Td (2.) Tj ET EMC\n" +
		"/LBody <</MCID 7>> BDC BT /F1 12 Tf 90 625 Td (Run) Tj ET EMC\n" +
		"/TH <</MCID 8>> BDC BT /F1 12 Tf 72 595 Td (Name) Tj ET EMC\n" +
		"/TH <</MCID 9>> BDC BT /F1 12 Tf 200 595 Td (Value) Tj ET EMC\n" +
		"/TD <</MCID 10>> BDC BT /F1 12 Tf 72 580 Td (a|b) Tj ET EMC\n" +
		"/TD <</MCID 11>> BDC BT /F1 12 Tf 200 580 Td (1) Tj ET EMC\n"
	const tree = "<< /Type /StructElem /S /Document /P 6 0 R /Pg 3 0 R /K [" +
		"<< /S /H1 /K 0 >> " +
		"<< /S /P /K [1 << /S /Strong /K 2 >> 3] >> " +
		"<< /S /L /K [<< /S /LI /K [<< /S /Lbl /K 4 >> << /S /LBody /K 5 >>] >> " +
		"<< /S /LI /K [<< /S /Lbl /K 6 >> << /S /LBody /K 7 >>] >>] >> " +
		"<< /S /Table /K [<< /S /TR /K [<< /S /TH /K 8 >> << /S /TH /K 9 >>] >> " +
		"<< /S /TR /K [<< /S /TD /K 10 >> << /S /TD /K 11 >>] >>] >> " +
		"<< /S /Figure /Alt (Company logo) >>] >>"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 6 0 R /MarkInfo << /Marked true >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> " +
			"/Contents 5 0 R /Annots [7 0 R] /StructParents 0 >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cs), cs),
		"<< /Type /StructTreeRoot /K 8 0 R >>",
		// "docs" runs from x 124.0 to 149.4 at 12 pt Helvetica.
		"<< /Type /Annot /Subtype /Link /Rect [123 665 150 682] /A << /S /URI /URI (https://example.com/docs) >> >>",
		tree,
	})
}

// TestReader_ExtractMarkdownReadsTheStructureTree pins the Markdown of a
// tagged page: each structure element becomes its Markdown block, a link
// annotation over text becomes a link, and the running head, an artifact
// (ISO 32000-1, 14.8.2.2), is left out unless [WithArtifacts] asks for it.
func TestReader_ExtractMarkdownReadsTheStructureTree(t *testing.T) {
	t.Parallel()
	const body = "# Annual report\n\n" +
		"Read the **[docs](https://example.com/docs)** today.\n\n" +
		"1. Install\n2. Run\n\n" +
		"| Name | Value |\n| --- | --- |\n| a\\|b | 1 |\n\n" +
		"![Company logo]()\n"

	cases := []struct {
		name string
		opts []ExtractTextOption
		want string
	}{
		{"artifacts left out", nil, body},
		{"artifacts kept", []ExtractTextOption{WithArtifacts()}, body + "\nRunning head\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			data := taggedMarkdownPDF()
			r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			got, err := r.ExtractMarkdown(context.Background(), nil, c.opts...)
			if err != nil {
				t.Fatalf("ExtractMarkdown: %v", err)
			}
			if got != c.want {
				t.Errorf("ExtractMarkdown =\n%s\nwant\n%s", got, c.want)
			}
		})
	}
}

// TestReader_ExtractMarkdownReadsTheLayout pins the Markdown of an untagged
// page: the larger line is a heading and the two body lines one paragraph.
func TestReader_ExtractMarkdownReadsTheLayout(t *testing.T) {
	t.Parallel()
	data := untaggedHeadingPDF()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	got, err := r.ExtractMarkdown(context.Background(), nil)
	if err != nil {
		t.Fatalf("ExtractMarkdown: %v", err)
	}
	const want = "# Document Heading\n\n" +
		"First body line of the paragraph here. Second body line continuing it here.\n"
	if got != want {
		t.Errorf("ExtractMarkdown =\n%s\nwant\n%s", got, want)
	}
}

// TestReader_ExtractMarkdownSelectsPages checks the pages read: those listed,
// in document order and once each, and an out-of-range one refused.
func TestReader_ExtractMarkdownSelectsPages(t *testing.T) {
	t.Parallel()
	page := func(text string) string {
		cs := fmt.Sprintf("BT /F1 12 Tf 72 700 Td (%s) Tj ET", text)
		return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cs), cs)
	}
	data := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 /Resources << /Font << /F1 5 0 R >> >> /MediaBox [0 0 612 792] >>",
		"<< /Type /Page /Parent 2 0 R /Contents 6 0 R >>",
		"<< /Type /Page /Parent 2 0 R /Contents 7 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		page("One"),
		page("Two"),
	})
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	for _, c := range []struct {
		pages []int
		want  string
	}{
		{nil, "One\n\nTwo\n"},
		{[]int{2}, "Two\n"},
		{[]int{2, 1, 2}, "One\n\nTwo\n"},
	} {
		got, err := r.ExtractMarkdown(context.Background(), c.pages)
		if err != nil {
			t.Fatalf("ExtractMarkdown(%v): %v", c.pages, err)
		}
		if got != c.want {
			t.Errorf("ExtractMarkdown(%v) = %q, want %q", c.pages, got, c.want)
		}
	}
	if _, err := r.ExtractMarkdown(context.Background(), []int{3}); !errors.Is(err, ErrInvalidPageNumber) {
		t.Errorf("ExtractMarkdown([3]) = %v, want ErrInvalidPageNumber", err)
	}
}
