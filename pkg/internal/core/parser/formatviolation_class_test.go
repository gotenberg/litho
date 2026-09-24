package parser

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
)

// TestFormatViolationsMatchTheirClass pins the sentinel class of every format
// violation the parser records, and the exact message it keeps.
//
// Compliance picks the rule by class. It matched substrings of the message
// before, so the message is also pinned: it is what a violation's context
// quotes, and it must read as it did.
func TestFormatViolationsMatchTheirClass(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		src    string
		class  error
		stream bool
		want   string
	}{
		{
			name:  "number with an exponent",
			src:   "1.5e2",
			class: ErrNumberExponent,
			want:  "compliance: number uses scientific notation, which ISO 32000-1 §7.3.3 does not permit",
		},
		{
			name:   "stream keyword followed by CR only",
			src:    "<< /Length 3 >>\nstream\rabc\nendstream",
			class:  ErrStreamKeywordEOL,
			stream: true,
			want:   "compliance: stream keyword followed by CR only (must be CR+LF or LF)",
		},
		{
			name:   "stream keyword not followed by EOL",
			src:    "<< /Length 4 >>\nstream abc\nendstream",
			class:  ErrStreamKeywordEOL,
			stream: true,
			want:   "compliance: stream keyword not followed by EOL",
		},
		{
			name:   "stream /Length disagreeing with the data",
			src:    "<< /Length 5 >>\nstream\nabc\nendstream",
			class:  ErrStreamLengthMismatch,
			stream: true,
			want:   "compliance: stream /Length 5 does not match actual byte count 4",
		},
		{
			name:   "stream with no /Length",
			src:    "<< >>\nstream\nabc\nendstream",
			class:  ErrStreamMissingLength,
			stream: true,
			want:   "compliance: stream missing /Length key",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
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
			if !errors.Is(v, tt.class) {
				t.Errorf("%q does not match its class %v", v, tt.class)
			}
			if got := errors.Is(v, ErrStreamFormat); got != tt.stream {
				t.Errorf("errors.Is(%q, ErrStreamFormat) = %v, want %v", v, got, tt.stream)
			}
			for _, other := range []error{ErrNumberExponent, ErrStreamKeywordEOL, ErrStreamLengthMismatch, ErrStreamMissingLength, ErrHexStringOddDigits, ErrHexStringInvalidChar} {
				if other != tt.class && errors.Is(v, other) {
					t.Errorf("%q also matches %v", v, other)
				}
			}
			if v.Error() != tt.want {
				t.Errorf("message = %q, want %q", v.Error(), tt.want)
			}
		})
	}
}
