package filter

import (
	"bytes"
	"errors"
	"runtime"
	"testing"
)

func TestDecodeRunLength(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		want    []byte
		wantErr bool
	}{
		{
			name:  "literal run",
			input: []byte{4, 'H', 'e', 'l', 'l', 'o', 0x80},
			want:  []byte("Hello"),
		},
		{
			name:  "repeat run",
			input: []byte{253, 'A', 0x80}, // 257-253 = 4 copies of 'A'
			want:  []byte("AAAA"),
		},
		{
			name: "mixed literal and repeat",
			input: []byte{
				2, 'A', 'B', 'C', // 3 literal bytes
				254, 'X', // 257-254 = 3 copies of 'X'
				0x80, // EOD
			},
			want: []byte("ABCXXX"),
		},
		{
			name:  "EOD marker mid-stream ignores trailing data",
			input: []byte{1, 'H', 'i', 0x80, 0, 'Z', 0x80},
			want:  []byte("Hi"),
		},
		{
			name:  "empty input",
			input: []byte{},
			want:  []byte{},
		},
		{
			name:  "only EOD",
			input: []byte{0x80},
			want:  []byte{},
		},
		{
			name:    "truncated literal run",
			input:   []byte{5, 'A', 'B'}, // claims 6 bytes but only 2 follow
			wantErr: true,
		},
		{
			name:    "truncated repeat run",
			input:   []byte{255}, // repeat run but no byte follows
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeRunLength(tt.input, maxDecompressedSize)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !bytes.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// lowBudget is a stream budget small enough for a test to cross cheaply.
const lowBudget = 1 << 20

// runLengthBomb returns RunLengthDecode data of n repeat runs of 128 zero
// bytes each: two bytes of input for every 128 of output, the filter's
// greatest expansion (ISO 32000-1, 7.4.5).
func runLengthBomb(n int) []byte {
	data := make([]byte, 0, 2*n+1)
	for range n {
		data = append(data, 0x81, 0x00)
	}
	return append(data, 0x80)
}

// TestDecodeRunLength_BoundedWhileDecoding pins that RunLengthDecode stops at
// the budget as it decodes rather than after building the whole output: a
// stream whose output would be 256 MiB fails under a 1 MiB budget having
// allocated a few MiB at most. A stream exactly at the budget is kept, as
// limitedCopy keeps one.
//
// Not parallel, so no other test allocates while the heap is measured.
func TestDecodeRunLength_BoundedWhileDecoding(t *testing.T) {
	bomb := runLengthBomb(2 << 20) // 2^21 runs of 128 bytes: 256 MiB

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := decodeRunLength(bomb, lowBudget)
	runtime.ReadMemStats(&after)
	if !errors.Is(err, ErrDecompressionBomb) {
		t.Fatalf("256 MiB of runs under a 1 MiB budget: err = %v, want ErrDecompressionBomb", err)
	}
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 16*lowBudget {
		t.Errorf("allocated %d bytes refusing the stream, want well under the %d its output would take", grew, 256*lowBudget)
	}

	exact := runLengthBomb(lowBudget / 128)
	if out, err := decodeRunLength(exact, lowBudget); err != nil || len(out) != lowBudget {
		t.Errorf("output exactly at the budget: %d bytes, %v; want %d bytes", len(out), err, lowBudget)
	}
	over := append(runLengthBomb(lowBudget / 128)[:2*(lowBudget/128)], 0x00, 'x', 0x80)
	if _, err := decodeRunLength(over, lowBudget); !errors.Is(err, ErrDecompressionBomb) {
		t.Errorf("one literal byte past the budget: err = %v, want ErrDecompressionBomb", err)
	}
}
