package ooxml

import (
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// TestParseXML_ReadsUTF16Parts checks that a part in UTF-16 with its byte
// order mark and a UTF-16 declaration parses (ECMA-376 Part 2, M1.17),
// and that a declaration of another encoding is refused.
func TestParseXML_ReadsUTF16Parts(t *testing.T) {
	t.Parallel()
	doc := `<?xml version="1.0" encoding="UTF-16"?><w:document xmlns:w="` + NSW + `"><w:t>Grüße</w:t></w:document>`
	files := minimalPackage()
	units := []byte{0xFF, 0xFE}
	for _, r := range doc {
		units = append(units, byte(r), byte(r>>8)) //nolint:gosec // the two bytes of a code unit of the BMP
	}
	files["word/document.xml"] = string(units)
	files["word/latin.xml"] = `<?xml version="1.0" encoding="ISO-8859-1"?><a/>`
	p, err := Open(t.Context(), buildZip(t, files))
	if err != nil {
		t.Fatal(err)
	}
	root, err := p.ParseXML(t.Context(), "word/document.xml", nil)
	if err != nil {
		t.Fatalf("UTF-16 part: %v", err)
	}
	if got := root.Find(NSW, "t").Text; got != "Grüße" {
		t.Errorf("text %q, want Grüße", got)
	}
	if _, err := p.ParseXML(t.Context(), "word/latin.xml", nil); !errors.Is(err, office.ErrMalformed) {
		t.Errorf("ISO-8859-1 part: %v, want office.ErrMalformed", err)
	}
}
