package remediation

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// spliceImage is an inline image whose data holds bytes no token reading
// keeps: a NUL, a high byte, a delimiter and an end-of-line marker.
const spliceImage = "BI /W 4 /H 1 /BPC 8 /CS /G ID \x00\xff)\r EI"

// TestRepairContentData_SplicesAroundInlineImages pins the repairs of a
// stream holding an inline image, each written in place of the operation it
// changes while every other byte, the image's included, is carried across.
// Every one of these streams used to keep its defects: serializing the parsed
// operations drops an inline image's data (ISO 32000-1, 8.9.7).
func TestRepairContentData_SplicesAroundInlineImages(t *testing.T) {
	t.Parallel()

	img := spliceImage
	for _, tc := range []struct {
		name, data, want string
		tally            unknownOpsTally
	}{
		{
			name:  "undefined operator",
			data:  "q " + img + " Q zz 0 0 m",
			want:  "q " + img + " Q  0 0 m",
			tally: unknownOpsTally{removed: 1},
		},
		{
			// The image runs through the white-space byte past its EI, where
			// PDF.js reads on, so the stray EI's span begins at the EI.
			name:  "stray EI and ID",
			data:  "q " + img + " EI Q ID",
			want:  "q " + img + "   Q ",
			tally: unknownOpsTally{removed: 2},
		},
		{
			name:  "glued operators",
			data:  "BT (a) Tj ETBT (b) Tj ET q " + img + " Q",
			want:  "BT (a) Tj ET\nBT\n (b) Tj ET q " + img + " Q",
			tally: unknownOpsTally{separated: 1},
		},
		{
			name:  "malformed number",
			data:  "q " + img + " Q -. 2 Td",
			want:  "q " + img + " Q 0 2 Td\n",
			tally: unknownOpsTally{repaired: 1},
		},
		{
			name:  "malformed number before an image",
			data:  "q -. " + img + " Q zz",
			want:  "q " + img + " Q ",
			tally: unknownOpsTally{repaired: 1, removed: 1},
		},
		{
			name:  "operator against a comment",
			data:  "q 0 0 10 10 re%c\nf Q q " + img + " Q",
			want:  "q 0 0 10 10 re f\n Q q " + img + " Q",
			tally: unknownOpsTally{delimited: 1},
		},
		{
			name:  "operator against a delimiter",
			data:  "q 1 0 0 1 0 0 cm) Q " + img,
			want:  "q 1 0 0 1 0 0 cm Q\n " + img,
			tally: unknownOpsTally{delimited: 1},
		},
		{
			name:  "stray delimiter",
			data:  "q 1 0 0 1 0 0 cm ) Q " + img,
			want:  "q 1 0 0 1 0 0 cm Q\n " + img,
			tally: unknownOpsTally{strays: 1},
		},
		{
			name:  "between two images",
			data:  "q " + img + " Q foo q " + img + " Q",
			want:  "q " + img + " Q  q " + img + " Q",
			tally: unknownOpsTally{removed: 1},
		},
		{
			name:  "a filtered image",
			data:  "q BI /W 2 /H 1 /BPC 8 /CS /G /F /AHx ID 00ff> EI Q foo",
			want:  "q BI /W 2 /H 1 /BPC 8 /CS /G /F /AHx ID 00ff> EI Q ",
			tally: unknownOpsTally{removed: 1},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()

			got, tally := repairContentData(ctx, []byte(tc.data), 2)
			if string(got) != tc.want || tally != tc.tally {
				t.Errorf("repaired %q to\n  %q, %+v\nwant\n  %q, %+v", tc.data, got, tally, tc.want, tc.tally)
			}
			if !bytes.Contains(got, []byte(" ID ")) || bytes.Count(got, []byte(" ID ")) != bytes.Count([]byte(tc.data), []byte(" ID ")) {
				t.Errorf("an image went: %q", got)
			}
			ops, err := content.ParseStreamData(ctx, got)
			if err != nil {
				t.Fatalf("validation cannot read the repair: %v", err)
			}
			for _, op := range ops {
				if content.IsUndefined(op.Operator) {
					t.Errorf("validation still reads the undefined operator %q", op.Operator)
				}
			}
		})
	}
}

// TestRepairContentData_LeavesAnImageStreamItCannotSplice pins the residual:
// a run of operators that splits into a BI would begin an inline image no
// dictionary follows, so the stream keeps its bytes, and its other defects
// stay for validation to report.
func TestRepairContentData_LeavesAnImageStreamItCannotSplice(t *testing.T) {
	t.Parallel()
	data := "q " + spliceImage + " QBI zz"
	got, tally := repairContentData(context.Background(), []byte(data), 2)
	if tally != (unknownOpsTally{}) {
		t.Errorf("repaired %q to %q, %+v, want it left as written", data, got, tally)
	}
}

