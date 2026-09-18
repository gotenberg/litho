package signature

import "testing"

// duplicateKidSigPDF names the same signature field twice in one /Kids array.
// ISO 32000-1, 12.7.3 describes a field tree, so a kid reached twice is
// malformed, but nothing stops a file from carrying it.
func duplicateKidSigPDF() []byte {
	return buildSigPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Kids [5 0 R 5 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (sig1) /Rect [0 0 10 10] /V 6 0 R >>",
		"<< /Type /Sig /ByteRange [0 100 200 50] /Contents <30820101> /M (D:20240101000000Z) >>",
	}, "")
}

// selfKidSigPDF points a field's /Kids back at the field itself.
func selfKidSigPDF() []byte {
	return buildSigPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /T (loop) /Kids [4 0 R] >>",
	}, "")
}

// TestExtractSignatures_AFieldNamedTwiceIsReportedOnce pins the answer, not
// merely termination.
//
// The walk appends each signature it finds straight into the slice
// [ExtractSignatures] returns, and nothing downstream removes a duplicate:
// Reader.ExtractSignatures hands that slice to the caller as it stands. So a
// /Kids array naming one field twice reported the same signature twice. The
// depth bound cannot catch it, since the tree is two levels deep.
func TestExtractSignatures_AFieldNamedTwiceIsReportedOnce(t *testing.T) {
	t.Parallel()

	names := sigsOf(t, duplicateKidSigPDF())
	if len(names) != 1 {
		t.Fatalf("ExtractSignatures reported %d signatures %v, want exactly one", len(names), names)
	}
	if names[0] != "sig1" {
		t.Errorf("signature field name = %q, want sig1", names[0])
	}
}

// TestExtractSignatures_SelfReferencingKidsTerminates pins termination only. A
// field whose /Kids names itself carries no signature value, so the guarded and
// the unguarded walk both report nothing; this would not have failed before the
// visited set was added.
func TestExtractSignatures_SelfReferencingKidsTerminates(t *testing.T) {
	t.Parallel()

	if names := sigsOf(t, selfKidSigPDF()); len(names) != 0 {
		t.Errorf("ExtractSignatures reported %v, want none", names)
	}
}
