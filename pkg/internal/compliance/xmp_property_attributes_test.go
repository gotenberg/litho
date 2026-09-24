package compliance

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// structComplaint reports whether any violation says the property carried text
// where a struct belongs.
func structComplaint(got []Violation) bool {
	for _, v := range got {
		if strings.Contains(v.Context, "plain text value instead of sub-elements") {
			return true
		}
	}
	return false
}

// TestXMPPropertyAttributes_AreAStruct pins the RDF/XML property-attribute
// shorthand (RDF 1.1 XML Syntax, 2.5): a property element whose fields are
// written as namespace-qualified attributes holds a struct, exactly as a nested
// Description would.
//
// # Corpus evidence
//
//   - govdocs thread_300_300116.pdf: writes <exif:Flash exif:Fired="False"
//     .../> in one packet and the same struct with sub-elements in another.
//     litho read the first as plain text and reported a value-type violation
//     for it; veraPDF, whose own SchemasDefinition types exif:Flash as
//     "flash", reports none.
func TestXMPPropertyAttributes_AreAStruct(t *testing.T) {
	t.Parallel()

	data := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:exif="http://ns.adobe.com/exif/1.0/">
      <exif:Flash exif:Fired="False" exif:Return="0" exif:Mode="3"
                  exif:Function="False" exif:RedEyeMode="False"/>
    </rdf:Description>`)

	var got []Violation
	xmpCheckPropertyTypes(data, 2, ast.PdfStream{}, nil, &got)
	if structComplaint(got) {
		t.Errorf("property attributes must read as a struct, got %v", got)
	}
}

// TestXMPPropertyAttributes_PlainTextStillReports pins the negative: the same
// property written with a text value is still a text value, and the shorthand
// must not excuse it.
func TestXMPPropertyAttributes_PlainTextStillReports(t *testing.T) {
	t.Parallel()

	data := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:exif="http://ns.adobe.com/exif/1.0/">
      <exif:Flash>0</exif:Flash>
    </rdf:Description>`)

	var got []Violation
	xmpCheckPropertyTypes(data, 2, ast.PdfStream{}, nil, &got)
	if !structComplaint(got) {
		t.Errorf("a text value for a struct property must still report, got %v", got)
	}
}

// TestXMPPropertyAttributes_FieldsAreStillAudited pins the other half of the
// shorthand: the attributes are the struct's fields, so the value-type field
// audit reads them there too. Reading the shorthand as a struct without
// auditing its attributes would silence an undefined field.
//
// # Corpus evidence
//
//   - pdfjs issue13971.pdf: writes <xmpMM:DerivedFrom
//     stRef:originalDocumentID=".."/>, and veraPDF reports the field the 2005
//     ResourceRef does not define.
func TestXMPPropertyAttributes_FieldsAreStillAudited(t *testing.T) {
	t.Parallel()

	data := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:xmpMM="http://ns.adobe.com/xap/1.0/mm/"
      xmlns:stRef="http://ns.adobe.com/xap/1.0/sType/ResourceRef#">
      <xmpMM:DerivedFrom stRef:instanceID="xmp.iid:1"
                         stRef:originalDocumentID="5F5D5233"/>
    </rdf:Description>`)

	var got []Violation
	xmpCheckStructFieldNames(data, 2, &got)
	if !hasRule(got, Rule_19005_2_6_6_2_3_1_t02) {
		t.Errorf("an undefined field written as an attribute must report, got %v", got)
	}
}

// TestXMPPropertyAttributes_FieldsInsideAnArrayAreAudited pins that a struct
// written with property attributes is read wherever its value type puts it. An
// xmpMM:History is a Seq of ResourceEvent, so each event is an rdf:li three
// levels down, and Adobe writes its fields as attributes. Auditing only the
// property element leaves such a field silent.
//
// # Corpus evidence
//
//   - pdfjs issue13971.pdf: carries stEvt:changed there, a field the 2005
//     ResourceEvent does not define, and veraPDF reports it against
//     xmpMM:History.
func TestXMPPropertyAttributes_FieldsInsideAnArrayAreAudited(t *testing.T) {
	t.Parallel()

	data := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:xmpMM="http://ns.adobe.com/xap/1.0/mm/"
      xmlns:stEvt="http://ns.adobe.com/xap/1.0/sType/ResourceEvent#">
      <xmpMM:History>
        <rdf:Seq>
          <rdf:li stEvt:action="saved" stEvt:instanceID="xmp.iid:1"
                  stEvt:when="2020-06-29T10:57:51+02:00" stEvt:changed="/metadata"/>
        </rdf:Seq>
      </xmpMM:History>
    </rdf:Description>`)

	var got []Violation
	xmpCheckStructFieldNames(data, 2, &got)
	if !hasRule(got, Rule_19005_2_6_6_2_3_1_t02) {
		t.Errorf("an undefined field on an array item must report, got %v", got)
	}
}

// TestXMPPropertyAttributes_DefinedArrayFieldsStaySilent pins the negative for
// the same shape: an event limited to the fields the 2005 ResourceEvent defines
// reports nothing.
func TestXMPPropertyAttributes_DefinedArrayFieldsStaySilent(t *testing.T) {
	t.Parallel()

	data := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:xmpMM="http://ns.adobe.com/xap/1.0/mm/"
      xmlns:stEvt="http://ns.adobe.com/xap/1.0/sType/ResourceEvent#">
      <xmpMM:History>
        <rdf:Seq>
          <rdf:li stEvt:action="saved" stEvt:instanceID="xmp.iid:1"
                  stEvt:when="2020-06-29T10:57:51+02:00"
                  stEvt:softwareAgent="Acrobat"/>
        </rdf:Seq>
      </xmpMM:History>
    </rdf:Description>`)

	var got []Violation
	xmpCheckStructFieldNames(data, 2, &got)
	if len(got) != 0 {
		t.Errorf("defined event fields must stay silent, got %v", got)
	}
}

// TestXMPPropertyAttributes_DefinedFieldsStaySilent pins that the attribute form
// of a fully defined struct reports nothing.
func TestXMPPropertyAttributes_DefinedFieldsStaySilent(t *testing.T) {
	t.Parallel()

	data := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:xmpMM="http://ns.adobe.com/xap/1.0/mm/"
      xmlns:stRef="http://ns.adobe.com/xap/1.0/sType/ResourceRef#">
      <xmpMM:DerivedFrom stRef:instanceID="xmp.iid:1" stRef:documentID="xmp.did:1"/>
    </rdf:Description>`)

	var got []Violation
	xmpCheckStructFieldNames(data, 2, &got)
	if len(got) != 0 {
		t.Errorf("defined fields written as attributes must stay silent, got %v", got)
	}
}

// TestXMPPropertyAttributes_RDFAndXMLAttributesAreSyntax pins the boundary. The
// RDF and xml namespaces carry syntax, not fields, so a simple property keeps
// its simple value whatever they say about it.
func TestXMPPropertyAttributes_RDFAndXMLAttributesAreSyntax(t *testing.T) {
	t.Parallel()

	data := xmpPacket(`<rdf:Description rdf:about=""
      xmlns:exif="http://ns.adobe.com/exif/1.0/">
      <exif:Flash rdf:resource="http://example.com/x" xml:lang="x-default">0</exif:Flash>
    </rdf:Description>`)

	var got []Violation
	xmpCheckPropertyTypes(data, 2, ast.PdfStream{}, nil, &got)
	if !structComplaint(got) {
		t.Errorf("RDF and xml attributes must not turn a text value into a struct, got %v", got)
	}
}
