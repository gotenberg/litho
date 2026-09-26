package signature

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/models"
)

// spawnContent is what the template page of [spawnCertifiedPDF] draws.
const spawnContent = "BT /Helv 12 Tf 50 80 Td (Amount due) Tj ET"

// spawnCertifiedPDF is a document certified at the given DocMDP policy whose
// page (3) lists the certification signature widget (4, value 5), and whose
// name dictionary names, in its /Templates tree (9), an invoice template page
// (6) of the given /Type, drawing content (8) with a resource dictionary (10)
// that names a font (11). The form (7) holds the signature field.
func spawnCertifiedPDF(policy int, typ, content string) []byte {
	return buildSigPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 7 0 R /Names << /Templates 9 0 R >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (cert) /Rect [0 0 0 0] /P 3 0 R /V 5 0 R >>",
		certification(policy),
		"<< /Type /" + typ + " /MediaBox [0 0 612 792] /Contents 8 0 R /Resources 10 0 R >>",
		"<< /Fields [4 0 R] /SigFlags 3 >>",
		streamObject(content),
		"<< /Names [(invoice) 6 0 R] >>",
		"<< /Font << /Helv 11 0 R >> >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}, "")
}

// spawnPage is the update that instantiates the template of
// [spawnCertifiedPDF] on a new page (20) with the given extra entries, and
// objs are more objects it writes.
func spawnPage(extra string, objs ...updatedObject) []updatedObject {
	return append([]updatedObject{
		{2, "<< /Type /Pages /Kids [3 0 R 20 0 R] /Count 2 >>"},
		{20, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]" + extra + " >>"},
	}, objs...)
}

// hexStream is a stream holding data through the ASCIIHexDecode filter.
func hexStream(data string) string {
	encoded := hex.EncodeToString([]byte(data)) + ">"
	return fmt.Sprintf("<< /Length %d /Filter /ASCIIHexDecode >>\nstream\n%s\nendstream", len(encoded), encoded)
}

// flateStream is a stream holding data through the FlateDecode filter, with
// the extra dictionary entries.
func flateStream(t *testing.T, extra, data string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("<<%s /Length %d /Filter /FlateDecode >>\nstream\n%s\nendstream", extra, buf.Len(), buf.String())
}

// wrapperFormEntries are the entries of the form XObject a wrapped spawn paints.
const wrapperFormEntries = " /Type /XObject /Subtype /Form /BBox [0 0 612 792] /Resources 10 0 R"

// formStream is a form XObject holding data, with the given entries.
func formStream(entries, data string) string {
	return fmt.Sprintf("<<%s /Length %d >>\nstream\n%s\nendstream", entries, len(data), data)
}

// wrapped is the update that instantiates the template of [spawnCertifiedPDF]
// on a new page (20) that paints the form XObject form (31) through the
// content stream wrapper (30).
func wrapped(wrapper, form string) []updatedObject {
	return spawnPage(" /Contents 30 0 R /Resources << /XObject << /Fm0 31 0 R >> >>",
		updatedObject{30, streamObject(wrapper)},
		updatedObject{31, form},
	)
}

