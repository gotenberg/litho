package schematron

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
)

const invoiceRulesSch = `<?xml version="1.0" encoding="UTF-8"?>
<sch:schema xmlns:sch="http://purl.oclc.org/dsdl/schematron" queryBinding="xslt2">
  <sch:ns prefix="inv" uri="urn:example:invoice"/>
  <sch:pattern id="headers">
    <sch:rule context="//inv:Invoice">
      <sch:assert id="R1" test="inv:ID">[R1] Invoice must have an ID</sch:assert>
      <sch:assert id="R2" test="inv:Date">[R2] Invoice must have a Date</sch:assert>
      <sch:report id="R3" test="inv:DraftFlag">[R3] Invoice is in draft state</sch:report>
    </sch:rule>
  </sch:pattern>
</sch:schema>`

func loadInline(t *testing.T, body string) *Schema {
	t.Helper()
	fsys := fstest.MapFS{"s.sch": &fstest.MapFile{Data: []byte(body)}}
	s, err := Load(context.Background(), fsys, "s.sch")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return s
}

func TestValidate_HappyPath(t *testing.T) {
	s := loadInline(t, invoiceRulesSch)
	xmlBytes := []byte(`<?xml version="1.0"?>
<inv:Invoice xmlns:inv="urn:example:invoice">
  <inv:ID>INV-001</inv:ID>
  <inv:Date>2026-05-18</inv:Date>
</inv:Invoice>`)
	got, _, err := Validate(context.Background(), s, xmlBytes)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d violations, want 0: %v", len(got), got)
	}
}

func TestValidate_AssertFires(t *testing.T) {
	s := loadInline(t, invoiceRulesSch)
	xmlBytes := []byte(`<?xml version="1.0"?>
<inv:Invoice xmlns:inv="urn:example:invoice">
  <inv:Date>2026-05-18</inv:Date>
</inv:Invoice>`)
	got, _, err := Validate(context.Background(), s, xmlBytes)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d violations, want 1: %v", len(got), got)
	}
	if got[0].RuleID != "R1" {
		t.Errorf("RuleID: got %q, want R1", got[0].RuleID)
	}
	if got[0].Negate {
		t.Errorf("R1 is an assert, Negate should be false")
	}
}

func TestValidate_ReportFires(t *testing.T) {
	s := loadInline(t, invoiceRulesSch)
	xmlBytes := []byte(`<?xml version="1.0"?>
<inv:Invoice xmlns:inv="urn:example:invoice">
  <inv:ID>INV-001</inv:ID>
  <inv:Date>2026-05-18</inv:Date>
  <inv:DraftFlag/>
</inv:Invoice>`)
	got, _, err := Validate(context.Background(), s, xmlBytes)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(got) != 1 || got[0].RuleID != "R3" {
		t.Fatalf("got %v, want one R3 violation", got)
	}
	if !got[0].Negate {
		t.Errorf("R3 is a report, Negate should be true")
	}
}

const phaseSch = `<?xml version="1.0" encoding="UTF-8"?>
<sch:schema xmlns:sch="http://purl.oclc.org/dsdl/schematron">
  <sch:ns prefix="inv" uri="urn:example:invoice"/>
  <sch:phase id="basic">
    <sch:active pattern="P-headers"/>
  </sch:phase>
  <sch:phase id="extended">
    <sch:active pattern="P-headers"/>
    <sch:active pattern="P-totals"/>
  </sch:phase>
  <sch:pattern id="P-headers">
    <sch:rule context="//inv:Invoice">
      <sch:assert id="H1" test="inv:ID">missing ID</sch:assert>
    </sch:rule>
  </sch:pattern>
  <sch:pattern id="P-totals">
    <sch:rule context="//inv:Invoice">
      <sch:assert id="T1" test="inv:Total">missing Total</sch:assert>
    </sch:rule>
  </sch:pattern>
</sch:schema>`

func TestValidate_WithPhase(t *testing.T) {
	s := loadInline(t, phaseSch)
	xmlBytes := []byte(`<?xml version="1.0"?>
<inv:Invoice xmlns:inv="urn:example:invoice">
  <inv:ID>INV-001</inv:ID>
</inv:Invoice>`)
	// "basic" only fires P-headers; Total absence does not trip.
	basic, _, err := Validate(context.Background(), s, xmlBytes, WithPhase("basic"))
	if err != nil {
		t.Fatalf("Validate basic: %v", err)
	}
	if len(basic) != 0 {
		t.Errorf("basic phase: got %d violations, want 0: %v", len(basic), basic)
	}
	// "extended" also fires P-totals, which complains about Total.
	extended, _, err := Validate(context.Background(), s, xmlBytes, WithPhase("extended"))
	if err != nil {
		t.Fatalf("Validate extended: %v", err)
	}
	if len(extended) != 1 || extended[0].RuleID != "T1" {
		t.Errorf("extended phase: got %v, want one T1", extended)
	}
}

func TestValidate_UnknownPhase(t *testing.T) {
	s := loadInline(t, phaseSch)
	_, _, err := Validate(context.Background(), s, []byte(`<root/>`), WithPhase("ghost"))
	if err == nil || !strings.Contains(err.Error(), "phase \"ghost\"") {
		t.Errorf("err: got %v, want phase-not-declared", err)
	}
}

func TestValidate_NilSchema(t *testing.T) {
	_, _, err := Validate(context.Background(), nil, []byte(`<root/>`))
	if err == nil {
		t.Error("expected error for nil schema")
	}
}

