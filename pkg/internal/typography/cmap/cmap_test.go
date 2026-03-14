package cmap

import (
	"testing"
)

func TestDecodeUTF16BE(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"empty", nil, ""},
		{"single byte", []byte{0x41}, "A"},
		{"ASCII via UTF16", []byte{0x00, 0x41}, "A"},
		{"non-ASCII", []byte{0x00, 0xE9}, "é"},
		{"CJK char", []byte{0x4E, 0x2D}, "中"},
		{"odd length padded", []byte{0x00, 0x41, 0x00}, "A\x00"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeUTF16BE(tc.data)
			if got != tc.want {
				t.Errorf("decodeUTF16BE(%v) = %q, want %q", tc.data, got, tc.want)
			}
		})
	}
}

func TestIncrementBytes(t *testing.T) {
	tests := []struct {
		name   string
		input  []byte
		expect []byte
	}{
		{"simple", []byte{0x00, 0x01}, []byte{0x00, 0x02}},
		{"carry", []byte{0x00, 0xFF}, []byte{0x01, 0x00}},
		{"all FF", []byte{0xFF, 0xFF}, []byte{0x00, 0x00}},
		{"single", []byte{0x05}, []byte{0x06}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := make([]byte, len(tc.input))
			copy(b, tc.input)
			incrementBytes(b)
			for i := range b {
				if b[i] != tc.expect[i] {
					t.Errorf("incrementBytes(%v) = %v, want %v", tc.input, b, tc.expect)
					break
				}
			}
		})
	}
}

func TestParse_BfChar(t *testing.T) {
	// Minimal CMap with beginbfchar/endbfchar.
	data := []byte(`
/CIDInit /ProcSet findresource begin
1 beginbfchar
<01> <0041>
endbfchar
end
`)
	cm, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(cm.Mapping) != 1 {
		t.Fatalf("expected 1 mapping, got %d", len(cm.Mapping))
	}
	if val, ok := cm.Mapping["\x01"]; !ok || val != "A" {
		t.Errorf("mapping[\\x01] = %q, want %q", val, "A")
	}
}

func TestParse_BfRange(t *testing.T) {
	data := []byte(`
1 beginbfrange
<01> <03> <0041>
endbfrange
`)
	cm, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	// Should map 01->A, 02->B, 03->C
	if len(cm.Mapping) != 3 {
		t.Fatalf("expected 3 mappings, got %d", len(cm.Mapping))
	}
	if cm.Mapping["\x01"] != "A" {
		t.Errorf("mapping[\\x01] = %q, want A", cm.Mapping["\x01"])
	}
	if cm.Mapping["\x02"] != "B" {
		t.Errorf("mapping[\\x02] = %q, want B", cm.Mapping["\x02"])
	}
	if cm.Mapping["\x03"] != "C" {
		t.Errorf("mapping[\\x03] = %q, want C", cm.Mapping["\x03"])
	}
}

func TestParse_BfRangeWithArray(t *testing.T) {
	data := []byte(`
1 beginbfrange
<01> <02> [<0058> <0059>]
endbfrange
`)
	cm, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(cm.Mapping) != 2 {
		t.Fatalf("expected 2 mappings, got %d", len(cm.Mapping))
	}
	if cm.Mapping["\x01"] != "X" {
		t.Errorf("mapping[\\x01] = %q, want X", cm.Mapping["\x01"])
	}
	if cm.Mapping["\x02"] != "Y" {
		t.Errorf("mapping[\\x02] = %q, want Y", cm.Mapping["\x02"])
	}
}

func TestParse_Empty(t *testing.T) {
	cm, err := Parse([]byte{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cm.Mapping) != 0 {
		t.Errorf("expected empty mapping, got %d", len(cm.Mapping))
	}
}

func TestParse_MultipleSections(t *testing.T) {
	data := []byte(`
1 beginbfchar
<10> <0041>
endbfchar
1 beginbfrange
<20> <22> <0058>
endbfrange
`)
	cm, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	// 1 from bfchar + 3 from bfrange
	if len(cm.Mapping) != 4 {
		t.Fatalf("expected 4 mappings, got %d", len(cm.Mapping))
	}
	if cm.Mapping["\x10"] != "A" {
		t.Errorf("bfchar mapping[\\x10] = %q, want A", cm.Mapping["\x10"])
	}
	if cm.Mapping["\x20"] != "X" {
		t.Errorf("bfrange mapping[\\x20] = %q, want X", cm.Mapping["\x20"])
	}
}

func TestMap_LongestPrefix(t *testing.T) {
	cm := &CMap{
		Mapping: map[string]string{
			"\x01":     "A",
			"\x01\x02": "B",
		},
	}

	// Two-byte match should take priority.
	n, val, ok := cm.Map([]byte{0x01, 0x02, 0x03})
	if !ok || n != 2 || val != "B" {
		t.Errorf("Map([01 02 03]) = (%d, %q, %v), want (2, B, true)", n, val, ok)
	}
}

func TestMap_SingleByte(t *testing.T) {
	cm := &CMap{
		Mapping: map[string]string{
			"\x41": "A",
		},
	}

	n, val, ok := cm.Map([]byte{0x41})
	if !ok || n != 1 || val != "A" {
		t.Errorf("Map([41]) = (%d, %q, %v), want (1, A, true)", n, val, ok)
	}
}

func TestMap_NoMatch(t *testing.T) {
	cm := &CMap{
		Mapping: map[string]string{
			"\x01": "A",
		},
	}

	n, val, ok := cm.Map([]byte{0xFF})
	if ok || n != 0 || val != "" {
		t.Errorf("Map([FF]) = (%d, %q, %v), want (0, \"\", false)", n, val, ok)
	}
}

func TestMap_EmptyData(t *testing.T) {
	cm := &CMap{Mapping: map[string]string{}}
	n, val, ok := cm.Map([]byte{})
	if ok || n != 0 {
		t.Errorf("Map([]) = (%d, %q, %v), want (0, \"\", false)", n, val, ok)
	}
}
