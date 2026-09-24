package xsd

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
)

// rangeCase is one value checked against one range facet on a built-in
// base. Every value is a valid literal of its base, so any violation
// ValidateValue reports comes from the facet.
type rangeCase struct {
	name  string
	base  string
	facet Facet
	value string
	pass  bool
}

func runRangeCases(t *testing.T, cases []rangeCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			st := &SimpleType{Base: builtin(tc.base), Facets: []Facet{tc.facet}}
			vs := ValidateValue(tc.value, st)
			if tc.pass && len(vs) != 0 {
				t.Errorf("xs:%s %q against %#v: got %v, want no violation", tc.base, tc.value, tc.facet, vs)
			}
			if !tc.pass && len(vs) != 1 {
				t.Errorf("xs:%s %q against %#v: got %v, want one violation", tc.base, tc.value, tc.facet, vs)
			}
		})
	}
}

func TestValidateValue_RangeDecimalFamily(t *testing.T) {
	t.Parallel()

	runRangeCases(t, []rangeCase{
		{"int minInclusive equal", "int", MinInclusive{"10"}, "10", true},
		{"int minInclusive below", "int", MinInclusive{"10"}, "9", false},
		{"int minExclusive equal", "int", MinExclusive{"10"}, "10", false},
		{"int minExclusive above", "int", MinExclusive{"10"}, "11", true},
		{"decimal maxInclusive equal with a trailing zero", "decimal", MaxInclusive{"1.5"}, "1.50", true},
		{"decimal maxInclusive above", "decimal", MaxInclusive{"1.5"}, "1.51", false},
		{"decimal maxExclusive below", "decimal", MaxExclusive{"1.5"}, "1.49", true},
		{"unsignedLong beyond int64", "unsignedLong", MaxInclusive{"18446744073709551615"}, "18446744073709551615", true},
	})
}

// TestValidateValue_RangeFloat pins Part 2, 3.2.4 and 3.2.5: one zero, NaN
// equal to itself and incomparable with everything else, and literals
// mapped to the nearest value of the type's own precision.
func TestValidateValue_RangeFloat(t *testing.T) {
	t.Parallel()

	runRangeCases(t, []rangeCase{
		{"float minInclusive equal", "float", MinInclusive{"1.5"}, "1.5", true},
		{"float minInclusive below", "float", MinInclusive{"1.5"}, "1.4", false},
		{"float exponent", "float", MinInclusive{"1.5"}, "1E1", true},
		{"float maxExclusive INF", "float", MaxExclusive{"INF"}, "3.4E38", true},
		{"float maxExclusive at INF", "float", MaxExclusive{"INF"}, "INF", false},
		{"float minInclusive -INF", "float", MinInclusive{"-INF"}, "-INF", true},
		{"negative zero equals zero", "float", MinInclusive{"-0"}, "0", true},
		{"negative zero is not above zero", "float", MinExclusive{"-0"}, "0", false},
		{"NaN value against a number", "float", MinInclusive{"0"}, "NaN", false},
		{"NaN value against a number, upper bound", "double", MaxInclusive{"0"}, "NaN", false},
		{"NaN bound admits NaN inclusively", "float", MaxInclusive{"NaN"}, "NaN", true},
		{"NaN bound excludes numbers", "float", MaxInclusive{"NaN"}, "1", false},
		{"NaN bound excludes NaN exclusively", "double", MaxExclusive{"NaN"}, "NaN", false},
		// 0.1000000001 rounds to the same float as 0.1, but not the same double.
		{"float precision", "float", MaxInclusive{"0.1"}, "0.1000000001", true},
		{"double precision", "double", MaxInclusive{"0.1"}, "0.1000000001", false},
		{"double overflow maps to the largest finite value", "double", MaxExclusive{"INF"}, "1E400", true},
	})
}