// TestEvaluateMDP_SpawnPaintsTemplate covers ISO 32000-1, 12.8.2.2, Table
// 254, which permits instantiating page templates from P=2, and 12.7.6, where
// an instantiation adds a template page as a regular page. A copy paints what
// the template paints: its content stored another way (7.3.8.2, Table 5), or
// held by a form XObject the page paints unmoved and unclipped (8.10.1), as
// Acrobat's Template.spawn can write it. A copy that paints anything else is
// no instantiation. The template may be typed /Template, as 12.7.6 has a
// hidden template typed, while its copy is a /Page.
func TestEvaluateMDP_SpawnPaintsTemplate(t *testing.T) {
	const altered = "BT /Helv 12 Tf 50 80 Td (Amount due: 0) Tj ET"
	form := formStream(wrapperFormEntries, spawnContent)

	tests := []struct {
		name   string
		policy int
		update []updatedObject
		want   bool
	}{
		{"P=2 faithful spawn", 2, spawnPage(" /Contents 8 0 R /Resources 10 0 R"), true},
		{"P=2 spawn re-encodes the content in hex", 2, spawnPage(" /Contents 30 0 R /Resources 10 0 R", updatedObject{30, hexStream(spawnContent)}), true},
		{"P=2 spawn copies the template's resources", 2, spawnPage(" /Contents 8 0 R /Resources << /Font << /Helv 12 0 R >> >>",
			updatedObject{12, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"},
		), true},
		{"P=2 spawn compresses the content", 2, spawnPage(" /Contents 30 0 R /Resources 10 0 R", updatedObject{30, flateStream(t, "", spawnContent)}), true},
		{"P=3 spawn compresses the content", 3, spawnPage(" /Contents 30 0 R /Resources 10 0 R", updatedObject{30, flateStream(t, "", spawnContent)}), true},
		{"P=2 spawn wraps the content in a form XObject", 2, wrapped("q /Fm0 Do Q", form), true},
		{"P=3 spawn wraps the content in a form XObject", 3, wrapped("q /Fm0 Do Q", form), true},
		{"P=2 spawn wraps the content in a compressed form XObject", 2, wrapped("q 1 0 0 1 0 0 cm /Fm0 Do Q", flateStream(t, wrapperFormEntries+" /Matrix [1 0 0 1 0 0]", spawnContent)), true},
		{"P=2 two spawns paint one form XObject", 2, []updatedObject{
			{2, "<< /Type /Pages /Kids [3 0 R 20 0 R 21 0 R] /Count 3 >>"},
			{20, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 30 0 R /Resources << /XObject << /Fm0 31 0 R >> >> >>"},
			{21, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 32 0 R /Resources << /XObject << /X 31 0 R >> >> >>"},
			{30, streamObject("q /Fm0 Do Q")},
			{31, form},
			{32, streamObject("/X Do")},
		}, true},

		{"P=1 spawn wraps the content in a form XObject", 1, wrapped("q /Fm0 Do Q", form), false},
		{"P=2 spawn compresses other content", 2, spawnPage(" /Contents 30 0 R /Resources 10 0 R", updatedObject{30, flateStream(t, "", altered)}), false},
		{"P=2 spawn re-encodes the content with other resources", 2, spawnPage(" /Contents 30 0 R /Resources << /Font << /Helv 12 0 R >> >>",
			updatedObject{30, hexStream(spawnContent)},
			updatedObject{12, "<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>"},
		), false},
		{"P=2 form XObject holds other content", 2, wrapped("q /Fm0 Do Q", formStream(wrapperFormEntries, altered)), false},
		{"P=2 form XObject draws with other resources", 2, wrapped("q /Fm0 Do Q", formStream(strings.Replace(wrapperFormEntries, "10 0 R", "<< /Font << /Helv << /Type /Font /Subtype /Type1 /BaseFont /Courier >> >> >>", 1), spawnContent)), false},
		{"P=2 form XObject moves the content", 2, wrapped("q /Fm0 Do Q", formStream(wrapperFormEntries+" /Matrix [1 0 0 1 0 100]", spawnContent)), false},
		{"P=2 form XObject clips the page", 2, wrapped("q /Fm0 Do Q", formStream(strings.Replace(wrapperFormEntries, "[0 0 612 792]", "[0 0 300 300]", 1), spawnContent)), false},
		{"P=2 form XObject is optional content", 2, append(wrapped("q /Fm0 Do Q", formStream(wrapperFormEntries+" /OC 40 0 R", spawnContent)),
			updatedObject{40, "<< /Type /OCG /Name (hidden) >>"}), false},
		{"P=2 form XObject is a transparency group", 2, wrapped("q /Fm0 Do Q", formStream(wrapperFormEntries+" /Group << /S /Transparency >>", spawnContent)), false},
		{"P=2 wrapper names another resource", 2, spawnPage(" /Contents 30 0 R /Resources << /XObject << /Fm0 31 0 R >> /Font << /F 12 0 R >> >>",
			updatedObject{30, streamObject("q /Fm0 Do Q")},
			updatedObject{31, form},
			updatedObject{12, "<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>"},
		), false},
		{"P=2 wrapper moves the form XObject", 2, wrapped("q 1 0 0 1 0 100 cm /Fm0 Do Q", form), false},
		{"P=2 wrapper paints more than the form XObject", 2, wrapped("q /Fm0 Do Q BT /Helv 12 Tf 50 60 Td (Paid) Tj ET", form), false},
		{"P=2 wrapper paints the form XObject twice", 2, wrapped("q /Fm0 Do Q q /Fm0 Do Q", form), false},
		{"P=2 wrapper sets a graphics state", 2, wrapped("0.5 g q /Fm0 Do Q", form), false},
	}
	for _, typ := range []string{"Page", "Template"} {
		for _, tc := range tests {
			t.Run(typ+"/"+tc.name, func(t *testing.T) {
				base := spawnCertifiedPDF(tc.policy, typ, spawnContent)
				result := mdpVerdict(t, appendUpdate(t, base, tc.update...), len(base), tc.policy)
				if result.MDPValid != tc.want {
					t.Fatalf("MDPValid = %v, want %v (MDPInfo %q)", result.MDPValid, tc.want, result.MDPInfo)
				}
				if !tc.want && !strings.Contains(result.MDPInfo, fmt.Sprintf("P=%d violation", tc.policy)) {
					t.Errorf("MDPInfo %q names no violation", result.MDPInfo)
				}
				if strings.Contains(result.MDPInfo, "not checked") {
					t.Errorf("MDPInfo %q: a comparison did not run", result.MDPInfo)
				}
			})
		}
	}
}

// TestEvaluateMDP_SpawnDecodeBudget pins the stream budget the comparison of a
// re-encoded spawn decodes under (limits.Limits.MaxStreamBytes): a copy whose
// content decodes past it cannot be told from the template's, so the page it
// adds is reported, while the default budget compares it.
func TestEvaluateMDP_SpawnDecodeBudget(t *testing.T) {
	large := strings.Repeat(spawnContent+"\n", 200)
	base := spawnCertifiedPDF(2, "Template", large)
	data := appendUpdate(t, base, spawnPage(" /Contents 30 0 R /Resources 10 0 R", updatedObject{30, flateStream(t, "", large)})...)

	for _, tc := range []struct {
		name string
		lim  limits.Limits
		want bool
	}{
		{"default budget", limits.Limits{}, true},
		{"budget below the content", limits.Limits{MaxStreamBytes: 4 << 10}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil, document.WithLimits(tc.lim))
			if err != nil {
				t.Fatalf("NewDocument: %v", err)
			}
			var result models.SignatureValidation
			evaluateMDP(ctx, doc, models.Signature{MDPPolicy: 2, ByteRange: [4]int64{0, 1, 1, int64(len(base)) - 1}}, &result)
			if result.MDPValid != tc.want {
				t.Fatalf("MDPValid = %v, want %v (MDPInfo %q)", result.MDPValid, tc.want, result.MDPInfo)
			}
		})
	}
}
