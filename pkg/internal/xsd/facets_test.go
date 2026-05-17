package xsd

import "testing"

func TestValidateValue_NoFacets(t *testing.T) {
	st := &SimpleType{Base: builtin("string")}
	if vs := ValidateValue("anything", st); len(vs) != 0 {
		t.Errorf("got %v, want no violations", vs)
	}
}

func TestValidateValue_LexicalFails(t *testing.T) {
	st := &SimpleType{Base: builtin("integer")}
	vs := ValidateValue("not-an-int", st)
	if len(vs) != 1 {
		t.Fatalf("got %d violations, want 1: %v", len(vs), vs)
	}
}

func TestValidateValue_PatternFacet(t *testing.T) {
	st := &SimpleType{
		Base:   builtin("string"),
		Facets: []Facet{Pattern{Value: "[A-Z]{3}"}},
	}
	if vs := ValidateValue("ABC", st); len(vs) != 0 {
		t.Errorf("want match, got %v", vs)
	}
	if vs := ValidateValue("abc", st); len(vs) == 0 {
		t.Errorf("expected pattern violation on lowercase")
	}
	if vs := ValidateValue("ABCD", st); len(vs) == 0 {
		t.Errorf("expected pattern violation on too-long: implicit anchoring")
	}
}

func TestValidateValue_EnumerationFacet(t *testing.T) {
	st := &SimpleType{
		Base: builtin("string"),
		Facets: []Facet{
			Enumeration{Value: "EUR"},
			Enumeration{Value: "USD"},
			Enumeration{Value: "GBP"},
		},
	}
	if vs := ValidateValue("EUR", st); len(vs) != 0 {
		t.Errorf("EUR: got %v, want no violations", vs)
	}
	if vs := ValidateValue("BTC", st); len(vs) != 1 {
		t.Errorf("BTC: got %d violations, want 1: %v", len(vs), vs)
	}
}

func TestValidateValue_LengthFacets(t *testing.T) {
	tests := []struct {
		name   string
		facets []Facet
		value  string
		want   int
	}{
		{"exact length match", []Facet{Length{Value: 3}}, "abc", 0},
		{"exact length too short", []Facet{Length{Value: 3}}, "ab", 1},
		{"exact length too long", []Facet{Length{Value: 3}}, "abcd", 1},
		{"min length pass", []Facet{MinLength{Value: 2}}, "ab", 0},
		{"min length fail", []Facet{MinLength{Value: 2}}, "a", 1},
		{"max length pass", []Facet{MaxLength{Value: 5}}, "hello", 0},
		{"max length fail", []Facet{MaxLength{Value: 3}}, "abcd", 1},
		{"utf-8 counts code points", []Facet{MaxLength{Value: 1}}, "é", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &SimpleType{Base: builtin("string"), Facets: tc.facets}
			got := len(ValidateValue(tc.value, st))
			if got != tc.want {
				t.Errorf("got %d violations, want %d", got, tc.want)
			}
		})
	}
}

func TestValidateValue_NumericBounds(t *testing.T) {
	tests := []struct {
		name   string
		facets []Facet
		value  string
		want   int
	}{
		{"minInclusive ok", []Facet{MinInclusive{Value: "0"}}, "0", 0},
		{"minInclusive below", []Facet{MinInclusive{Value: "0"}}, "-1", 1},
		{"maxInclusive ok", []Facet{MaxInclusive{Value: "100"}}, "100", 0},
		{"maxInclusive above", []Facet{MaxInclusive{Value: "100"}}, "101", 1},
		{"minExclusive at boundary fails", []Facet{MinExclusive{Value: "0"}}, "0", 1},
		{"minExclusive above ok", []Facet{MinExclusive{Value: "0"}}, "0.01", 0},
		{"maxExclusive at boundary fails", []Facet{MaxExclusive{Value: "10"}}, "10", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &SimpleType{Base: builtin("decimal"), Facets: tc.facets}
			got := len(ValidateValue(tc.value, st))
			if got != tc.want {
				t.Errorf("got %d violations, want %d: %v",
					got, tc.want, ValidateValue(tc.value, st))
			}
		})
	}
}

func TestValidateValue_FractionAndTotalDigits(t *testing.T) {
	tests := []struct {
		name   string
		facets []Facet
		value  string
		want   int
	}{
		{"fractionDigits=2 accepts 2", []Facet{FractionDigits{Value: 2}}, "12.34", 0},
		{"fractionDigits=2 rejects 3", []Facet{FractionDigits{Value: 2}}, "12.345", 1},
		{"totalDigits=4 accepts", []Facet{TotalDigits{Value: 4}}, "12.34", 0},
		{"totalDigits=4 rejects 5", []Facet{TotalDigits{Value: 4}}, "123.45", 1},
		{"totalDigits ignores sign", []Facet{TotalDigits{Value: 3}}, "-100", 0},
		{"totalDigits ignores leading zero", []Facet{TotalDigits{Value: 3}}, "0.123", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &SimpleType{Base: builtin("decimal"), Facets: tc.facets}
			got := len(ValidateValue(tc.value, st))
			if got != tc.want {
				t.Errorf("got %d violations, want %d: %v",
					got, tc.want, ValidateValue(tc.value, st))
			}
		})
	}
}

func TestValidateValue_WhiteSpaceNormalization(t *testing.T) {
	// "collapse" normalises before downstream facets see the value.
	st := &SimpleType{
		Base: builtin("string"),
		Facets: []Facet{
			WhiteSpace{Value: "collapse"},
			MaxLength{Value: 5},
		},
	}
	// "  abc  " collapses to "abc" → length 3 → ok.
	if vs := ValidateValue("  abc  ", st); len(vs) != 0 {
		t.Errorf("collapse: got %v, want no violations", vs)
	}
}

func TestValidateValue_BaseChainAccumulatesFacets(t *testing.T) {
	// Derived type tightens an upstream maxLength. Validation should
	// accumulate both facets and apply the tighter one (10 -> 5).
	upstream := &SimpleType{
		Name:   QName{Namespace: "urn:t", Local: "Upstream"},
		Base:   builtin("string"),
		Facets: []Facet{MaxLength{Value: 10}},
	}
	derived := &SimpleType{
		Name:   QName{Namespace: "urn:t", Local: "Derived"},
		Base:   upstream,
		Facets: []Facet{MaxLength{Value: 5}},
	}
	if vs := ValidateValue("hello", derived); len(vs) != 0 {
		t.Errorf("len 5: got %v, want no violations", vs)
	}
	if vs := ValidateValue("helloworld", derived); len(vs) == 0 {
		t.Errorf("len 10 against derived: expected violation from tightened maxLength")
	}
}

func TestValidateValue_LexicalAndFacetCombine(t *testing.T) {
	st := &SimpleType{
		Base:   builtin("integer"),
		Facets: []Facet{MinInclusive{Value: "0"}, MaxInclusive{Value: "100"}},
	}
	// In-range integer passes.
	if vs := ValidateValue("42", st); len(vs) != 0 {
		t.Errorf("42: got %v, want no violations", vs)
	}
	// Out-of-range integer trips one facet violation but lexical passes.
	if vs := ValidateValue("200", st); len(vs) != 1 {
		t.Errorf("200: got %d violations, want 1: %v", len(vs), vs)
	}
	// Non-integer trips lexical AND bound checks (bound check sees it
	// as non-numeric).
	if vs := ValidateValue("abc", st); len(vs) < 1 {
		t.Errorf("abc: got %d violations, want at least 1", len(vs))
	}
}
