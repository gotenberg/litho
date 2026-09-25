package signature

import (
	"fmt"
	"strings"
	"testing"
)

// xmpPacket is an XMP metadata stream (ISO 32000-1, 14.3.2) modified at date.
func xmpPacket(date string) string {
	content := "<x:xmpmeta xmlns:x=\"adobe:ns:meta/\"><xmp:ModifyDate>" + date + "</xmp:ModifyDate></x:xmpmeta>"
	return fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(content), content)
}

// metadataCertifiedPDF is a document certified at the given DocMDP policy whose
// catalog (1) names an XMP metadata stream (8) and whose trailer names an
// information dictionary (9). Its page (3) lists the certification signature
// widget (4, value 5) and a text field widget (6), and draws a content stream
// (10).
func metadataCertifiedPDF(policy int) []byte {
	return buildSigPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 7 0 R /Metadata 8 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 10 0 R /Annots [4 0 R 6 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (cert) /Rect [0 0 0 0] /P 3 0 R /V 5 0 R >>",
		certification(policy),
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /Rect [20 20 120 40] /P 3 0 R >>",
		"<< /Fields [4 0 R 6 0 R] /SigFlags 3 >>",
		xmpPacket("2026-01-01T00:00:00Z"),
		"<< /Producer (Writer) /ModDate (D:20260101000000Z) >>",
		streamObject("BT ET"),
	}, "/Info 9 0 R ")
}

// metadataSave is what a producer rewrites on every incremental save of
// [metadataCertifiedPDF]: the XMP packet and the information dictionary, in
// place.
var metadataSave = []updatedObject{
	{8, xmpPacket("2026-02-01T00:00:00Z")},
	{9, "<< /Producer (Writer; modified using iText) /ModDate (D:20260201000000Z) >>"},
}

// nameFilled is the text field of [metadataCertifiedPDF] filled in.
var nameFilled = updatedObject{6, "<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /Rect [20 20 120 40] /P 3 0 R /V (Jane) >>"}

// savedWithInfo appends an update defining objs to base whose trailer keeps
// naming the information dictionary, as ISO 32000-1, 7.5.6 requires of every
// update's trailer.
func savedWithInfo(t *testing.T, base []byte, objs ...updatedObject) []byte {
	t.Helper()
	return retrailered(t, appendUpdate(t, base, objs...), "/Root 1 0 R /Info 9 0 R")
}

