package document

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// buildDrawResourcesPDF builds a page tree whose root lends the font FR, and
// whose inner node lends F6 and CS4 to six pages, each stating /Resources in
// one way: a dictionary of its own naming F2, none, null, a reference to a
// missing object, an integer, and none under a node stating none.
func buildDrawResourcesPDF() []byte {
	objs := []string{
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n",
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 6 /MediaBox [0 0 612 792] /Resources << /Font << /FR 20 0 R >> >> >>\nendobj\n",
		"3 0 obj\n<< /Type /Pages /Parent 2 0 R /Kids [4 0 R 5 0 R 6 0 R 7 0 R 8 0 R 9 0 R] /Count 6 /Resources << /Font << /F6 20 0 R >> /ColorSpace << /CS4 /DeviceRGB >> >> >>\nendobj\n",
		"4 0 obj\n<< /Type /Page /Parent 3 0 R /Resources << /Font << /F2 20 0 R >> >> >>\nendobj\n",
		"5 0 obj\n<< /Type /Page /Parent 3 0 R >>\nendobj\n",
		"6 0 obj\n<< /Type /Page /Parent 3 0 R /Resources null >>\nendobj\n",
		"7 0 obj\n<< /Type /Page /Parent 3 0 R /Resources 99 0 R >>\nendobj\n",
		"8 0 obj\n<< /Type /Page /Parent 3 0 R /Resources 7 >>\nendobj\n",
		"9 0 obj\n<< /Type /Pages /Parent 3 0 R /Kids [10 0 R] /Count 1 >>\nendobj\n",
		"10 0 obj\n<< /Type /Page /Parent 9 0 R >>\nendobj\n",
	}
	objs = append(objs, "20 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>\nendobj\n")

	header := "%PDF-1.7\n"
	var body strings.Builder
	offsets := map[int]int{}
	for _, o := range objs {
		var num int
		if _, err := fmt.Sscanf(o, "%d 0 obj", &num); err != nil {
			panic(err)
		}
		offsets[num] = len(header) + body.Len()
		body.WriteString(o)
	}
	const size = 21
	var xref strings.Builder
	fmt.Fprintf(&xref, "xref\n0 %d\n0000000000 65535 f\r\n", size)
	for n := 1; n < size; n++ {
		if off, ok := offsets[n]; ok {
			fmt.Fprintf(&xref, "%010d 00000 n\r\n", off)
		} else {
			xref.WriteString("0000000000 65535 f\r\n")
		}
	}
	xrefOffset := len(header) + body.Len()
	trailer := fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", size, xrefOffset)
	return []byte(header + body.String() + xref.String() + trailer)
}

// TestDrawResourcesFollowsInheritance pins that a page draws with the nearest
// /Resources dictionary whole (ISO 32000-1, 7.7.3.4), where Resources merges
// every ancestor's in: a page stating F2 alone reaches neither F6 nor FR, and
// a page stating none, null (7.3.7), a missing object (7.3.10) or a value
// that is no dictionary takes its node's, without the root's FR.
func TestDrawResourcesFollowsInheritance(t *testing.T) {
	pdf := buildDrawResourcesPDF()
	ctx := context.Background()
	doc, err := NewDocument(ctx, bytes.NewReader(pdf), int64(len(pdf)), nil)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatal(err)
	}
	keys := func(res ast.PdfDict, category ast.PdfName) []string {
		sub, err := ResolveDict(ctx, doc, res[category])
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for k := range sub {
			out = append(out, string(k))
		}
		slices.Sort(out)
		return out
	}
	tests := []struct {
		page        int
		name        string
		fonts       []string
		spaces      []string
		mergedFonts []string
	}{
		{4, "own dictionary", []string{"F2"}, nil, []string{"F2", "F6", "FR"}},
		{5, "none", []string{"F6"}, []string{"CS4"}, []string{"F6", "FR"}},
		{6, "null", []string{"F6"}, []string{"CS4"}, []string{"F6", "FR"}},
		{7, "missing object", []string{"F6"}, []string{"CS4"}, []string{"F6", "FR"}},
		{8, "integer", []string{"F6"}, []string{"CS4"}, []string{"F6", "FR"}},
		{10, "none under a node stating none", []string{"F6"}, []string{"CS4"}, []string{"F6", "FR"}},
	}
	if len(pages) != len(tests) {
		t.Fatalf("got %d pages, want %d", len(pages), len(tests))
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := pages[i]
			if p.Ref.ObjectNum != tt.page {
				t.Fatalf("page %d is object %d, want %d", i+1, p.Ref.ObjectNum, tt.page)
			}
			if got := keys(p.DrawResources, "Font"); !slices.Equal(got, tt.fonts) {
				t.Errorf("DrawResources /Font = %v, want %v", got, tt.fonts)
			}
			if got := keys(p.DrawResources, "ColorSpace"); !slices.Equal(got, tt.spaces) {
				t.Errorf("DrawResources /ColorSpace = %v, want %v", got, tt.spaces)
			}
			if got := keys(p.Resources, "Font"); !slices.Equal(got, tt.mergedFonts) {
				t.Errorf("Resources /Font = %v, want the merge %v", got, tt.mergedFonts)
			}
		})
	}
}
