package remediation

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// qNestingViolation is the finding that sends a page through the q/Q nesting
// repair at PDF/A-2b (ISO 19005-2, 6.1.13 t8).
var qNestingViolation = []compliance.Violation{{Rule: compliance.Rule_19005_2_6_1_13_t08}}

// inlineSplicePage builds a one-page document whose page content, object 4,
// is data, which draws text with /F1, the standard font Helvetica (object 5).
func inlineSplicePage(data string) []byte {
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /Lang (en) /MarkInfo << /Marked true >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		streamBody("", data),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	})
}

// contentFixer is a fixer's apply function.
type contentFixer func(context.Context, *writer.ObjAllocator, *document.Document, Options, []compliance.Violation, string) ([]string, error)

// validationOps returns the operations validation reads in data, failing t
// when it reads none, and checks that data carries every inline image of src
// byte for byte, each read as an image.
func validationOps(t *testing.T, src string, data []byte) []content.Operation {
	t.Helper()
	if got, want := bytes.Count(data, []byte(spliceImageData)), strings.Count(src, spliceImageData); got != want {
		t.Errorf("%d inline images came across byte for byte, want %d: %q", got, want, data)
	}
	ops, err := content.ParseStreamData(context.Background(), data)
	if err != nil {
		t.Fatalf("validation cannot read the rewrite: %v", err)
	}
	images := 0
	for _, op := range ops {
		if op.Operator != "BI" {
			continue
		}
		images++
		if dict, ok := op.Operands[0].(ast.PdfDict); !ok || dict["W"] != ast.PdfInt(4) {
			t.Errorf("an image lost its dictionary: %v", op.Operands)
		}
	}
	if want := strings.Count(src, spliceImageData); images != want {
		t.Errorf("validation reads %d images, want %d", images, want)
	}
	return ops
}

// maxQDepth returns the deepest q/Q nesting of ops.
func maxQDepth(ops []content.Operation) int {
	depth, deepest := 0, 0
	for _, op := range ops {
		switch op.Operator {
		case "q":
			depth++
			deepest = max(deepest, depth)
		case "Q":
			depth = max(depth-1, 0)
		}
	}
	return deepest
}