// TestValidateValue_RangeDateTime pins the order of Part 2, 3.2.7.4,
// timezone normalization included.
func TestValidateValue_RangeDateTime(t *testing.T) {
	t.Parallel()

	const midnightZ = "2000-01-01T00:00:00Z"
	runRangeCases(t, []rangeCase{
		{"minInclusive equal", "dateTime", MinInclusive{midnightZ}, midnightZ, true},
		{"minInclusive below", "dateTime", MinInclusive{midnightZ}, "1999-12-31T23:59:59Z", false},
		{"minInclusive equal after normalization", "dateTime", MinInclusive{midnightZ}, "2000-01-01T01:00:00+01:00", true},
		{"minInclusive below after normalization", "dateTime", MinInclusive{midnightZ}, "2000-01-01T00:59:59+01:00", false},
		{"minExclusive equal after normalization", "dateTime", MinExclusive{midnightZ}, "2000-01-01T01:00:00+01:00", false},
		{"maxExclusive equal after normalization", "dateTime", MaxExclusive{midnightZ}, "1999-12-31T19:00:00-05:00", false},
		{"maxInclusive equal fraction", "dateTime", MaxInclusive{"2000-01-01T00:00:00.5Z"}, "2000-01-01T00:00:00.50Z", true},
		{"maxInclusive above by a fraction", "dateTime", MaxInclusive{"2000-01-01T00:00:00.5Z"}, "2000-01-01T00:00:00.500001Z", false},
		{"hour 24 is the next midnight, exclusive", "dateTime", MaxExclusive{"2000-01-02T00:00:00"}, "2000-01-01T24:00:00", false},
		{"hour 24 is the next midnight, inclusive", "dateTime", MinInclusive{"2000-01-02T00:00:00"}, "2000-01-01T24:00:00", true},
		{"negative year below year 1", "dateTime", MinInclusive{"0001-01-01T00:00:00"}, "-0001-12-31T23:59:59", false},
	})
}

// TestValidateValue_RangeTimezoneMix pins the partial order of Part 2,
// 3.2.7.4 between a timezoned and a non-timezoned value: ordered when more
// than 14 hours apart, incomparable otherwise, and an incomparable value
// satisfies no bound.
func TestValidateValue_RangeTimezoneMix(t *testing.T) {
	t.Parallel()

	const boundZ = "2000-01-15T12:00:00Z"
	const boundLocal = "2000-01-15T12:00:00"
	runRangeCases(t, []rangeCase{
		{"local value a day later", "dateTime", MinInclusive{boundZ}, "2000-01-16T12:00:00", true},
		{"local value exactly 14 hours later", "dateTime", MinInclusive{boundZ}, "2000-01-16T02:00:00", false},
		{"local value just over 14 hours later", "dateTime", MinInclusive{boundZ}, "2000-01-16T02:00:01", true},
		{"local value on the same face", "dateTime", MinInclusive{boundZ}, boundLocal, false},
		{"local value on the same face, upper bound", "dateTime", MaxInclusive{boundZ}, boundLocal, false},
		{"timezoned value just over 14 hours earlier", "dateTime", MaxInclusive{boundLocal}, "2000-01-14T21:59:59Z", true},
		{"timezoned value exactly 14 hours earlier", "dateTime", MaxInclusive{boundLocal}, "2000-01-14T22:00:00Z", false},
		{"date against a timezoned bound", "date", MinInclusive{"2000-01-01Z"}, "2000-01-01", false},
		{"date a day after a timezoned bound", "date", MinInclusive{"2000-01-01Z"}, "2000-01-02", true},
		{"time against a timezoned bound", "time", MaxInclusive{"12:00:00Z"}, "12:00:00", false},
	})
}

