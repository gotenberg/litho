package parser

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
)

// TestHexStringOddCountIncludesInvalidCharacters pins that the odd-length
// finding counts every non-white-space character of a hex string, invalid ones
// included: ISO 19005-2, 6.1.6 t1 requires an even number of them, and an
// invalid character is t2's business. veraPDF counts the same way, so <484!>
// fails t2 alone. White space is not counted.
func TestHexStringOddCountIncludesInvalidCharacters(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		src         string
		wantOdd     bool
		wantInvalid bool
	}{
		{"<484!>", false, true},
		{"<484>", true, false},
		{"<48 4>", true, false},
		{"<4848>", false, false},
		{"<48!>", true, true},
	} {
		t.Run(tt.src, func(t *testing.T) {
			t.Parallel()
			src := []byte(tt.src)
			p := NewParser(lexer.NewLexer(ioat.NewReader(bytes.NewReader(src), int64(len(src)), 0))).WithFormatChecking()
			if _, err := p.ParseObject(context.Background()); err != nil {
				t.Fatalf("ParseObject: %v", err)
			}
			var odd, invalid bool
			for _, v := range p.FormatViolations {
				odd = odd || strings.Contains(v.Error(), "odd number of digits")
				invalid = invalid || strings.Contains(v.Error(), "invalid character")
			}
			if odd != tt.wantOdd || invalid != tt.wantInvalid {
				t.Errorf("odd = %v, invalid = %v, want %v, %v (%v)", odd, invalid, tt.wantOdd, tt.wantInvalid, p.FormatViolations)
			}
		})
	}
}

// TestHexStringViolationsWrapTheirSentinels pins that each hex-string finding
// wraps its sentinel, which remediation matches with errors.Is, and keeps the
// exact wording compliance reads to pick the rule and quotes in the
// violation's context.
func TestHexStringViolationsWrapTheirSentinels(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		src      string
		sentinel error
		other    error
		want     string
	}{
		{"<484>", ErrHexStringOddDigits, ErrHexStringInvalidChar, "compliance: hex string has odd number of digits"},
		{"<484!>", ErrHexStringInvalidChar, ErrHexStringOddDigits, "compliance: hex string contains invalid character '!'"},
	} {
		t.Run(tt.src, func(t *testing.T) {
			t.Parallel()
			src := []byte(tt.src)
			p := NewParser(lexer.NewLexer(ioat.NewReader(bytes.NewReader(src), int64(len(src)), 0))).WithFormatChecking()
			if _, err := p.ParseObject(context.Background()); err != nil {
				t.Fatalf("ParseObject: %v", err)
			}
			if len(p.FormatViolations) != 1 {
				t.Fatalf("got %d violations, want 1: %v", len(p.FormatViolations), p.FormatViolations)
			}
			v := p.FormatViolations[0]
			if !errors.Is(v, tt.sentinel) {
				t.Errorf("%q does not wrap %v", v, tt.sentinel)
			}
			if errors.Is(v, tt.other) {
				t.Errorf("%q wraps the other finding's sentinel %v", v, tt.other)
			}
			if v.Error() != tt.want {
				t.Errorf("message = %q, want %q", v, tt.want)
			}
		})
	}
}

// TestHexStringViolationNamesItsEntry pins that a hex-string finding in a
// string written as a dictionary value names the entry and the depth the
// string was read at, and keeps the message and sentinel it has anywhere else.
// A string nested in an array, or read on its own, names no entry, and the
// entry a nested dictionary notes is put back once it closes.
func TestHexStringViolationNamesItsEntry(t *testing.T) {
	t.Parallel()
	type entry struct {
		key   string
		depth int
	}
	for _, tt := range []struct {
		src  string
		want []entry // one per violation, key "" for none
	}{
		{"<< /ActualText <414> >>", []entry{{"ActualText", 2}}},
		{"<< /X << /ActualText <414> >> >>", []entry{{"ActualText", 3}}},
		{"<< /X [<414>] >>", []entry{{"", 0}}},
		{"<414>", []entry{{"", 0}}},
		{"<< /A <41Z> >>", []entry{{"A", 2}, {"A", 2}}},
		{"<< /A << /B (x) >> /C <414> >>", []entry{{"C", 2}}},
		{"[<< /A 1 >> <414>]", []entry{{"", 0}}},
	} {
		t.Run(tt.src, func(t *testing.T) {
			t.Parallel()
			src := []byte(tt.src)
			p := NewParser(lexer.NewLexer(ioat.NewReader(bytes.NewReader(src), int64(len(src)), 0))).WithFormatChecking()
			if _, err := p.ParseObject(context.Background()); err != nil {
				t.Fatalf("ParseObject: %v", err)
			}
			if len(p.FormatViolations) != len(tt.want) {
				t.Fatalf("got %d violations, want %d: %v", len(p.FormatViolations), len(tt.want), p.FormatViolations)
			}
			for i, v := range p.FormatViolations {
				if !errors.Is(v, ErrHexStringOddDigits) && !errors.Is(v, ErrHexStringInvalidChar) {
					t.Errorf("violation %d is %v, want a hex string violation", i, v)
				}
				if !strings.HasPrefix(v.Error(), "compliance: hex string ") {
					t.Errorf("violation %d reads %q", i, v)
				}
				var got entry
				if ev, ok := errors.AsType[*EntryViolationError](v); ok {
					got = entry{string(ev.Key), ev.Depth}
				}
				if got != tt.want[i] {
					t.Errorf("violation %d names %+v, want %+v", i, got, tt.want[i])
				}
			}
		})
	}
}
