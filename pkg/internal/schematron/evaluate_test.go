package schematron

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
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

func TestValidate_CarriageReturnIsWhitespace(t *testing.T) {
	// XPath 1.0, 3.7 ExprWhitespace and XPath 2.0, A.2.4: CR (#xD)
	// separates tokens like space. The bundled Factur-X profiles write
	// CRLF into test attributes as "&#13;&#10;", which XML keeps as a
	// literal CR. A relative path after one must still resolve against
	// the rule context, and a literal keeps its CR.
	sch := `<?xml version="1.0" encoding="UTF-8"?>
<sch:schema xmlns:sch="http://purl.oclc.org/dsdl/schematron">
  <sch:ns prefix="inv" uri="urn:example:invoice"/>
  <sch:let name="sep" value="'a&#13;b'"/>
  <sch:pattern id="P">
    <sch:rule context="&#13;&#10;inv:Line">
      <sch:assert id="OR" test="(&#13;&#10;inv:Missing&#13;&#10;or&#13;&#10;inv:Amount)">or</sch:assert>
      <sch:assert id="LIT" test="string-length($sep) = 3">literal keeps its CR</sch:assert>
      <sch:report id="CTX" test="true()">the context matched the line</sch:report>
    </sch:rule>
  </sch:pattern>
</sch:schema>`
	payload := `<inv:Invoice xmlns:inv="urn:example:invoice"><inv:Line><inv:Amount>1</inv:Amount></inv:Line></inv:Invoice>`
	fired, skipped := runOne(t, sch, payload)
	if strings.Join(fired, ",") != "CTX" || len(skipped) != 0 {
		t.Errorf("fired %v skipped %v, want only CTX", fired, skipped)
	}
}

func TestExprCache_UpstreamPanicsBecomeErrors(t *testing.T) {
	// antchfx/xpath v1.3.6 signals a malformed expression by panicking
	// in its parser and recovers that panic inside build() (build.go),
	// so a compile failure reaches litho as an error with no guard of
	// litho's own. Evaluation has no such recover: an undeclared
	// variable compiles and then dereferences nil, which evalBoolean
	// converts. This pins both upstream behaviours so an upgrade that
	// changes either fails here rather than crashing a validation run.
	c := newExprCache(map[string]string{"inv": "urn:example:invoice"})
	for _, expr := range []string{"'unclosed", "inv:a[", "undeclared:x", "1 +"} {
		if _, err := c.compile(expr); err == nil {
			t.Errorf("compile(%q): want an error", expr)
		}
	}
	fired, skipped := runOne(t, `<?xml version="1.0" encoding="UTF-8"?>
<sch:schema xmlns:sch="http://purl.oclc.org/dsdl/schematron">
  <sch:pattern id="P">
    <sch:rule context="/a">
      <sch:assert id="NOVAR" test="$undeclared = 1">undeclared</sch:assert>
    </sch:rule>
  </sch:pattern>
</sch:schema>`, `<a/>`)
	if len(fired) != 0 || len(skipped) != 1 {
		t.Errorf("fired %v skipped %v, want NOVAR skipped", fired, skipped)
	}
}

