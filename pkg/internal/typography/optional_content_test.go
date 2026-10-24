package typography

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// optionalContentPDF builds a page whose /D configuration turns group 7 off
// and leaves group 8 on, and which draws:
//
//   - SHOWN, outside optional content;
//   - MDOFF, under /OC /Off, a membership dictionary over group 7 (AnyOn);
//   - MDON, under /OC /On, a membership dictionary over group 8;
//   - VEOFF, under /OC /Ve, a membership dictionary whose /VE is [/Not 8];
//   - FORMOFF, in a form whose /OC names group 7;
//   - FORMNAME, in a form whose own /Properties maps /L to group 8 while
//     the page maps /L to group 7, under /OC /L;
//   - AFTERFORM, under the page's /OC /L after that form, which is off again.
func optionalContentPDF() []byte {
	text := func(s string, y int) string {
		return fmt.Sprintf("BT /F1 12 Tf 50 %d Td (%s) Tj ET\n", y, s)
	}
	page := text("SHOWN", 700) +
		"/OC /Off BDC\n" + text("MDOFF", 680) + "EMC\n" +
		"/OC /On BDC\n" + text("MDON", 660) + "EMC\n" +
		"/OC /Ve BDC\n" + text("VEOFF", 640) + "EMC\n" +
		"/FmOff Do\n/FmName Do\n" +
		"/OC /L BDC\n" + text("AFTERFORM", 560) + "EMC\n"
	formOff := text("FORMOFF", 620)
	formName := "/OC /L BDC\n" + text("FORMNAME", 600) + "EMC\n"

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /OCProperties << /OCGs [7 0 R 8 0 R] /D << /OFF [7 0 R] >> >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
			"/Resources << /Font << /F1 5 0 R >> /XObject << /FmOff 6 0 R /FmName 11 0 R >> " +
			"/Properties << /Off 9 0 R /On 10 0 R /Ve 12 0 R /L 7 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(page), page),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 612 792] /OC 7 0 R "+
			"/Resources << /Font << /F1 5 0 R >> >> /Length %d >>\nstream\n%sendstream", len(formOff), formOff),
		"<< /Type /OCG /Name (Off) >>",
		"<< /Type /OCG /Name (On) >>",
		"<< /Type /OCMD /OCGs [7 0 R] >>",
		"<< /Type /OCMD /OCGs [8 0 R] >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 612 792] "+
			"/Resources << /Font << /F1 5 0 R >> /Properties << /L 8 0 R >> >> /Length %d >>\nstream\n%sendstream", len(formName), formName),
		"<< /Type /OCMD /OCGs [7 0 R] /VE [/Not 8 0 R] >>",
	}

	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	start := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, start)
	return b.Bytes()
}

// extractOptionalContent returns the text extracted from [optionalContentPDF]
// with the machine reading optional content visibility unless ignore is set.
func extractOptionalContent(t *testing.T, ignore bool) string {
	t.Helper()
	ctx := context.Background()
	data := optionalContentPDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}
	ops, err := content.ParsePageContent(ctx, doc, pages[0].Dict)
	if err != nil {
		t.Fatalf("ParsePageContent: %v", err)
	}
	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	machine.IgnoreOCVisibility = ignore
	elements, err := ExtractElements(ctx, doc, machine, pages[0], ops)
	if err != nil {
		t.Fatalf("ExtractElements: %v", err)
	}
	var joined strings.Builder
	for _, e := range elements {
		joined.WriteString(e.Text)
	}
	return joined.String()
}

// TestExtractElements_OptionalContentAsRendered pins that extraction hides
// the optional content the renderer hides (ISO 32000-1, 8.11): membership
// dictionaries by policy and visibility expression (8.11.2.2), a form's own
// /OC (8.11.3.3), and /Properties names read in the stream that names them.
func TestExtractElements_OptionalContentAsRendered(t *testing.T) {
	t.Parallel()
	got := extractOptionalContent(t, false)
	for _, want := range []string{"SHOWN", "MDON", "FORMNAME"} {
		if !strings.Contains(got, want) {
			t.Errorf("extracted %q, want it to contain the shown %q", got, want)
		}
	}
	for _, hidden := range []string{"MDOFF", "VEOFF", "FORMOFF", "AFTERFORM"} {
		if strings.Contains(got, hidden) {
			t.Errorf("extracted %q, want the hidden %q left out", got, hidden)
		}
	}
}

// TestExtractElements_OptionalContentIgnored pins that a caller reading
// every layer still gets the hidden text.
func TestExtractElements_OptionalContentIgnored(t *testing.T) {
	t.Parallel()
	got := extractOptionalContent(t, true)
	for _, want := range []string{"SHOWN", "MDOFF", "MDON", "VEOFF", "FORMOFF", "FORMNAME", "AFTERFORM"} {
		if !strings.Contains(got, want) {
			t.Errorf("extracted %q, want it to contain %q", got, want)
		}
	}
}
