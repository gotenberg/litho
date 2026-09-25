package compliance

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gotenberg/litho/pkg/internal/xsd"
)

// ciiXSDTestSchema declares an Invoice holding one optional token ID, enough
// for the XSD pass to judge a payload it parsed.
const ciiXSDTestSchema = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"
           targetNamespace="urn:t" elementFormDefault="qualified">
  <xs:element name="Invoice">
    <xs:complexType>
      <xs:sequence>
        <xs:element name="ID" type="xs:token" minOccurs="0"/>
      </xs:sequence>
    </xs:complexType>
  </xs:element>
</xs:schema>`

func loadCIIXSDTestSchema(t *testing.T) *xsd.Schema {
	t.Helper()
	s, err := xsd.Load(context.Background(), fstest.MapFS{
		"schema.xsd": &fstest.MapFile{Data: []byte(ciiXSDTestSchema)},
	}, "schema.xsd")
	if err != nil {
		t.Fatalf("loading the test schema: %v", err)
	}
	return s
}

// TestValidateCIIXSDBytes_UnreadableEncoding pins that a payload whose
// encoding declaration XML 1.0, 4.3.3 rejects fails the XSD pass under
// [Rule_cii_xsd_WellFormed], as Mustang fails its schema validation, and
// that WithFacturXLenient leaves the verdict to the spine check.
func TestValidateCIIXSDBytes_UnreadableEncoding(t *testing.T) {
	s := loadCIIXSDTestSchema(t)
	payload := []byte(`<?xml version="1.0" encoding="UTF-8-W/O-BOM"?><Invoice xmlns="urn:t"/>`)

	strict := validateCIIXSDBytes(context.Background(), payload, variantFacturX, s, false)
	if len(strict) != 1 || strict[0].Rule != Rule_cii_xsd_WellFormed {
		t.Fatalf("strict: got %+v, want one Rule_cii_xsd_WellFormed", strict)
	}
	if !strings.Contains(strict[0].Context, "UTF-8-W/O-BOM") {
		t.Errorf("strict: context %q does not name the declared encoding", strict[0].Context)
	}

	if lenient := validateCIIXSDBytes(context.Background(), payload, variantFacturX, s, true); len(lenient) != 0 {
		t.Errorf("lenient: got %+v, want none", lenient)
	}
}

// TestValidateCIIXSDBytes_KeepsViolationsBeforeTheParseFailure pins that a
// payload malformed past a schema violation reports both.
func TestValidateCIIXSDBytes_KeepsViolationsBeforeTheParseFailure(t *testing.T) {
	s := loadCIIXSDTestSchema(t)
	payload := []byte(`<Invoice xmlns="urn:t"><Bogus/><ID>`)

	got := violationRules(validateCIIXSDBytes(context.Background(), payload, variantZUGFeRD10, s, false))
	if !got[Rule_cii_xsd_Structure] || !got[Rule_cii_xsd_WellFormed] {
		t.Errorf("got rules %v, want Rule_cii_xsd_Structure and Rule_cii_xsd_WellFormed", got)
	}
}

// TestValidateCIIXSDBytes_CanceledContext pins that a canceled run reports
// no parse failure: the cancellation is not the payload's.
func TestValidateCIIXSDBytes_CanceledContext(t *testing.T) {
	s := loadCIIXSDTestSchema(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := validateCIIXSDBytes(ctx, []byte(`<Invoice xmlns="urn:t"/>`), variantFacturX, s, false); len(got) != 0 {
		t.Errorf("got %+v, want none", got)
	}
}

// TestValidateCIIXSDBytes_ReadsISO88591 pins that an encoding XML 1.0,
// 4.3.3 allows raises no parse failure in the XSD pass or in the spine.
func TestValidateCIIXSDBytes_ReadsISO88591(t *testing.T) {
	s := loadCIIXSDTestSchema(t)
	payload := []byte(`<?xml version="1.0" encoding="ISO-8859-1"?><Invoice xmlns="urn:t"><ID>Z` + "\xE9" + `</ID></Invoice>`)
	if got := validateCIIXSDBytes(context.Background(), payload, variantFacturX, s, false); len(got) != 0 {
		t.Errorf("XSD pass: got %+v, want none", got)
	}

	spine := strings.Replace(validCII(), `encoding="UTF-8"`, `encoding="ISO-8859-1"`, 1)
	spine = strings.Replace(spine, `Acme Co`, "Acm\xE9 Co", 1)
	if got := validateCIIBytes([]byte(spine), variantFacturX, false); len(got) != 0 {
		t.Errorf("spine: got %+v, want none", got)
	}
}