func TestValidateValue_RangeDateAndTime(t *testing.T) {
	t.Parallel()

	runRangeCases(t, []rangeCase{
		{"date minInclusive equal", "date", MinInclusive{"2000-01-01"}, "2000-01-01", true},
		{"date minInclusive below", "date", MinInclusive{"2000-01-01"}, "1999-12-31", false},
		{"date minInclusive above", "date", MinInclusive{"2000-01-01"}, "2000-01-02", true},
		{"date maxExclusive equal", "date", MaxExclusive{"2000-01-01"}, "2000-01-01", false},
		{"date maxExclusive below", "date", MaxExclusive{"2000-01-01"}, "1999-12-31", true},
		// 1.0 has no year 0: -0001 is 1 BCE, the year right before 0001.
		{"date 1 BCE below 1 CE", "date", MinInclusive{"0001-01-01"}, "-0001-12-31", false},
		{"date 1 BCE within an upper bound of 1 CE", "date", MaxInclusive{"0001-01-01"}, "-0001-12-31", true},
		{"date 1 BCE above 2 BCE", "date", MinInclusive{"-0002-01-01"}, "-0001-01-01", true},
		{"date five-digit year", "date", MinInclusive{"9999-12-31"}, "10000-01-01", true},
		// A timezoned date is the day starting at midnight in its timezone.
		{"date east of the bound starts earlier", "date", MinInclusive{"2000-01-01Z"}, "2000-01-01+01:00", false},
		{"date west of the bound starts later", "date", MinInclusive{"2000-01-01Z"}, "2000-01-01-01:00", true},
		// The 3.2.9 example: 2002-10-10+13:00 and 2002-10-09-11:00 are one interval.
		{"date recoverable timezone, inclusive", "date", MinInclusive{"2002-10-09-11:00"}, "2002-10-10+13:00", true},
		{"date recoverable timezone, exclusive", "date", MaxExclusive{"2002-10-09-11:00"}, "2002-10-10+13:00", false},
		{"time minInclusive equal", "time", MinInclusive{"09:00:00"}, "09:00:00", true},
		{"time minInclusive below", "time", MinInclusive{"09:00:00"}, "08:59:59", false},
		{"time equal after normalization", "time", MaxInclusive{"17:00:00Z"}, "18:00:00+01:00", true},
		{"time above after normalization", "time", MaxInclusive{"17:00:00Z"}, "18:00:01+01:00", false},
		// Per 3.2.8.2, 24:00:00 is midnight, the value 00:00:00.
		{"time 24:00:00 is midnight", "time", MaxInclusive{"00:00:00"}, "24:00:00", true},
		{"time 24:00:00 is not the end of the day", "time", MinExclusive{"23:59:59"}, "24:00:00", false},
		// Per 3.2.8 the order uses one arbitrary date, so normalizing
		// 01:00+03:00 lands on the day before.
		{"time normalization crosses midnight", "time", MinInclusive{"22:00:00Z"}, "01:00:00+03:00", false},
	})
}

// TestValidateValue_RangeTruncatedKinds pins the order on starting
// instants that Part 2, 3.2.10 to 3.2.14 give the truncated kinds.
func TestValidateValue_RangeTruncatedKinds(t *testing.T) {
	t.Parallel()

	runRangeCases(t, []rangeCase{
		{"gYearMonth equal", "gYearMonth", MinInclusive{"2000-02"}, "2000-02", true},
		{"gYearMonth month below", "gYearMonth", MinInclusive{"2000-02"}, "2000-01", false},
		{"gYearMonth year below", "gYearMonth", MinInclusive{"2000-02"}, "1999-12", false},
		{"gYearMonth above", "gYearMonth", MinInclusive{"2000-02"}, "2000-03", true},
		{"gYear equal", "gYear", MaxInclusive{"2000"}, "2000", true},
		{"gYear above", "gYear", MaxInclusive{"2000"}, "2001", false},
		{"gYear negative", "gYear", MaxInclusive{"2000"}, "-2001", true},
		{"gYear minExclusive equal", "gYear", MinExclusive{"1999"}, "1999", false},
		{"gYear minExclusive above", "gYear", MinExclusive{"1999"}, "2000", true},
		{"gYear timezone starts the year earlier", "gYear", MinInclusive{"2000Z"}, "2000+01:00", false},
		{"gMonthDay leap day equal", "gMonthDay", MaxInclusive{"--02-29"}, "--02-29", true},
		{"gMonthDay after the leap day", "gMonthDay", MaxInclusive{"--02-29"}, "--03-01", false},
		{"gDay equal", "gDay", MinInclusive{"---15"}, "---15", true},
		{"gDay below", "gDay", MinInclusive{"---15"}, "---14", false},
		{"gDay 31", "gDay", MaxInclusive{"---31"}, "---31", true},
		{"gMonth minExclusive equal", "gMonth", MinExclusive{"--06"}, "--06", false},
		{"gMonth minExclusive above", "gMonth", MinExclusive{"--06"}, "--07", true},
	})
}

