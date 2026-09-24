package xsd

import "testing"

// TestValidateLexical_TemporalYears pins the year rules of XML Schema Part
// 2, 3.2.7.1, which date (3.2.9.1) inherits. The former checkDate read the
// year as exactly four characters at a fixed offset, so it rejected every
// negative year and every year above 9999, both of which 1.0 allows.
func TestValidateLexical_TemporalYears(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		base    string
		value   string
		wantErr bool
	}{
		{"date negative year", "date", "-0001-01-01", false},
		{"date negative five-digit year", "date", "-12345-06-30", false},
		{"date five-digit year", "date", "10000-01-01", false},
		{"date year beyond the digit cap", "date", "1234567890-01-01", false},
		{"date year 0000", "date", "0000-01-01", true},
		{"date negative year 0000", "date", "-0000-01-01", true},
		{"date long year with leading zero", "date", "01234-01-01", true},
		{"date plus-signed year", "date", "+2026-01-01", true},
		{"date three-digit year", "date", "999-01-01", true},
		{"dateTime negative year", "dateTime", "-0044-03-15T12:00:00", false},
		{"dateTime five-digit year", "dateTime", "12026-05-17T12:00:00Z", false},
		{"dateTime year 0000", "dateTime", "0000-05-17T12:00:00", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := validateLexical(tc.value, builtin(tc.base))
			if tc.wantErr && got == nil {
				t.Errorf("%s %q: got nil, want lexical error", tc.base, tc.value)
			}
			if !tc.wantErr && got != nil {
				t.Errorf("%s %q: got %v, want nil", tc.base, tc.value, got)
			}
		})
	}
}

// TestValidateLexical_TemporalFields pins the field limits of Part 2,
// 3.2.7: each field is bounded by the next-higher one, so the day depends
// on the month and the year, and the timezone is bounded by 3.2.7.3.
func TestValidateLexical_TemporalFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		base    string
		value   string
		wantErr bool
	}{
		{"leap day in a leap year", "date", "2024-02-29", false},
		{"leap day in a common year", "date", "2023-02-29", true},
		{"leap day in a century year", "date", "1900-02-29", true},
		{"leap day in a 400th year", "date", "2000-02-29", false},
		{"leap day in 1 BCE", "date", "-0001-02-29", false},
		{"day 31 of a 30-day month", "date", "2026-04-31", true},
		{"trailing characters", "date", "2026-05-17garbage", true},
		{"timezone +14:00", "date", "2026-05-17+14:00", false},
		{"timezone +14:01", "date", "2026-05-17+14:01", true},
		{"timezone +15:00", "date", "2026-05-17+15:00", true},
		{"timezone minutes 60", "date", "2026-05-17-05:60", true},
		{"hour 24 at midnight", "dateTime", "2026-01-01T24:00:00", false},
		{"hour 24 past midnight", "dateTime", "2026-01-01T24:00:01", true},
		{"hour 24 with a fraction", "dateTime", "2026-01-01T24:00:00.5", true},
		{"minute 60", "dateTime", "2026-01-01T12:60:00", true},
		{"leap second", "dateTime", "2026-12-31T23:59:60Z", false},
		{"period without digits", "dateTime", "2026-01-01T12:00:00.", true},
		{"fraction and timezone", "dateTime", "2026-01-01T12:00:00.123Z", false},
		{"trailing characters after seconds", "dateTime", "2026-01-01T12:00:00x", true},
		{"time hour 24", "time", "24:00:00", false},
		{"time hour 25", "time", "25:00:00", true},
		{"time trailing characters", "time", "12:00:00garbage", true},
		{"time with offset", "time", "13:20:00-05:00", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := validateLexical(tc.value, builtin(tc.base))
			if tc.wantErr && got == nil {
				t.Errorf("%s %q: got nil, want lexical error", tc.base, tc.value)
			}
			if !tc.wantErr && got != nil {
				t.Errorf("%s %q: got %v, want nil", tc.base, tc.value, got)
			}
		})
	}
}

// TestParseTemporal_TruncatedKinds covers the lexical spaces the range
// facets parse but [validateLexical] does not dispatch: gYearMonth, gYear,
// gMonthDay, gDay and gMonth (Part 2, 3.2.10.1 to 3.2.14.1).
func TestParseTemporal_TruncatedKinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind  temporalKind
		value string
		valid bool
	}{
		{kindGYearMonth, "1999-05", true},
		{kindGYearMonth, "-1999-05Z", true},
		{kindGYearMonth, "1999-13", false},
		{kindGYearMonth, "1999", false},
		{kindGYear, "1999", true},
		{kindGYear, "1999-05:00", true},
		{kindGYear, "99", false},
		{kindGMonthDay, "--02-29", true}, // 3.2.12 compares in a leap year.
		{kindGMonthDay, "--02-30", false},
		{kindGMonthDay, "-02-28", false},
		{kindGDay, "---31", true},
		{kindGDay, "---32", false},
		{kindGDay, "--31", false},
		{kindGMonth, "--12", true},
		{kindGMonth, "--12--", false}, // The 1.0 first-edition form, dropped by the second edition.
		{kindGMonth, "--13", false},
	}
	for _, tc := range tests {
		_, reason := parseTemporal(tc.value, tc.kind)
		if got := reason == ""; got != tc.valid {
			t.Errorf("%s %q: valid = %v (reason %q), want %v", tc.kind.form(), tc.value, got, reason, tc.valid)
		}
	}
}

// TestParseDuration pins the duration lexical space of Part 2, 3.2.6.1,
// including the examples it lists as allowed and as not allowed.
func TestParseDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value string
		valid bool
	}{
		{"P1Y2M3DT10H30M", true},
		{"-P120D", true},
		{"P1347Y", true},
		{"P1347M", true},
		{"P1Y2MT2H", true},
		{"P0Y1347M", true},
		{"P0Y1347M0D", true},
		{"PT1.5S", true},
		{"P-1347M", false},
		{"P1Y2MT", false},
		{"P", false},
		{"PT", false},
		{"-P", false},
		{"1D", false},
		{"P1D1Y", false},
		{"P1Y1Y", false},
		{"PT1.5M", false},
		{"P1.5D", false},
		{"PT.5S", false},
		{"PT1.S", false},
		{"P1H", false},
		{"PT1D", false},
	}
	for _, tc := range tests {
		_, reason := parseDuration(tc.value)
		if got := reason == ""; got != tc.valid {
			t.Errorf("%q: valid = %v (reason %q), want %v", tc.value, got, reason, tc.valid)
		}
	}
}
