package cmap

import (
	"context"
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
	cm, err := Parse(context.Background(), data)
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
	cm, err := Parse(context.Background(), data)
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
	cm, err := Parse(context.Background(), data)
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

func TestParse_BfRangeCrossingFirstByte(t *testing.T) {
	// Adobe Technical Note #5411, 1.4.1 rules a bfrange whose source codes
	// cross a first-byte boundary invalid. Mapping still expands it for text
	// extraction; StrictMapping keeps the start code's leading bytes and the
	// end code's last byte, as veraPDF does.
	const codespace = "1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n"
	tests := []struct {
		name      string
		body      string
		mapped    map[string]string
		strict    map[string]string
		notStrict []string
	}{
		{
			name:      "end last byte below start covers no conforming code",
			body:      "1 beginbfrange\n<01ED> <0206> <0061>\nendbfrange\n",
			mapped:    map[string]string{"\x01\xED": "a", "\x01\xFF": "s", "\x02\x00": "t", "\x02\x06": "z"},
			notStrict: []string{"\x01\xED", "\x01\xFF", "\x02\x00", "\x02\x06"},
		},
		{
			name:      "conforming part stops at the end code's last byte",
			body:      "1 beginbfrange\n<0105> <0207> <0041>\nendbfrange\n",
			mapped:    map[string]string{"\x01\x05": "A", "\x01\x08": "D", "\x02\x07": "Ń"},
			strict:    map[string]string{"\x01\x05": "A", "\x01\x07": "C"},
			notStrict: []string{"\x01\x08", "\x02\x07"},
		},
		{
			name:      "array destination is clamped the same way",
			body:      "1 beginbfrange\n<01FE> <02FF> [<0030> <0031> <0032>]\nendbfrange\n",
			mapped:    map[string]string{"\x01\xFE": "0", "\x01\xFF": "1", "\x02\x00": "2"},
			strict:    map[string]string{"\x01\xFE": "0", "\x01\xFF": "1"},
			notStrict: []string{"\x02\x00"},
		},
		{
			name:   "conforming range is kept whole",
			body:   "1 beginbfrange\n<10FE> <10FF> <4E00>\nendbfrange\n",
			mapped: map[string]string{"\x10\xFE": "一", "\x10\xFF": "丁"},
			strict: map[string]string{"\x10\xFE": "一", "\x10\xFF": "丁"},
		},
		{
			name: "conforming entries before and after survive in the strict view",
			body: "1 beginbfchar\n<0110> <0058>\nendbfchar\n" +
				"1 beginbfrange\n<0105> <0207> <0041>\nendbfrange\n" +
				"1 beginbfchar\n<0200> <0059>\nendbfchar\n",
			mapped: map[string]string{"\x01\x10": "L", "\x02\x00": "Y"},
			strict: map[string]string{"\x01\x10": "X", "\x02\x00": "Y", "\x01\x05": "A"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm, err := Parse(context.Background(), []byte(codespace+tt.body))
			if err != nil {
				t.Fatal(err)
			}
			for code, want := range tt.mapped {
				if got := cm.Mapping[code]; got != want {
					t.Errorf("Mapping[%X] = %q, want %q", code, got, want)
				}
			}
			strict := cm.StrictMapping()
			for code, want := range tt.strict {
				if got, ok := strict[code]; !ok || got != want {
					t.Errorf("StrictMapping()[%X] = %q (present %v), want %q", code, got, ok, want)
				}
			}
			for _, code := range tt.notStrict {
				if got, ok := strict[code]; ok {
					t.Errorf("StrictMapping()[%X] = %q, want absent", code, got)
				}
			}
		})
	}
}

func TestParse_Empty(t *testing.T) {
	cm, err := Parse(context.Background(), []byte{})
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
	cm, err := Parse(context.Background(), data)
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
