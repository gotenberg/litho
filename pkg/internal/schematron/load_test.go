package schematron

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"
)

const en16931ish = `<?xml version="1.0" encoding="UTF-8"?>
<sch:schema xmlns:sch="http://purl.oclc.org/dsdl/schematron" queryBinding="xslt2">
  <sch:title>EN 16931 CII validation</sch:title>
  <sch:ns prefix="rsm" uri="urn:un:unece:uncefact:data:standard:CrossIndustryInvoice:100"/>
  <sch:ns prefix="ram" uri="urn:un:unece:uncefact:data:standard:ReusableAggregateBusinessInformationEntity:100"/>
  <sch:let name="threshold" value="0"/>
  <sch:phase id="business-rules">
    <sch:active pattern="CII-headers"/>
    <sch:active pattern="CII-totals"/>
  </sch:phase>
  <sch:pattern id="CII-headers">
    <sch:title>Header rules</sch:title>
    <sch:rule context="//rsm:ExchangedDocument">
      <sch:assert id="BR-CO-25" flag="fatal" test="ram:ID">[BR-CO-25] An invoice shall have an Invoice number</sch:assert>
      <sch:assert id="BR-CO-26" test="ram:IssueDateTime">[BR-CO-26] Invoice issue date is required</sch:assert>
    </sch:rule>
  </sch:pattern>
  <sch:pattern id="CII-totals">
    <sch:rule context="//ram:ApplicableHeaderTradeSettlement">
      <sch:report id="BR-DE-01" test="not(ram:InvoiceCurrencyCode)">[BR-DE-01] Currency required</sch:report>
    </sch:rule>
  </sch:pattern>
</sch:schema>`

func TestLoad_HappyPath(t *testing.T) {
	fsys := fstest.MapFS{"s.sch": &fstest.MapFile{Data: []byte(en16931ish)}}
	s, err := Load(context.Background(), fsys, "s.sch")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.QueryBinding != "xslt2" {
		t.Errorf("QueryBinding: got %q, want %q", s.QueryBinding, "xslt2")
	}
	if s.Title != "EN 16931 CII validation" {
		t.Errorf("Title: got %q", s.Title)
	}
	if len(s.Namespaces) != 2 {
		t.Errorf("Namespaces: got %d, want 2", len(s.Namespaces))
	}
	if len(s.Lets) != 1 || s.Lets[0].Name != "threshold" {
		t.Errorf("Lets: got %+v, want one entry named threshold", s.Lets)
	}
	if len(s.PatternOrder) != 2 || s.PatternOrder[0] != "CII-headers" {
		t.Errorf("PatternOrder: got %v, want [CII-headers, CII-totals]", s.PatternOrder)
	}
	phase, ok := s.Phases["business-rules"]
	if !ok {
		t.Fatalf("phase business-rules missing")
	}
	if len(phase.Active) != 2 {
		t.Errorf("phase active: got %d, want 2", len(phase.Active))
	}
}

func TestLoad_RuleAssertsAndReports(t *testing.T) {
	fsys := fstest.MapFS{"s.sch": &fstest.MapFile{Data: []byte(en16931ish)}}
	s, err := Load(context.Background(), fsys, "s.sch")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	headers := s.Patterns["CII-headers"]
	if len(headers.Rules) != 1 {
		t.Fatalf("rules: got %d, want 1", len(headers.Rules))
	}
	r := headers.Rules[0]
	if r.Context != "//rsm:ExchangedDocument" {
		t.Errorf("Context: got %q", r.Context)
	}
	if len(r.Asserts) != 2 {
		t.Fatalf("asserts: got %d, want 2", len(r.Asserts))
	}
	if r.Asserts[0].ID != "BR-CO-25" || r.Asserts[0].Flag != "fatal" {
		t.Errorf("first assert: got %+v", r.Asserts[0])
	}
	if r.Asserts[0].Negate {
		t.Errorf("assert should not be negated")
	}

	totals := s.Patterns["CII-totals"]
	if len(totals.Rules[0].Reports) != 1 {
		t.Fatalf("reports: got %d, want 1", len(totals.Rules[0].Reports))
	}
	rep := totals.Rules[0].Reports[0]
	if !rep.Negate {
		t.Errorf("report should be negated")
	}
	if rep.Message != "[BR-DE-01] Currency required" {
		t.Errorf("report message: got %q", rep.Message)
	}
}

