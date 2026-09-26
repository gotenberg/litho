package remediation

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// aggressiveIntent is the finding that sends a page through the aggressive
// content rewrite's replacement of invalid rendering intents.
var aggressiveIntent = []compliance.Violation{{Rule: compliance.Rule_19005_2_6_2_6_t01}}

// intentReport is the fix the aggressive content rewrite reports for one
// rendering intent replaced.
const intentReport = "replaced 1 invalid rendering intents in content streams"

// invalidIntents returns the intents validation reads in data, the operand of
// ri or the /Intent of an inline image, that ISO 32000-1, 8.6.5.8, Table 70
// does not define.
func invalidIntents(t *testing.T, data []byte) []ast.PdfName {
	t.Helper()
	ops, err := content.ParseStreamData(context.Background(), data)
	if err != nil {
		t.Fatalf("validation cannot read the rewrite: %v", err)
	}
	var bad []ast.PdfName
	for _, op := range ops {
		var intent ast.PdfObject
		switch {
		case op.Operator == "ri" && len(op.Operands) == 1:
			intent = op.Operands[0]
		case op.Operator == "BI" && len(op.Operands) == 1:
			if dict, ok := op.Operands[0].(ast.PdfDict); ok {
				intent = dict["Intent"]
			}
		}
		if name, ok := intent.(ast.PdfName); ok && !isValidRenderingIntent(string(name)) {
			bad = append(bad, name)
		}
	}
	return bad
}

// TestFixAggressive_RepairsAStreamHoldingAnInlineImage pins the aggressive
// content rewrite of a page whose content holds an inline image: each repair
// is written in place of the operation it changes, and every other byte, the
// image's included, is carried across. The rewrite used to leave such a
// stream as written, since serializing the parsed operations drops an inline
// image's data (ISO 32000-1, 8.9.7), so an invalid rendering intent in it
// stayed a residual (ISO 19005-2, 6.2.6 t1).
func TestFixAggressive_RepairsAStreamHoldingAnInlineImage(t *testing.T) {
	t.Parallel()

	img := spliceImage
	for _, tc := range []struct {
		name, data, want string
		violations       []compliance.Violation
		fixes            []string
	}{
		{
			name:       "invalid intent",
			data:       "q /Foo ri " + img + " Q",
			want:       "q /RelativeColorimetric ri\n " + img + " Q",
			violations: aggressiveIntent,
			fixes:      []string{intentReport},
		},
		{
			name:       "invalid intent after the image",
			data:       img + " /Foo ri 0 0 m",
			want:       img + "  /RelativeColorimetric ri\n 0 0 m",
			violations: aggressiveIntent,
			fixes:      []string{intentReport},
		},
		{
			name:       "invalid intent and an undefined operator",
			data:       "q /Foo ri " + img + " Q zz",
			want:       "q /RelativeColorimetric ri\n " + img + " Q ",
			violations: append(slices.Clone(aggressiveIntent), aggressiveUndefinedOperator...),
			fixes:      []string{intentReport, "removed 1 unknown operators from BX/EX sections"},
		},
		{
			name:       "invalid intent of an image",
			data:       "q BI /W 4 /H 1 /BPC 8 /CS /G /Intent /Custom" + spliceImageData + " Q",
			want:       "q BI /W 4 /H 1 /BPC 8 /CS /G /Intent /RelativeColorimetric" + spliceImageData + " Q",
			violations: aggressiveIntent,
			fixes:      []string{intentReport},
		},
		{
			name:       "invalid intent of an image, spelled with an escape",
			data:       "q BI /Intent /Cus#74om /W 4 /H 1 /BPC 8 /CS /G" + spliceImageData + " Q",
			want:       "q BI /Intent /RelativeColorimetric /W 4 /H 1 /BPC 8 /CS /G" + spliceImageData + " Q",
			violations: aggressiveIntent,
			fixes:      []string{intentReport},
		},
		{
			name:       "invalid intents of an image and of ri",
			data:       "q /Foo ri BI /W 4 /H 1 /BPC 8 /CS /G /Intent /Custom" + spliceImageData + " Q",
			want:       "q /RelativeColorimetric ri\n BI /W 4 /H 1 /BPC 8 /CS /G /Intent /RelativeColorimetric" + spliceImageData + " Q",
			violations: aggressiveIntent,
			fixes:      []string{"replaced 2 invalid rendering intents in content streams"},
		},
		{
			name:       "between two images",
			data:       "q " + img + " /Saturation ri /Foo ri " + img + " Q",
			want:       "q " + img + " /Saturation ri /RelativeColorimetric ri\n " + img + " Q",
			violations: aggressiveIntent,
			fixes:      []string{intentReport},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			doc, alloc := newTestDoc(t, exponentPagePDF(tc.data, "", "", ""))

			fixes, err := fixAggressive(ctx, alloc, doc, Options{}, tc.violations, "PDF/A-2b")
			if err != nil {
				t.Fatalf("fixAggressive: %v", err)
			}
			if !slices.Equal(fixes, tc.fixes) {
				t.Errorf("fixes = %q, want %q", fixes, tc.fixes)
			}
			got := pendingStreamBytes(t, alloc, 4)
			if string(got) != tc.want {
				t.Errorf("rewrote %q to\n  %q\nwant\n  %q", tc.data, got, tc.want)
			}
			if bytes.Count(got, []byte(spliceImageData)) != strings.Count(tc.data, spliceImageData) {
				t.Errorf("an image did not come across byte for byte: %q", got)
			}
			if bad := invalidIntents(t, got); len(bad) > 0 {
				t.Errorf("validation still reads the intents %v", bad)
			}
		})
	}
}

// TestFixAggressive_LeavesAStreamItCannotSplice pins the residual: a run of
// operators that splits into a BI would begin an inline image no dictionary
// follows, so the stream keeps its bytes, whether or not it holds an image,
// and its invalid intent stays for validation to report rather than being
// counted as replaced.
func TestFixAggressive_LeavesAStreamItCannotSplice(t *testing.T) {
	t.Parallel()
	for _, data := range []string{
		"q /Foo ri QBI zz " + spliceImage + " Q",
		"q /Foo ri QBI zz",
	} {
		ctx := context.Background()
		doc, alloc := newTestDoc(t, exponentPagePDF(data, "", "", ""))

		violations := append(slices.Clone(aggressiveIntent), aggressiveUndefinedOperator...)
		fixes, err := fixAggressive(ctx, alloc, doc, Options{}, violations, "PDF/A-2b")
		if err != nil {
			t.Fatalf("%q: fixAggressive: %v", data, err)
		}
		if len(fixes) != 0 {
			t.Errorf("%q: fixes = %q, want none", data, fixes)
		}
		if _, pending := alloc.Pending()[4]; pending {
			t.Errorf("%q: the page content was rewritten to %q, want it left as written", data, pendingStreamBytes(t, alloc, 4))
		}
	}
}

// TestRespellInlineImageIntents_LeavesADictionaryItMisreads pins the read-back
// of the respelling: a name that follows /Intent as the value of another key
// is no intent, and the dictionary the respelling would write reads as another
// one, so the stream keeps its bytes.
func TestRespellInlineImageIntents_LeavesADictionaryItMisreads(t *testing.T) {
	t.Parallel()
	data := "q BI /W 4 /H 1 /BPC 8 /CS /Intent /Custom /G" + spliceImageData + " Q"
	got, n := respellInlineImageIntents(context.Background(), []byte(data))
	if string(got) != data || n != 0 {
		t.Errorf("respelled %q to %q (%d), want it left as written", data, got, n)
	}
}
