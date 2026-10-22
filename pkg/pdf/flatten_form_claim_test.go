package pdf

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

// flattenClaimXMP is an XMP packet claiming PDF/UA-1 (ISO 14289-1, 5).
const flattenClaimXMP = `<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
	`<rdf:Description rdf:about="" xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/"><pdfuaid:part>1</pdfuaid:part></rdf:Description>` +
	`</rdf:RDF></x:xmpmeta>`

// untaggedFieldPDF builds an untagged page with one text field whose
// appearance shows "Jane Doe", claiming PDF/UA-1 in its metadata when claim
// is set.
func untaggedFieldPDF(claim bool) []byte {
	const ap = "BT /Helv 10 Tf 5 5 Td (Jane Doe) Tj ET"
	catalog := "<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>"
	if claim {
		catalog = "<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> /Metadata 7 0 R >>"
	}
	objs := []string{
		catalog,
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (Jane Doe) /Rect [50 700 250 720] /F 4 /P 3 0 R /AP << /N 5 0 R >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 200 20] /Resources << /Font << /Helv 6 0 R >> >> /Length %d >>\nstream\n%s\nendstream", len(ap), ap),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	}
	if claim {
		objs = append(objs, fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(flattenClaimXMP), flattenClaimXMP))
	}
	return buildPDF(objs)
}

// TestWriter_FlattenFormKeepsUntaggedValuesExtractable pins what default text
// extraction returns after [Writer.FlattenForm] flattens an untagged
// document. With no PDF/UA claim the flattened value is plain page content,
// so flatten-then-extract returns it. With a PDF/UA-1 claim it is an artifact
// (ISO 14289-1, 7.1), which the default extraction leaves out and
// [WithArtifacts] returns.
func TestWriter_FlattenFormKeepsUntaggedValuesExtractable(t *testing.T) {
	t.Parallel()
	for _, claim := range []bool{false, true} {
		t.Run(fmt.Sprintf("claim=%v", claim), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			data := untaggedFieldPDF(claim)
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			w := NewWriter(r)
			w.FlattenForm()
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			flat, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
			if err != nil {
				t.Fatalf("Open output: %v", err)
			}
			defer flat.Close()

			text, err := flat.ExtractText(ctx, 1)
			if err != nil {
				t.Fatalf("ExtractText: %v", err)
			}
			if got := strings.Contains(text, "Jane Doe"); got == claim {
				t.Errorf("default text = %q, want the value present %v", text, !claim)
			}
			all, err := flat.ExtractText(ctx, 1, WithArtifacts())
			if err != nil {
				t.Fatalf("ExtractText(WithArtifacts): %v", err)
			}
			if !strings.Contains(all, "Jane Doe") {
				t.Errorf("text with artifacts = %q, want the value", all)
			}
		})
	}
}