const includeOuter = `<?xml version="1.0" encoding="UTF-8"?>
<sch:schema xmlns:sch="http://purl.oclc.org/dsdl/schematron">
  <sch:include href="inner.sch"/>
  <sch:phase id="p"><sch:active pattern="P1"/></sch:phase>
</sch:schema>`

const includeInner = `<?xml version="1.0" encoding="UTF-8"?>
<sch:schema xmlns:sch="http://purl.oclc.org/dsdl/schematron">
  <sch:pattern id="P1">
    <sch:rule context="/root">
      <sch:assert id="X" test="@ok">missing @ok</sch:assert>
    </sch:rule>
  </sch:pattern>
</sch:schema>`

func TestLoad_FollowsIncludes(t *testing.T) {
	fsys := fstest.MapFS{
		"outer.sch": &fstest.MapFile{Data: []byte(includeOuter)},
		"inner.sch": &fstest.MapFile{Data: []byte(includeInner)},
	}
	s, err := Load(context.Background(), fsys, "outer.sch")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := s.Patterns["P1"]; !ok {
		t.Errorf("included pattern P1 missing, have %v", s.PatternOrder)
	}
}

const phaseRefMissingPattern = `<?xml version="1.0" encoding="UTF-8"?>
<sch:schema xmlns:sch="http://purl.oclc.org/dsdl/schematron">
  <sch:phase id="p"><sch:active pattern="ghost"/></sch:phase>
</sch:schema>`

func TestLoad_UnresolvedPhaseRef(t *testing.T) {
	fsys := fstest.MapFS{"s.sch": &fstest.MapFile{Data: []byte(phaseRefMissingPattern)}}
	_, err := Load(context.Background(), fsys, "s.sch")
	if !errors.Is(err, ErrUnresolvedRef) {
		t.Errorf("err: got %v, want errors.Is(_, ErrUnresolvedRef)", err)
	}
}

const malformed = `<?xml version="1.0"?><not-a-schema/>`

func TestLoad_Malformed(t *testing.T) {
	fsys := fstest.MapFS{"s.sch": &fstest.MapFile{Data: []byte(malformed)}}
	_, err := Load(context.Background(), fsys, "s.sch")
	if !errors.Is(err, ErrSchemaParse) {
		t.Errorf("err: got %v, want errors.Is(_, ErrSchemaParse)", err)
	}
}

const abstract = `<?xml version="1.0" encoding="UTF-8"?>
<sch:schema xmlns:sch="http://purl.oclc.org/dsdl/schematron">
  <sch:pattern id="abs" abstract="true">
    <sch:rule context="X"><sch:assert id="A" test="@x">m</sch:assert></sch:rule>
  </sch:pattern>
</sch:schema>`

func TestLoad_AbstractPatternsRejected(t *testing.T) {
	fsys := fstest.MapFS{"s.sch": &fstest.MapFile{Data: []byte(abstract)}}
	_, err := Load(context.Background(), fsys, "s.sch")
	if !errors.Is(err, ErrUnsupportedConstruct) {
		t.Errorf("err: got %v, want errors.Is(_, ErrUnsupportedConstruct)", err)
	}
}

func TestLoad_ContextCancel(t *testing.T) {
	fsys := fstest.MapFS{"s.sch": &fstest.MapFile{Data: []byte(en16931ish)}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Load(ctx, fsys, "s.sch")
	if err == nil {
		t.Error("expected ctx error")
	}
}
