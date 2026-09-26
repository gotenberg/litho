package compliance

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// referencedContentPDF assembles a one-page document whose page takes its
// /Contents from object 4, a stream that fills with DeviceRGB, which a file
// with no output intent may not use (ISO 19005-2, 6.2.4.3). table decides
// whether the cross-reference table lists object 4 ("listed"), leaves it out
// while the body carries it ("unlisted"), or the file holds no object 4 at all
// ("absent"), or leaves it out and gives its header a generation no integer
// holds ("overflow"), which the raw scan of a rebuild does not read and the
// raw-byte augmentation does. With broken set, startxref points past the end
// of the file, so the table cannot be read and litho rebuilds the index from a
// raw scan.
func referencedContentPDF(table string, broken bool) []byte {
	const content = "1 0 0 rg 0 0 10 10 re f"
	bodies := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	offsets := make([]int, len(bodies)+1)
	for i, body := range bodies {
		if i+1 == 4 && table == "absent" {
			continue
		}
		offsets[i+1] = buf.Len()
		gen := "0"
		if i+1 == 4 && table == "overflow" {
			gen = "18446744073709551616"
		}
		fmt.Fprintf(&buf, "%d %s obj\n%s\nendobj\n", i+1, gen, body)
	}
	listed := 3
	if table == "listed" {
		listed = 4
	}
	start := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", listed+1)
	for i := 1; i <= listed; i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offsets[i])
	}
	if broken {
		start = buf.Len() + 1000
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size 5 /Root 1 0 R /ID [<00112233445566778899AABBCCDDEEFF> <00112233445566778899AABBCCDDEEFF>] >>\nstartxref\n%d\n%%%%EOF\n", start)
	return buf.Bytes()
}

// TestValidationReadsAnUnlistedReferenceAsUndefined pins that a validation
// run judges a reference to an object the cross-reference table leaves out as
// it judges one to an object the file does not hold, which ISO 32000-1, 7.3.10
// makes null, and not as the object the body carries under that number.
//
// veraPDF 1.30.2 reports no DeviceRGB use on such a page at PDF/A-2b, and the
// DeviceRGB violation when the table lists the stream. litho followed the
// reference through the raw-byte augmentation that finds objects a damaged
// table dropped, and reported it. A caller that ran the augmentation first,
// text extraction for one, must not change that either.
func TestValidationReadsAnUnlistedReferenceAsUndefined(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	validate := func(t *testing.T, data []byte, before func(*document.Document)) ([]string, bool) {
		t.Helper()
		doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		if before != nil {
			before(doc)
		}
		errs, err := ValidatePDFA(ctx, doc, models.PDFA2b)
		if err != nil {
			t.Fatalf("ValidatePDFA: %v", err)
		}
		out := make([]string, len(errs))
		rgb := false
		for i, e := range errs {
			out[i] = e.Error()
			var v Violation
			if errors.As(e, &v) && v.Rule.Clause == "6.2.4.3" {
				rgb = true
			}
		}
		return out, rgb
	}

	// Control: the fixture draws the violation when the table lists the
	// stream.
	if _, rgb := validate(t, referencedContentPDF("listed", false), nil); !rgb {
		t.Fatal("a listed /Contents drew no DeviceRGB violation, so the cases below prove nothing")
	}

	absent, rgb := validate(t, referencedContentPDF("absent", false), nil)
	if rgb {
		t.Fatal("an absent /Contents drew a DeviceRGB violation")
	}

	unlisted := referencedContentPDF("unlisted", false)
	fresh, rgb := validate(t, unlisted, nil)
	if rgb {
		t.Errorf("a /Contents the table leaves out drew a DeviceRGB violation: %q", fresh)
	}
	if !slices.Equal(fresh, absent) {
		t.Errorf("a /Contents the table leaves out reports\n%q\nand an absent one reports\n%q", fresh, absent)
	}

	after, rgb := validate(t, unlisted, func(doc *document.Document) {
		// Control: without the validation's view the augmentation finds the
		// stream, as text extraction does, and caches it.
		if _, err := document.ResolveObject(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4}); err != nil {
			t.Fatalf("resolving object 4 before the validation: %v", err)
		}
	})
	if rgb {
		t.Errorf("after an earlier caller resolved it, a /Contents the table leaves out drew a DeviceRGB violation: %q", after)
	}
	if !slices.Equal(after, fresh) {
		t.Errorf("a validation after an earlier caller resolved the stream reports\n%q\nand a fresh one reports\n%q", after, fresh)
	}
}

// TestValidationJudgesWhatARebuiltIndexRecovers pins that a file whose table
// cannot be read, and whose index litho rebuilds from a raw scan, is judged on
// every object a raw scan recovers, the rebuild's or the raw-byte
// augmentation's: no table leaves an object out, so the view a validation run
// reads through leaves them all in reach, whatever an earlier caller resolved.
//
// The "overflow" case mirrors pdfjs poppler-85140-0.pdf, whose page only the
// augmentation recovers: reading it as undefined dropped a violation from a
// file veraPDF does not open.
func TestValidationJudgesWhatARebuiltIndexRecovers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, table := range []string{"listed", "unlisted", "overflow"} {
		for _, earlier := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s earlier %v", table, earlier), func(t *testing.T) {
				t.Parallel()
				data := referencedContentPDF(table, true)
				doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
				if err != nil {
					t.Fatalf("NewDocument: %v", err)
				}
				if !doc.Rebuilt {
					t.Fatal("the index was not rebuilt, so the case proves nothing")
				}
				if earlier {
					if _, err := document.ResolveObject(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4}); err != nil {
						t.Fatalf("resolving object 4 before the validation: %v", err)
					}
				}
				errs, err := ValidatePDFA(ctx, doc, models.PDFA2b)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				if !slices.ContainsFunc(errs, func(e error) bool {
					var v Violation
					return errors.As(e, &v) && v.Rule.Clause == "6.2.4.3"
				}) {
					t.Errorf("the rebuilt index's /Contents drew no DeviceRGB violation: %q", errs)
				}
			})
		}
	}
}
