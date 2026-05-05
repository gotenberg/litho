package compliance

import (
	"bytes"
	"testing"
)

func TestXMPDetectEncoding(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{
			name: "UTF-8 ASCII XML declaration",
			data: []byte(`<?xml version="1.0" encoding="UTF-8"?><x/>`),
			want: "UTF-8",
		},
		{
			name: "UTF-16 BE BOM",
			data: []byte{0xFE, 0xFF, 0x00, '<', 0x00, 'x', 0x00, '/', 0x00, '>'},
			want: "UTF-16BE",
		},
		{
			name: "UTF-16 LE BOM",
			data: []byte{0xFF, 0xFE, '<', 0x00, 'x', 0x00, '/', 0x00, '>', 0x00},
			want: "UTF-16LE",
		},
		{
			name: "UTF-16 LE no BOM",
			data: []byte{'<', 0x00, '?', 0x00, 'x', 0x00, 'm', 0x00, 'l', 0x00},
			want: "UTF-16LE",
		},
		{
			name: "UTF-16 BE no BOM",
			data: []byte{0x00, '<', 0x00, '?', 0x00, 'x', 0x00, 'm', 0x00, 'l'},
			want: "UTF-16BE",
		},
		{
			name: "UTF-32 LE BOM",
			data: []byte{0xFF, 0xFE, 0x00, 0x00, '<', 0, 0, 0, 'x', 0, 0, 0},
			want: "UTF-32LE",
		},
		{
			name: "UTF-32 BE BOM",
			data: []byte{0x00, 0x00, 0xFE, 0xFF, 0, 0, 0, '<', 0, 0, 0, 'x'},
			want: "UTF-32BE",
		},
		{
			name: "UTF-32 LE no BOM",
			data: []byte{'<', 0, 0, 0, '?', 0, 0, 0},
			want: "UTF-32LE",
		},
		{
			name: "UTF-32 BE no BOM",
			data: []byte{0, 0, 0, '<', 0, 0, 0, '?'},
			want: "UTF-32BE",
		},
		{
			name: "UTF-8 with explicit UTF-16 declaration in header",
			data: []byte(`<?xml version="1.0" encoding="UTF-16LE"?>`),
			want: "UTF-16LE",
		},
		{
			name: "empty input is UTF-8",
			data: []byte{},
			want: "UTF-8",
		},
		{
			name: "single byte ASCII",
			data: []byte{'a'},
			want: "UTF-8",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := xmpDetectEncoding(tc.data); got != tc.want {
				t.Errorf("xmpDetectEncoding = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestXMPDecodeToUTF8(t *testing.T) {
	t.Run("UTF-8 passthrough", func(t *testing.T) {
		t.Parallel()
		in := []byte("hello")
		out, err := xmpDecodeToUTF8(in, "UTF-8")
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !bytes.Equal(out, in) {
			t.Errorf("UTF-8 passthrough mutated: %q vs %q", out, in)
		}
	})

	t.Run("UTF-16 BE round trip", func(t *testing.T) {
		t.Parallel()
		// "Hi" in UTF-16 BE with BOM.
		in := []byte{0xFE, 0xFF, 0x00, 'H', 0x00, 'i'}
		out, err := xmpDecodeToUTF8(in, "UTF-16BE")
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if string(out) != "Hi" {
			t.Errorf("UTF-16 BE decode = %q, want %q", out, "Hi")
		}
	})

	t.Run("UTF-16 LE round trip strips BOM", func(t *testing.T) {
		t.Parallel()
		in := []byte{0xFF, 0xFE, 'H', 0x00, 'i', 0x00}
		out, err := xmpDecodeToUTF8(in, "UTF-16LE")
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if string(out) != "Hi" {
			t.Errorf("UTF-16 LE decode = %q, want %q", out, "Hi")
		}
	})

	t.Run("UTF-32 LE round trip", func(t *testing.T) {
		t.Parallel()
		in := []byte{0xFF, 0xFE, 0x00, 0x00, 'H', 0, 0, 0, 'i', 0, 0, 0}
		out, err := xmpDecodeToUTF8(in, "UTF-32LE")
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if string(out) != "Hi" {
			t.Errorf("UTF-32 LE decode = %q, want %q", out, "Hi")
		}
	})

	t.Run("UTF-32 BE round trip", func(t *testing.T) {
		t.Parallel()
		in := []byte{0x00, 0x00, 0xFE, 0xFF, 0, 0, 0, 'H', 0, 0, 0, 'i'}
		out, err := xmpDecodeToUTF8(in, "UTF-32BE")
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if string(out) != "Hi" {
			t.Errorf("UTF-32 BE decode = %q, want %q", out, "Hi")
		}
	})
}

func TestXMPEncodingRule(t *testing.T) {
	tests := []struct {
		part    int
		wantHit bool
	}{
		{1, false}, // PDF/A-1 has no UTF-8 mandate
		{2, true},
		{3, true},
		{4, true},
		{5, false},
	}
	for _, tc := range tests {
		got := xmpEncodingRule(tc.part, "UTF-16LE")
		if (len(got) > 0) != tc.wantHit {
			t.Errorf("xmpEncodingRule(%d) hit=%v, want %v", tc.part, len(got) > 0, tc.wantHit)
		}
	}
}

func TestXMPNormalizeForChecks(t *testing.T) {
	t.Run("UTF-8 passes through silently", func(t *testing.T) {
		t.Parallel()
		var results []Violation
		in := []byte(`<?xml version="1.0" encoding="UTF-8"?>`)
		out := xmpNormalizeForChecks(in, 2, &results)
		if !bytes.Equal(out, in) {
			t.Errorf("UTF-8 mutated by normaliser")
		}
		if len(results) != 0 {
			t.Errorf("UTF-8 emitted %d violations, want 0", len(results))
		}
	})

	t.Run("UTF-16 LE emits violation and decodes", func(t *testing.T) {
		t.Parallel()
		var results []Violation
		// "<x/>" in UTF-16 LE with BOM.
		in := []byte{0xFF, 0xFE, '<', 0, 'x', 0, '/', 0, '>', 0}
		out := xmpNormalizeForChecks(in, 2, &results)
		if string(out) != "<x/>" {
			t.Errorf("decoded = %q, want %q", out, "<x/>")
		}
		if len(results) != 1 {
			t.Fatalf("violation count = %d, want 1", len(results))
		}
	})

	t.Run("UTF-16 in PDF/A-1 still decodes but emits no rule", func(t *testing.T) {
		t.Parallel()
		var results []Violation
		in := []byte{0xFF, 0xFE, '<', 0, 'x', 0, '/', 0, '>', 0}
		out := xmpNormalizeForChecks(in, 1, &results)
		if string(out) != "<x/>" {
			t.Errorf("PDF/A-1 decode = %q, want %q", out, "<x/>")
		}
		if len(results) != 0 {
			t.Errorf("PDF/A-1 emitted %d violations, want 0", len(results))
		}
	})
}