// TestContentFixers_SpliceAStreamHoldingAnInlineImage pins the repairs of the
// string-limit, text-control, marked-content-lang and q-nesting fixers on a
// page whose content holds an inline image: each repair is written in place
// of the operation it changes, and every other byte, the image's included, is
// carried across.
//
// Each fixer wrote the page back from its parsed operations, and the content
// parser reads an image's data off-token, so the BI operation keeps its
// dictionary alone: the output held "<< ... >> BI" with no ID, data or EI, a
// content stream no reader can parse (ISO 32000-1, 8.9.7).
func TestContentFixers_SpliceAStreamHoldingAnInlineImage(t *testing.T) {
	t.Parallel()

	img := spliceImage
	long := strings.Repeat("a", pdfA2StringLimit+10)
	for _, tc := range []struct {
		name, data, want, profile string
		fix                       contentFixer
		violations                []compliance.Violation
		fixes                     []string
	}{
		{
			name:    "string-limit",
			data:    "q BT /F1 12 Tf (" + long + ") Tj ET " + img + " Q",
			want:    "q BT /F1 12 Tf (" + long[:pdfA2StringLimit] + ") Tj\n ET " + img + " Q",
			profile: "PDF/A-2b",
			fix:     fixStringLimit,
			fixes:   []string{"truncated over-long strings in 1 object(s)/stream(s)"},
		},
		{
			name:    "text-control",
			data:    "q BT /F1 12 Tf (a\x01b) Tj ET " + img + " Q",
			want:    "q BT /F1 12 Tf (ab) Tj\n ET " + img + " Q",
			profile: "PDF/A-2b",
			fix:     fixTextControl,
			fixes:   []string{"stripped 1 control-byte(s) from text operators"},
		},
		{
			name:    "text-control, a string it empties and a TJ array",
			data:    "q BT /F1 12 Tf (\x01) Tj " + img + " [(a\x02) 120 (b)] TJ ET Q",
			want:    "q BT /F1 12 Tf  " + img + "  [(a) 120 (b)] TJ\n ET Q",
			profile: "PDF/A-2b",
			fix:     fixTextControl,
			fixes:   []string{"stripped 2 control-byte(s) from text operators"},
		},
		{
			name:    "marked-content-lang",
			data:    "/Span <</Lang (-pt) /MCID 0>> BDC " + img + " EMC",
			want:    " /Span << /MCID 0>> BDC\n " + img + " EMC",
			profile: "PDF/UA-1",
			fix:     fixMarkedContentLang,
			fixes:   []string{"removed invalid /Lang from 1 marked-content property list(s)"},
		},
		{
			name:       "q nesting",
			data:       strings.Repeat("q ", pdfAGraphicsDepth+1) + img + strings.Repeat(" Q", pdfAGraphicsDepth+1),
			want:       "q" + strings.Repeat(" q", pdfAGraphicsDepth-1) + "  " + img + "  " + strings.Repeat(" Q", pdfAGraphicsDepth),
			profile:    "PDF/A-2b",
			fix:        fixAggressiveLimits,
			violations: qNestingViolation,
			fixes:      []string{"flattened q/Q nesting on 1 page(s)"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			doc, alloc := newTestDoc(t, inlineSplicePage(tc.data))
			fixes, err := tc.fix(ctx, alloc, doc, Options{}, tc.violations, tc.profile)
			if err != nil {
				t.Fatalf("fix: %v", err)
			}
			if !slices.Equal(fixes, tc.fixes) {
				t.Errorf("fixes = %q, want %q", fixes, tc.fixes)
			}
			got := pendingStreamBytes(t, alloc, 4)
			ops := validationOps(t, tc.data, got)
			if string(got) != tc.want {
				t.Errorf("rewrote the page to\n  %q\nwant\n  %q", got, tc.want)
			}
			if d := maxQDepth(ops); d > pdfAGraphicsDepth {
				t.Errorf("validation reads a q/Q nesting of %d", d)
			}
		})
	}
}

// TestContentFixers_LeaveAStreamTheyCannotSplice pins the residual: an
// operation the repair changes that holds a name with a NUL byte, which
// [serializeOps] has no conforming spelling for (ISO 32000-1, 7.3.5), cannot
// be written again, so the page holding an inline image keeps its bytes, and
// its finding stays for validation to report rather than being counted as
// repaired.
func TestContentFixers_LeaveAStreamTheyCannotSplice(t *testing.T) {
	t.Parallel()

	img := spliceImage
	long := strings.Repeat("a", pdfA2StringLimit+10)
	for _, tc := range []struct {
		name, data, profile string
		fix                 contentFixer
	}{
		{"string-limit", "q /A#00 (" + long + ") Tj " + img + " Q", "PDF/A-2b", fixStringLimit},
		{"text-control", "q BT /F1 12 Tf /A#00 (a\x01b) Tj ET " + img + " Q", "PDF/A-2b", fixTextControl},
		{"marked-content-lang", "/Span <</Lang (-pt) /X /A#00>> BDC " + img + " EMC", "PDF/UA-1", fixMarkedContentLang},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			doc, alloc := newTestDoc(t, inlineSplicePage(tc.data))
			fixes, err := tc.fix(ctx, alloc, doc, Options{}, nil, tc.profile)
			if err != nil {
				t.Fatalf("fix: %v", err)
			}
			if len(fixes) != 0 {
				t.Errorf("fixes = %q, want none", fixes)
			}
			if _, pending := alloc.Pending()[4]; pending {
				t.Errorf("the page content was rewritten to %q, want it left as written", pendingStreamBytes(t, alloc, 4))
			}
		})
	}
}