func TestValidate_CaseFunctionsBeyondASCII(t *testing.T) {
	// XPath F&O 3.1, 5.4.7 and 5.4.8: upper-case and lower-case map
	// characters through the Unicode case mappings, not ASCII only.
	// The bundled rules compare upper-case() against ASCII code lists,
	// so a non-ASCII letter whose upper case is ASCII (dotless i, long
	// s, sharp s) decides a verdict.
	sch := `<?xml version="1.0" encoding="UTF-8"?>
<sch:schema xmlns:sch="http://purl.oclc.org/dsdl/schematron">
  <sch:ns prefix="inv" uri="urn:example:invoice"/>
  <sch:pattern id="P">
    <sch:rule context="//inv:Invoice">
      <sch:assert id="LOW" test="lower-case(inv:Name) = 'élan'">lower-case maps É</sch:assert>
      <sch:assert id="UPI" test="upper-case(inv:Code) = 'IT'">dotless i uppercases to I</sch:assert>
      <sch:assert id="UPS" test="upper-case(inv:Unit) = 'S'">long s uppercases to S</sch:assert>
      <sch:assert id="UPSS" test="upper-case(inv:Street) = 'STRASSE'">sharp s uppercases to SS</sch:assert>
      <sch:assert id="UPASCII" test="upper-case(normalize-space(inv:Country)) = 'DE'">ASCII still maps</sch:assert>
    </sch:rule>
  </sch:pattern>
</sch:schema>`
	payload := `<inv:Invoice xmlns:inv="urn:example:invoice">` +
		`<inv:Name>ÉLAN</inv:Name><inv:Code>ıt</inv:Code><inv:Unit>ſ</inv:Unit>` +
		`<inv:Street>straße</inv:Street><inv:Country> de </inv:Country></inv:Invoice>`
	fired, skipped := runOne(t, sch, payload)
	if len(fired) != 0 || len(skipped) != 0 {
		t.Errorf("fired %v skipped %v, want neither", fired, skipped)
	}
}

func TestUpperCaseToASCIIMatchesUnicode(t *testing.T) {
	// Regenerates upperCaseToASCII: every non-ASCII code point whose
	// full upper-case mapping is non-empty ASCII and differs from it.
	upper := cases.Upper(language.Und)
	var want []string
	for r := rune(0x80); r <= unicode.MaxRune; r++ {
		if !utf8.ValidRune(r) {
			continue
		}
		u := upper.String(string(r))
		if u == string(r) || u == "" {
			continue
		}
		ascii := true
		for i := 0; i < len(u); i++ {
			if u[i] >= utf8.RuneSelf {
				ascii = false
				break
			}
		}
		if ascii {
			want = append(want, string(r)+"="+u)
		}
	}
	got := make([]string, 0, len(upperCaseToASCII))
	for _, m := range upperCaseToASCII {
		got = append(got, m.from+"="+m.to)
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("upperCaseToASCII = %v, Unicode gives %v", got, want)
	}
}

func TestXPathBoolean_EffectiveBooleanValue(t *testing.T) {
	// antchfx/xpath v1.3.6 returns round() as a Go int. XPath 2.0,
	// 2.4.3 (and XPath 1.0, 4.3 boolean()): a number is true unless it
	// is zero or NaN, whatever Go type carries it.
	cases := []struct {
		in   any
		want bool
	}{
		{int(3), true},
		{int(0), false},
		{float64(2), true},
		{float64(0), false},
		{"x", true},
		{"", false},
		{true, true},
		{false, false},
	}
	for _, c := range cases {
		if got := xpathBoolean(c.in); got != c.want {
			t.Errorf("xpathBoolean(%#v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestValidate_RoundAcrossCarriageReturn(t *testing.T) {
	// XPath 1.0, 3.7: CR is whitespace between "round" and "(". The
	// bundled Factur-X profiles carry CRLF inside test attributes.
	sch := `<?xml version="1.0" encoding="UTF-8"?>
<sch:schema xmlns:sch="http://purl.oclc.org/dsdl/schematron">
  <sch:ns prefix="inv" uri="urn:example:invoice"/>
  <sch:pattern id="P">
    <sch:rule context="//inv:Invoice">
      <sch:assert id="EBV" test="round&#13;&#10;(2.6)">round is 3, so true</sch:assert>
      <sch:assert id="SUM" test="round&#13;&#10;(2.6) + 1 = 4">round feeds arithmetic</sch:assert>
    </sch:rule>
  </sch:pattern>
</sch:schema>`
	fired, skipped := runOne(t, sch, `<inv:Invoice xmlns:inv="urn:example:invoice"/>`)
	if len(fired) != 0 || len(skipped) != 0 {
		t.Errorf("fired %v skipped %v, want neither", fired, skipped)
	}
}
