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