// TestEvaluateMDP_Metadata covers the document metadata, the information
// dictionary (ISO 32000-1, 14.3.3) and the XMP stream (14.3.2), under each P
// of 12.8.2.2, Table 254 and ISO 32000-2, Table 257. An update that makes a
// change the policy permits may rewrite both in place, as producers do on
// every save: an approval signature, a form fill or added validation data.
// An update that changes only the metadata, or points the trailer or the
// catalog at other metadata, changes nothing a policy permits.
func TestEvaluateMDP_Metadata(t *testing.T) {
	dss := dssUpdate("/Metadata 8 0 R ")

	tests := []struct {
		name     string
		policy   int
		update   func(t *testing.T, base []byte) []byte
		want     bool
		reported string // what MDPInfo names when the verdict is a violation
	}{
		{"P=2 form fill rewrites the metadata", 2, func(t *testing.T, base []byte) []byte {
			return savedWithInfo(t, base, append([]updatedObject{nameFilled}, metadataSave...)...)
		}, true, ""},
		{"P=3 form fill rewrites the metadata", 3, func(t *testing.T, base []byte) []byte {
			return savedWithInfo(t, base, append([]updatedObject{nameFilled}, metadataSave...)...)
		}, true, ""},
		{"P=1 validation data rewrites the metadata", 1, func(t *testing.T, base []byte) []byte {
			return savedWithInfo(t, base, append(dss, metadataSave...)...)
		}, true, ""},
		{"P=2 two form fills each rewrite the metadata", 2, func(t *testing.T, base []byte) []byte {
			once := savedWithInfo(t, base, append([]updatedObject{nameFilled}, metadataSave...)...)
			return savedWithInfo(t, once,
				updatedObject{6, "<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /Rect [20 20 120 40] /P 3 0 R /V (John) >>"},
				updatedObject{8, xmpPacket("2026-03-01T00:00:00Z")},
				updatedObject{9, "<< /Producer (Writer) /ModDate (D:20260301000000Z) >>"},
			)
		}, true, ""},

		{"P=1 metadata rewritten alone", 1, func(t *testing.T, base []byte) []byte {
			return savedWithInfo(t, base, metadataSave...)
		}, false, "added after signing"},
		{"P=2 information dictionary rewritten alone", 2, func(t *testing.T, base []byte) []byte {
			return savedWithInfo(t, base, metadataSave[1])
		}, false, "object 9 (untyped) is reached through the trailer's /Info"},
		{"P=2 XMP packet rewritten alone", 2, func(t *testing.T, base []byte) []byte {
			return savedWithInfo(t, base, metadataSave[0])
		}, false, "object 8 (Metadata) is not a form field or signature"},
		{"P=3 metadata rewritten alone", 3, func(t *testing.T, base []byte) []byte {
			return savedWithInfo(t, base, metadataSave...)
		}, false, "object 8 (Metadata)"},
		{"P=2 metadata rewritten in an update of its own after a form fill", 2, func(t *testing.T, base []byte) []byte {
			return savedWithInfo(t, savedWithInfo(t, base, nameFilled), metadataSave...)
		}, false, "object 8 (Metadata)"},
		{"P=2 form fill replaces the XMP packet", 2, func(t *testing.T, base []byte) []byte {
			return savedWithInfo(t, base, nameFilled,
				updatedObject{1, "<< /Type /Catalog /Pages 2 0 R /AcroForm 7 0 R /Metadata 40 0 R >>"},
				updatedObject{40, xmpPacket("2026-02-01T00:00:00Z")},
			)
		}, false, "object 40 (Metadata) is not a form field or signature"},
		{"P=2 form fill points the trailer at another information dictionary", 2, func(t *testing.T, base []byte) []byte {
			return retrailered(t, appendUpdate(t, base, nameFilled,
				updatedObject{40, "<< /Title (Paid in full) /ModDate (D:20260201000000Z) >>"},
			), "/Root 1 0 R /Info 40 0 R")
		}, false, "object 40 (untyped) is reached through the trailer's /Info"},
		{"P=1 form fill rewrites the metadata", 1, func(t *testing.T, base []byte) []byte {
			return savedWithInfo(t, base, append([]updatedObject{nameFilled}, metadataSave...)...)
		}, false, "object 6 (Annot:Widget) added after signing"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := metadataCertifiedPDF(tc.policy)
			result := mdpVerdict(t, tc.update(t, base), len(base), tc.policy)
			if result.MDPValid != tc.want {
				t.Fatalf("MDPValid = %v, want %v (MDPInfo %q)", result.MDPValid, tc.want, result.MDPInfo)
			}
			if tc.reported != "" && !strings.Contains(result.MDPInfo, tc.reported) {
				t.Errorf("MDPInfo %q does not name %s", result.MDPInfo, tc.reported)
			}
			if strings.Contains(result.MDPInfo, "not recognized") || strings.Contains(result.MDPInfo, "not checked") {
				t.Errorf("MDPInfo %q: a check did not run", result.MDPInfo)
			}
		})
	}
}

// bareCertifiedPDF is [metadataCertifiedPDF] whose catalog names no XMP
// metadata stream and whose trailer names no information dictionary. Objects 8
// and 9 are still defined, and nothing refers to them.
func bareCertifiedPDF(policy int) []byte {
	return buildSigPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 7 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 10 0 R /Annots [4 0 R 6 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (cert) /Rect [0 0 0 0] /P 3 0 R /V 5 0 R >>",
		certification(policy),
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /Rect [20 20 120 40] /P 3 0 R >>",
		"<< /Fields [4 0 R 6 0 R] /SigFlags 3 >>",
		xmpPacket("2026-01-01T00:00:00Z"),
		"<< /Producer (Writer) /ModDate (D:20260101000000Z) >>",
		streamObject("BT ET"),
	}, "")
}