func TestValidate_MalformedXML(t *testing.T) {
	s := loadInline(t, invoiceRulesSch)
	_, _, err := Validate(context.Background(), s, []byte(`<oops`))
	if err == nil {
		t.Error("expected parse error")
	}
}

func TestValidate_FirstMatchingRuleClaims(t *testing.T) {
	// Two rules with overlapping contexts: per the spec, the first
	// rule that matches a node claims it; later rules in the same
	// pattern skip that node.
	sch := `<?xml version="1.0" encoding="UTF-8"?>
<sch:schema xmlns:sch="http://purl.oclc.org/dsdl/schematron">
  <sch:ns prefix="inv" uri="urn:example:invoice"/>
  <sch:pattern id="P">
    <sch:rule context="//inv:Invoice">
      <sch:assert id="FIRST" test="false()">first rule fired</sch:assert>
    </sch:rule>
    <sch:rule context="//inv:Invoice">
      <sch:assert id="SECOND" test="false()">second rule fired</sch:assert>
    </sch:rule>
  </sch:pattern>
</sch:schema>`
	s := loadInline(t, sch)
	got, _, err := Validate(context.Background(), s,
		[]byte(`<inv:Invoice xmlns:inv="urn:example:invoice"/>`))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(got) != 1 || got[0].RuleID != "FIRST" {
		t.Errorf("got %v, want one FIRST violation (second rule should be claimed-skipped)", got)
	}
}

func TestValidate_SurfacesSkippedRules(t *testing.T) {
	// tokenize() is XPath 2.0 and not covered by the rewriter; the
	// rule should land in the skipped slice rather than aborting
	// the run or being silently absent.
	sch := `<?xml version="1.0" encoding="UTF-8"?>
<sch:schema xmlns:sch="http://purl.oclc.org/dsdl/schematron">
  <sch:ns prefix="inv" uri="urn:example:invoice"/>
  <sch:pattern id="P">
    <sch:rule context="//inv:Invoice">
      <sch:assert id="SKIPME" test="count(tokenize(inv:Tags, ',')) &gt; 0">tags required</sch:assert>
      <sch:assert id="RUNME" test="inv:ID">missing ID</sch:assert>
    </sch:rule>
  </sch:pattern>
</sch:schema>`
	s := loadInline(t, sch)
	xmlBytes := []byte(`<inv:Invoice xmlns:inv="urn:example:invoice"><inv:ID>X</inv:ID></inv:Invoice>`)
	violations, skipped, err := Validate(context.Background(), s, xmlBytes)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("violations: got %d, want 0 (RUNME passes, SKIPME skips)", len(violations))
	}
	if len(skipped) != 1 || skipped[0].RuleID != "SKIPME" {
		t.Errorf("skipped: got %+v, want one SKIPME entry", skipped)
	}
}

func TestValidate_ContextCancel(t *testing.T) {
	s := loadInline(t, invoiceRulesSch)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := Validate(ctx, s, []byte(`<inv:Invoice xmlns:inv="urn:example:invoice"/>`))
	if err == nil {
		t.Error("expected ctx error")
	}
}

// runOne loads sch, validates payload, and fails the test on a load or
// parse error. It returns the fired rule ids and the skipped rules.
func runOne(t *testing.T, sch, payload string, opts ...EvalOption) (fired, skipped []string) {
	t.Helper()
	s := loadInline(t, sch)
	v, sk, err := Validate(context.Background(), s, []byte(payload), opts...)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, x := range v {
		fired = append(fired, x.RuleID)
	}
	for _, x := range sk {
		skipped = append(skipped, x.RuleID+": "+x.Reason)
	}
	return fired, skipped
}

func TestValidate_PhaseLetsApply(t *testing.T) {
	// ISO/IEC 19757-3:2016, 5.4.6: a let in a phase binds for the
	// patterns that phase activates. The name must resolve, not leave an
	// undeclared variable that skips the rule.
	sch := `<?xml version="1.0" encoding="UTF-8"?>
<sch:schema xmlns:sch="http://purl.oclc.org/dsdl/schematron">
  <sch:ns prefix="inv" uri="urn:example:invoice"/>
  <sch:phase id="strict">
    <sch:active pattern="P"/>
    <sch:let name="wanted" value="'EUR'"/>
  </sch:phase>
  <sch:pattern id="P">
    <sch:rule context="//inv:Invoice">
      <sch:assert id="CUR" test="inv:Currency = $wanted">currency</sch:assert>
    </sch:rule>
  </sch:pattern>
</sch:schema>`
	ok := `<inv:Invoice xmlns:inv="urn:example:invoice"><inv:Currency>EUR</inv:Currency></inv:Invoice>`
	bad := `<inv:Invoice xmlns:inv="urn:example:invoice"><inv:Currency>USD</inv:Currency></inv:Invoice>`
	fired, skipped := runOne(t, sch, ok, WithPhase("strict"))
	if len(fired) != 0 || len(skipped) != 0 {
		t.Errorf("EUR: fired %v skipped %v, want neither", fired, skipped)
	}
	fired, skipped = runOne(t, sch, bad, WithPhase("strict"))
	if len(fired) != 1 || len(skipped) != 0 {
		t.Errorf("USD: fired %v skipped %v, want CUR to fire", fired, skipped)
	}
}
