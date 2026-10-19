package content

import (
	"context"
	"testing"
)

// TestTerminatedEnd pins where content written after a stream can start: the
// end of the stream, unless the stream ends inside a literal or hex string it
// never terminates (ISO 32000-1, 7.3.4.2 and 7.3.4.3), where it is the end of
// the last operator.
func TestTerminatedEnd(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const body = "0 0 m 10 10 l S"
	for _, tc := range []struct {
		name, data string
		want       int
	}{
		{"terminated", body + "\n(closed) Tj\n", len(body + "\n(closed) Tj\n")},
		{"trailing operand", body + "\n1 2", len(body + "\n1 2")},
		{"unterminated literal string", body + "\n(dangling ( string", len(body)},
		{"unterminated hex string", body + "\n<0A1B", len(body)},
		{"operands before the string", body + "\n1 2 (dangling", len(body)},
		{"nothing but the string", "(dangling", 0},
		{"empty", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := TerminatedEnd(ctx, []byte(tc.data)); got != tc.want {
				t.Errorf("TerminatedEnd(%q) = %d, want %d", tc.data, got, tc.want)
			}
		})
	}
}