// TestValidateValue_RangeDuration pins the partial order of Part 2,
// 3.2.6.2 and its facet rule, 3.2.6.3: an indeterminate comparison counts
// as false, so a duration incomparable with the bound fails it.
func TestValidateValue_RangeDuration(t *testing.T) {
	t.Parallel()

	runRangeCases(t, []rangeCase{
		{"P12M equals P1Y", "duration", MinInclusive{"P1Y"}, "P12M", true},
		{"P11M below P1Y", "duration", MinInclusive{"P1Y"}, "P11M", false},
		{"P13M above P1Y", "duration", MinInclusive{"P1Y"}, "P13M", true},
		{"P364D below P1Y", "duration", MinInclusive{"P1Y"}, "P364D", false},
		{"P365D incomparable with P1Y", "duration", MinInclusive{"P1Y"}, "P365D", false},
		{"P366D incomparable with P1Y", "duration", MaxInclusive{"P1Y"}, "P366D", false},
		{"P367D above P1Y", "duration", MinInclusive{"P1Y"}, "P367D", true},
		{"P27D below P1M", "duration", MaxInclusive{"P1M"}, "P27D", true},
		{"P30D incomparable with P1M", "duration", MaxInclusive{"P1M"}, "P30D", false},
		{"P32D above P1M", "duration", MaxInclusive{"P1M"}, "P32D", false},
		{"PT24H equals P1D", "duration", MinInclusive{"P1D"}, "PT24H", true},
		{"a millisecond short of P1D", "duration", MinInclusive{"P1D"}, "PT23H59M59.999S", false},
		{"PT3600S equals PT1H, exclusive", "duration", MaxExclusive{"PT1H"}, "PT3600S", false},
		{"half a second short of PT1H", "duration", MaxExclusive{"PT1H"}, "PT3599.5S", true},
		{"negative equal", "duration", MinInclusive{"-P1D"}, "-PT24H", true},
		{"negative below by a fraction", "duration", MinInclusive{"-P1D"}, "-PT24H0.5S", false},
		{"negative above", "duration", MinInclusive{"-P1D"}, "-PT23H", true},
		{"negative fraction above the bound", "duration", MaxInclusive{"-PT1.5S"}, "-PT1.25S", false},
		{"negative fraction below the bound", "duration", MaxInclusive{"-PT1.5S"}, "-PT1.75S", true},
	})
}

// TestValidateValue_RangePermissiveMisses pins the cases where the range
// facet cannot judge the value, and so reports nothing.
func TestValidateValue_RangePermissiveMisses(t *testing.T) {
	t.Parallel()

	runRangeCases(t, []rangeCase{
		// Part 2, 4.1.5: range facets do not apply to string or boolean,
		// so the facet is a schema error, not a fact about the value.
		{"string base", "string", MinInclusive{"5"}, "abc", true},
		{"boolean base", "boolean", MaxInclusive{"0"}, "true", true},
		{"bound outside the base value space", "date", MinInclusive{"not-a-date"}, "2000-01-01", true},
		{"value year beyond the digit cap", "gYear", MaxInclusive{"2000"}, "1234567890", true},
		{"bound year beyond the digit cap", "gYear", MinInclusive{"1234567890"}, "2000", true},
		{"duration component beyond the digit cap", "duration", MaxInclusive{"P1D"}, "P1234567890D", true},
		{"padding zeros do not count toward the cap", "duration", MaxInclusive{"P1D"}, "P0000000000002D", false},
	})

	t.Run("no base", func(t *testing.T) {
		t.Parallel()

		st := &SimpleType{Facets: []Facet{MinInclusive{Value: "10"}}}
		if vs := ValidateValue("5", st); len(vs) != 0 {
			t.Errorf("got %v, want no violation without a base to order by", vs)
		}
	})
}