// TestEvaluateMDP_AddedMetadata covers the document metadata an update adds
// where the signed revision had none, under each P of ISO 32000-1, 12.8.2.2,
// Table 254 and ISO 32000-2, Table 257. A producer that saves a permitted
// change may add an information dictionary (14.3.3) and an XMP stream
// (14.3.2) in new objects, as it rewrites them where they exist. Metadata
// added on its own, or in an object the signed document already held, is not
// part of such a change.
func TestEvaluateMDP_AddedMetadata(t *testing.T) {
	info := updatedObject{40, "<< /Producer (Writer; modified using iText) /ModDate (D:20260201000000Z) >>"}
	xmp := updatedObject{41, xmpPacket("2026-02-01T00:00:00Z")}
	withXMP := certifiedCatalog(" /Metadata 41 0 R")
	withInfo := func(t *testing.T, data []byte) []byte {
		t.Helper()
		return retrailered(t, data, "/Root 1 0 R /Info 40 0 R")
	}

	tests := []struct {
		name     string
		policy   int
		update   func(t *testing.T, base []byte) []byte
		want     bool
		reported string // what MDPInfo names when the verdict is a violation
	}{
		{"P=2 form fill adds an information dictionary", 2, func(t *testing.T, base []byte) []byte {
			return withInfo(t, appendUpdate(t, base, nameFilled, info))
		}, true, ""},
		{"P=2 form fill adds an XMP stream", 2, func(t *testing.T, base []byte) []byte {
			return appendUpdate(t, base, nameFilled, withXMP, xmp)
		}, true, ""},
		{"P=3 form fill adds both", 3, func(t *testing.T, base []byte) []byte {
			return withInfo(t, appendUpdate(t, base, nameFilled, withXMP, xmp, info))
		}, true, ""},
		{"P=1 validation data adds both", 1, func(t *testing.T, base []byte) []byte {
			return withInfo(t, appendUpdate(t, base, append(dssUpdate("/Metadata 41 0 R "), xmp, info)...))
		}, true, ""},
		{"P=2 a later fill rewrites the added metadata", 2, func(t *testing.T, base []byte) []byte {
			once := withInfo(t, appendUpdate(t, base, nameFilled, withXMP, xmp, info))
			return withInfo(t, appendUpdate(t, once,
				updatedObject{6, "<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /Rect [20 20 120 40] /P 3 0 R /V (John) >>"},
				updatedObject{41, xmpPacket("2026-03-01T00:00:00Z")},
				updatedObject{40, "<< /Producer (Writer) /ModDate (D:20260301000000Z) >>"},
			))
		}, true, ""},

		{"P=2 information dictionary added alone", 2, func(t *testing.T, base []byte) []byte {
			return withInfo(t, appendUpdate(t, base, info))
		}, false, "object 40 (untyped) is reached through the trailer's /Info"},
		{"P=2 XMP stream added alone", 2, func(t *testing.T, base []byte) []byte {
			return appendUpdate(t, base, withXMP, xmp)
		}, false, "object 41 (Metadata) is not a form field or signature"},
		{"P=2 information dictionary added in an object the signed revision holds", 2, func(t *testing.T, base []byte) []byte {
			return retrailered(t, appendUpdate(t, base, nameFilled,
				updatedObject{9, "<< /Title (Paid in full) /ModDate (D:20260201000000Z) >>"},
			), "/Root 1 0 R /Info 9 0 R")
		}, false, "object 9 (untyped) is reached through the trailer's /Info"},
		{"P=2 XMP stream added in an object the signed revision holds", 2, func(t *testing.T, base []byte) []byte {
			return appendUpdate(t, base, nameFilled, certifiedCatalog(" /Metadata 8 0 R"), updatedObject{8, xmpPacket("2026-02-01T00:00:00Z")})
		}, false, "object 8 (Metadata) is not a form field or signature"},
		{"P=1 form fill adds both", 1, func(t *testing.T, base []byte) []byte {
			return withInfo(t, appendUpdate(t, base, nameFilled, withXMP, xmp, info))
		}, false, "object 6 (Annot:Widget) added after signing"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := bareCertifiedPDF(tc.policy)
			result := mdpVerdict(t, tc.update(t, base), len(base), tc.policy)
			if result.MDPValid != tc.want {
				t.Fatalf("MDPValid = %v, want %v (MDPInfo %q)", result.MDPValid, tc.want, result.MDPInfo)
			}
			if tc.reported != "" && !strings.Contains(result.MDPInfo, tc.reported) {
				t.Errorf("MDPInfo %q does not name %s", result.MDPInfo, tc.reported)
			}
			if strings.Contains(result.MDPInfo, "not recognized") || strings.Contains(result.MDPInfo, "not checked") {
				t.Errorf("MDPInfo %q: a check did not run", result.MDPInfo)
			}
		})
	}
}
