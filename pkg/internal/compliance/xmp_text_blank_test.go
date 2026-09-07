package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// pdfDescription wraps property markup in an rdf:Description binding the pdf
// prefix.
func pdfDescription(body string) []byte {
	return xmpPacket(`<rdf:Description rdf:about=""
      xmlns:pdf="http://ns.adobe.com/pdf/1.3/"
      ` + body + `></rdf:Description>`)
}

// TestXMPTextType_BlankValueAccepted pins that a whitespace-only value
// satisfies the Text type. Per ISO 16684-1, Text is a sequence of Unicode
// characters with no non-blank requirement, so a blank value conforms to the
// type its predefined schema gives it and ISO 19005-2 §6.6.2.3.1 t2 has
// nothing to report. Acrobat Distiller 6.0.1 emits pdf:Keywords=' ' (govdocs
// thread_300_300483.pdf, thread_400_400924.pdf) and veraPDF passes it.
func TestXMPTextType_BlankValueAccepted(t *testing.T) {
	data := pdfDescription(`pdf:Keywords=" "`)

	for _, part := range []int{1, 2, 3} {
		var got []Violation
		xmpCheckPropertyTypes(data, part, ast.PdfStream{}, &got)
		if len(got) != 0 {
			t.Errorf(`pdf:Keywords=" " under PDF/A-%d: got %v, want no violation`, part, got)
		}
	}
}

// TestXMPTextType_BlankContainerItemAccepted pins the same rule for a
// container item: the element type check runs the identical validator, so a
// blank item in a Text container is a conforming value too.
func TestXMPTextType_BlankContainerItemAccepted(t *testing.T) {
	data := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:dc="http://purl.org/dc/elements/1.1/">
      <dc:creator><rdf:Seq><rdf:li> </rdf:li></rdf:Seq></dc:creator>
    </rdf:Description>`)

	var got []Violation
	xmpCheckPropertyTypes(data, 2, ast.PdfStream{}, &got)
	if len(got) != 0 {
		t.Errorf("blank dc:creator item: got %v, want no violation", got)
	}
}

// TestXMPTextType_WrongTypeStillFlagged guards the fix: accepting a blank Text
// value must not weaken the type check for a property whose value genuinely
// does not match. pdfaid:part is an Integer.
func TestXMPTextType_WrongTypeStillFlagged(t *testing.T) {
	data := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:pdfaid="http://www.aiim.org/pdfa/ns/id/"
      pdfaid:part="notanumber"></rdf:Description>`)

	var got []Violation
	xmpCheckPropertyTypes(data, 2, ast.PdfStream{}, &got)
	if len(got) == 0 {
		t.Error(`pdfaid:part="notanumber" is not an Integer, want a violation`)
	}
}