// TestValidateRangeBound_ValueOutsideLexicalSpace pins that a value that
// is no literal of the base fails the facet, naming the base. The lexical
// check reports it separately when the base has one.
func TestValidateRangeBound_ValueOutsideLexicalSpace(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ base, bound, value string }{
		{"date", "2000-01-01", "2000-02-30"},
		{"gMonth", "--06", "--6"},
		{"float", "0", "+INF"},
		{"double", "0", "0x1p3"},
		{"duration", "P1D", "P1D1Y"},
	} {
		fc := MinInclusive{Value: tc.bound}
		v := validateRangeBound(tc.value, tc.bound, boundMinInclusive, builtin(tc.base), fc)
		if v == nil {
			t.Errorf("xs:%s %q: got nil, want a violation", tc.base, tc.value)
			continue
		}
		if want := "not an xs:" + tc.base + " literal"; !strings.Contains(v.Reason, want) {
			t.Errorf("xs:%s %q: reason %q, want it to contain %q", tc.base, tc.value, v.Reason, want)
		}
	}
}

// TestValidateRangeBound_IncomparableReason pins that an indeterminate
// comparison is reported as such rather than as below or above the bound.
func TestValidateRangeBound_IncomparableReason(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ base, bound, value string }{
		{"dateTime", "2000-01-16T12:00:00Z", "2000-01-16T12:00:00"},
		{"duration", "P1M", "P30D"},
		{"double", "1", "NaN"},
	} {
		for _, kind := range []boundKind{boundMinInclusive, boundMaxInclusive, boundMinExclusive, boundMaxExclusive} {
			v := validateRangeBound(tc.value, tc.bound, kind, builtin(tc.base), MinInclusive{Value: tc.bound})
			if v == nil || !strings.Contains(v.Reason, "incomparable") {
				t.Errorf("xs:%s %q against bound kind %d of %q: got %v, want an incomparable violation", tc.base, tc.value, kind, tc.bound, v)
			}
		}
	}
}

// dateRangeXSD restricts xs:date through two simpleTypes, so the range
// facet's base is found only by walking the restriction chain.
const dateRangeXSD = `<?xml version="1.0" encoding="UTF-8"?>
<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema" targetNamespace="urn:t" xmlns:t="urn:t" elementFormDefault="qualified">
  <xs:simpleType name="Day"><xs:restriction base="xs:date"/></xs:simpleType>
  <xs:simpleType name="Since2000">
    <xs:restriction base="t:Day"><xs:minInclusive value="2000-01-01"/></xs:restriction>
  </xs:simpleType>
  <xs:element name="Root" type="t:Since2000"/>
</xs:schema>`

// TestValidate_DateRangeFacetRegression reproduces the false positive the
// decimal-only comparison produced: every value of a date-based type with
// a range facet failed with "value is not a numeric literal", whatever the
// date, on any caller-supplied schema set.
func TestValidate_DateRangeFacetRegression(t *testing.T) {
	t.Parallel()

	schema, err := Load(context.Background(), fstest.MapFS{
		"s.xsd": &fstest.MapFile{Data: []byte(dateRangeXSD)},
	}, "s.xsd")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	for _, tc := range []struct {
		value string
		want  int
	}{
		{"2026-01-01", 0},
		{"2000-01-01", 0},
		{"1999-12-31", 1},
	} {
		doc := `<Root xmlns="urn:t">` + tc.value + `</Root>`
		vs, err := Validate(context.Background(), schema, []byte(doc))
		if err != nil {
			t.Fatalf("%s: Validate: %v", tc.value, err)
		}
		if len(vs) != tc.want {
			t.Errorf("%s: got %d violations %v, want %d", tc.value, len(vs), vs, tc.want)
		}
		for _, v := range vs {
			if strings.Contains(v.Reason, "numeric") {
				t.Errorf("%s: reason %q still compares the date as a number", tc.value, v.Reason)
			}
			if v.Rule != RuleFacet {
				t.Errorf("%s: rule %q, want %q", tc.value, v.Rule, RuleFacet)
			}
		}
	}
}
