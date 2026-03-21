package cmap

import (
	"testing"
)

func TestBcmapStreamReadNumber(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want int
	}{
		{"single byte zero", []byte{0x00}, 0},
		{"single byte 127", []byte{0x7F}, 127},
		{"two bytes", []byte{0x81, 0x00}, 128},
		{"two bytes max", []byte{0xFF, 0x7F}, 16383},
		{"three bytes", []byte{0x81, 0x80, 0x00}, 16384},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &bcmapStream{data: tt.data}
			got, err := s.readNumber()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestBcmapStreamReadSigned(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want int
	}{
		{"zero", []byte{0x00}, 0},
		{"positive 1", []byte{0x02}, 1},
		{"negative 1", []byte{0x01}, -1},
		{"positive 2", []byte{0x04}, 2},
		{"negative 2", []byte{0x03}, -2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &bcmapStream{data: tt.data}
			got, err := s.readSigned()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestBcmapStreamReadHex(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		dataSize int
		want     int
	}{
		{"1 byte", []byte{0x42}, 0, 0x42},
		{"2 bytes", []byte{0x00, 0x20}, 1, 0x0020},
		{"2 bytes high", []byte{0xFF, 0xFF}, 1, 0xFFFF},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &bcmapStream{data: tt.data}
			got, err := s.readHex(tt.dataSize)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got 0x%X, want 0x%X", got, tt.want)
			}
		})
	}
}

func TestParseBCMapCIDToUnicode_Empty(t *testing.T) {
	_, err := parseBCMapCIDToUnicode(nil)
	if err == nil {
		t.Error("expected error for empty data")
	}
}

func TestParseBCMapCIDToUnicode_Japan1(t *testing.T) {
	data, err := bcmapFS.ReadFile("bcmaps/UniJIS-UCS2-H.bcmap")
	if err != nil {
		t.Fatalf("reading bcmap: %v", err)
	}
	m, err := parseBCMapCIDToUnicode(data)
	if err != nil {
		t.Fatalf("parsing bcmap: %v", err)
	}
	if len(m) < 1000 {
		t.Errorf("map too small: %d entries", len(m))
	}
	// CID 1 should map to U+0020 (space) per Adobe-Japan1.
	if m[1] != 0x0020 {
		t.Errorf("CID 1: got U+%04X, want U+0020", m[1])
	}
}

func TestParseBCMapCIDToUnicode_AllCollections(t *testing.T) {
	files := map[string]string{
		"Japan1": "bcmaps/UniJIS-UCS2-H.bcmap",
		"GB1":    "bcmaps/UniGB-UCS2-H.bcmap",
		"CNS1":   "bcmaps/UniCNS-UCS2-H.bcmap",
		"Korea1": "bcmaps/UniKS-UCS2-H.bcmap",
	}
	for name, path := range files {
		t.Run(name, func(t *testing.T) {
			data, err := bcmapFS.ReadFile(path)
			if err != nil {
				t.Fatalf("reading bcmap: %v", err)
			}
			m, err := parseBCMapCIDToUnicode(data)
			if err != nil {
				t.Fatalf("parsing bcmap: %v", err)
			}
			if len(m) < 100 {
				t.Errorf("only %d entries", len(m))
			}
		})
	}
}
