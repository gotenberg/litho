package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestUntaggedContent_InlineImage pins that an inline image is real content the
// tagging rule judges.
//
// An inline image (BI ... ID ... EI, ISO 32000-1, 8.9.7) paints an image just as
// a Do operator naming an image XObject does, and veraPDF builds a content item
// for it that ISO 14289-2, 8.2.2 requires to be tagged or marked as an Artifact.
// litho counted fills, strokes, text, shadings and Do as painting and left the
// inline image out, so pdfjs issue11124.pdf, whose only page content is an
// untagged inline image, passed. Measured against veraPDF on fixtures of each
// shape.
func TestUntaggedContent_InlineImage(t *testing.T) {
	t.Parallel()

	const image = "BI /W 2 /H 2 /CS /G /BPC 8 ID \x00\xff\xff\x00 EI"

	// doc paints the inline image through body. With tagged, a Figure element
	// owns MCID 0 through the page's /StructParents.
	doc := func(body string, tagged bool) []byte {
		pageKeys, rootKeys := "", " /K 5 0 R"
		elems := []string{"<< /Type /StructElem /S /Document /P 4 0 R >>"}
		if tagged {
			pageKeys = " /StructParents 0"
			rootKeys = " /K 5 0 R /ParentTree << /Nums [0 [6 0 R]] >>"
			elems = []string{
				"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R] >>",
				"<< /Type /StructElem /S /Figure /Alt (dots) /P 5 0 R /Pg 3 0 R /K 0 >>",
			}
		}
		content := "q 100 0 0 100 50 50 cm " + body + " Q"
		objs := []string{
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /MarkInfo << /Marked true >> /Lang (en-US) >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200]%s /Contents %d 0 R >>", pageKeys, 5+len(elems)),
			"<< /Type /StructTreeRoot" + rootKeys + " >>",
		}
		objs = append(objs, elems...)
		objs = append(objs, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
		return cyclePDF(objs...)
	}

	// fires reports whether the profile's tagging rule fires: ISO 14289-2, 8.2.2
	// t1, or ISO 14289-1, 7.1 t3, which veraPDF judges the same way.
	fires := func(t *testing.T, data []byte, profile models.PDFUAProfile) bool {
		t.Helper()
		d, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		viols, err := ValidatePDFUA(context.Background(), d, profile)
		if err != nil {
			t.Fatalf("ValidatePDFUA: %v", err)
		}
		clause, test := "8.2.2", 1
		if profile == models.PDFUA1 {
			clause, test = "7.1", 3
		}
		for _, e := range viols {
			if v, ok := e.(Violation); ok && v.Rule.Clause == clause && v.Rule.TestNumber == test {
				return true
			}
		}
		return false
	}

	tests := []struct {
		name   string
		body   string
		tagged bool
		want   bool
	}{
		{"an untagged inline image", image, false, true},
		{"an inline image marked as an Artifact", "/Artifact BMC " + image + " EMC", false, false},
		{"an inline image tagged through its MCID", "/Figure << /MCID 0 >> BDC " + image + " EMC", true, false},
	}
	for _, tt := range tests {
		for _, profile := range []models.PDFUAProfile{models.PDFUA1, models.PDFUA2} {
			t.Run(tt.name+" ("+string(profile)+")", func(t *testing.T) {
				t.Parallel()
				if got := fires(t, doc(tt.body, tt.tagged), profile); got != tt.want {
					t.Errorf("tagging rule fired = %v, want %v", got, tt.want)
				}
			})
		}
	}
}
