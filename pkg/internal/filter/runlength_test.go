package filter

import (
	"bytes"
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
			got, err := decodeRunLength(tt.input)
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