// TestRepairContentData_LeavesAnOperatorPDFJSReadsAsData pins that an
// undefined operator written just past an image's EI stays: PDF.js ends the
// data at no EI whose next bytes lex to an operator Annex A, Table A.1 does
// not define (Parser.findDefaultInlineStreamEnd, src/core/parser.js), so it
// reads "foo" and the second image as the first image's data, and removing
// "foo" would end the first image at its EI and draw the second.
func TestRepairContentData_LeavesAnOperatorPDFJSReadsAsData(t *testing.T) {
	t.Parallel()
	data := spliceImage + " foo " + spliceImage
	got, tally := repairContentData(context.Background(), []byte(data), 2)
	if tally != (unknownOpsTally{}) {
		t.Errorf("repaired %q to %q, %+v, want it left as written", data, got, tally)
	}
}

// TestFixUnknownOps_SplicesAnInlineImageForm pins that the splice reaches the
// streams a page draws besides its content, a form XObject here, and reports
// what it removed.
func TestFixUnknownOps_SplicesAnInlineImageForm(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, alloc := newTestDoc(t, exponentPagePDF("/X0 Do", "q "+spliceImage+" Q zz foo", "", ""))

	fixes, err := fixUnknownOps(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixUnknownOps: %v", err)
	}
	if !slices.Contains(fixes, "removed 2 unknown content-stream operator(s)") {
		t.Errorf("fixes %q, want the two operators reported", fixes)
	}
	if got, want := string(pendingStreamBytes(t, alloc, 5)), "q "+spliceImage+" Q  "; got != want {
		t.Errorf("form = %q, want %q", got, want)
	}
}

// TestFixUnknownOps_SpliceKeepsTransparencysFormWrite is the clobber guard
// against the real peer that rewrites the same form first: transparency
// (PDF/A-1, registry ahead of unknown-ops) drops the form's /Group, and the
// splice writes the form back over the allocator's copy, so the group stays
// dropped.
func TestFixUnknownOps_SpliceKeepsTransparencysFormWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	form := "q " + spliceImage + " Q zz"
	doc, alloc := newTestDoc(t, assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /XObject << /Fm0 5 0 R >> >> >>",
		streamBody("", "/Fm0 Do"),
		streamBody("/Type /XObject /Subtype /Form /BBox [0 0 10 10] /Group << /S /Transparency >>", form),
	}))
	if _, err := fixTransparency(ctx, alloc, doc, Options{}, nil, "PDF/A-1b"); err != nil {
		t.Fatalf("fixTransparency: %v", err)
	}
	if stream, ok := alloc.Pending()[5].(ast.PdfStream); !ok || stream.Dict["Group"] != nil {
		t.Fatal("control: transparency did not drop the form's /Group")
	}

	if _, err := fixUnknownOps(ctx, alloc, doc, Options{}, nil, "PDF/A-1b"); err != nil {
		t.Fatalf("fixUnknownOps: %v", err)
	}
	stream, _ := alloc.Pending()[5].(ast.PdfStream)
	if stream.Dict["Group"] != nil {
		t.Errorf("the form's /Group is back: the splice rebuilt the form from the document; dict = %v", stream.Dict)
	}
	if got, want := string(pendingStreamBytes(t, alloc, 5)), "q "+spliceImage+" Q "; got != want {
		t.Errorf("form = %q, want %q", got, want)
	}
}

// TestFixUnknownOps_SpliceKeepsTheRenamedNames is the clobber guard against
// the real peer that rewrites the same stream first: name-utf8 (registry
// ahead of unknown-ops) renames an over-long colour space an inline image
// names, and the splice builds on the renamed stream, so both repairs land and
// the image data survives both.
func TestFixUnknownOps_SpliceKeepsTheRenamedNames(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	stream := fmt.Sprintf("q BI /W 4 /H 1 /BPC 8 /CS %s ID \x00\xff)\r EI Q zz", longKeySpelled)
	doc, alloc := newTestDoc(t, assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /ColorSpace << %s /DeviceGray >> >> >>", longKeySpelled),
		streamBody("", stream),
	}))
	if _, err := fixNameUTF8(ctx, alloc, doc, Options{}, nameLengthViolations, "PDF/A-2b"); err != nil {
		t.Fatalf("fixNameUTF8: %v", err)
	}
	if _, ok := alloc.Pending()[4]; !ok {
		t.Fatal("control: name-utf8 did not rewrite the page's content")
	}

	if _, err := fixUnknownOps(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixUnknownOps: %v", err)
	}
	body := string(pendingStreamBytes(t, alloc, 4))
	if strings.Contains(body, longKeySpelled) || strings.Contains(body, "zz") || !strings.Contains(body, " ID \x00\xff)\r EI") {
		t.Errorf("page = %q, want the name renamed, zz removed and the image data kept", body)
	}
	if checkResolves(t, alloc, doc, 4, liveResourcesOf(t, alloc, doc, 3)) != 1 {
		t.Fatal("want the inline image's colour space checked")
	}
}
