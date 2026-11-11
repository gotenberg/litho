package pdf

import "testing"

// TestReader_PermissionsOfAnUnencryptedDocumentGrantEverything pins that a
// document with no /Encrypt grants every permission: ISO 32000-1, 7.6.3.2
// defines the /P flags only for an encrypted document.
func TestReader_PermissionsOfAnUnencryptedDocumentGrantEverything(t *testing.T) {
	t.Parallel()

	r := openColorPDF(t, minimalPDF())
	if r.IsEncrypted() {
		t.Fatal("precondition: the fixture reports itself encrypted")
	}
	want := Permissions{
		Print:             true,
		ModifyContent:     true,
		ExtractText:       true,
		ModifyAnnotations: true,
		FillForms:         true,
		Assemble:          true,
		PrintHighQuality:  true,
	}
	if got := r.Permissions(); got != want {
		t.Errorf("Permissions() = %+v, want every flag granted", got)
	}
}
