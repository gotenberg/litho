package compliance

import "testing"

func TestXmpWellFormed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data string
		want bool
	}{
		{"valid", `<?xpacket begin=''?><x:xmpmeta xmlns:x="adobe:ns:meta/"><a>x</a></x:xmpmeta>`, true},
		// The double-encoded BOM (the 6-7-2-1 quirk) is harmless attribute content.
		{"valid with quirk BOM", "<?xpacket begin='\xc3\xaf\xc2\xbb\xc2\xbf'?><x:xmpmeta xmlns:x=\"adobe:ns:meta/\"><a>x</a></x:xmpmeta>", true},
		{"mismatched tag", `<x:xmpmeta xmlns:x="adobe:ns:meta/"><dc:creator xmlns:dc="d">x</dc:Creator></x:xmpmeta>`, false},
		{"unclosed tag", `<x:xmpmeta xmlns:x="adobe:ns:meta/"><a>x</x:xmpmeta>`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := xmpWellFormed([]byte(tc.data)); got != tc.want {
				t.Errorf("xmpWellFormed(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestXmpPacketSerializationValidity(t *testing.T) {
	t.Parallel()
	// A mismatched start/end tag, the 6-7-2-1-t01-fail-d defect.
	malformed := []byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><dc:creator xmlns:dc="d">x</dc:Creator></x:xmpmeta>`)
	wellFormed := []byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><a>x</a></x:xmpmeta>`)

	t.Run("malformed fires t4+t5 part 2", func(t *testing.T) {
		t.Parallel()
		vs := xmpPacketSerializationValidity(malformed, 2)
		if !hasRule(vs, Rule_19005_2_6_6_2_1_t04) || !hasRule(vs, Rule_19005_2_6_6_2_1_t05) {
			t.Errorf("expected t4+t5 (part 2), got %v", vs)
		}
	})
	t.Run("malformed fires t4+t5 part 3", func(t *testing.T) {
		t.Parallel()
		vs := xmpPacketSerializationValidity(malformed, 3)
		if !hasRule(vs, Rule_19005_3_6_6_2_1_t04) || !hasRule(vs, Rule_19005_3_6_6_2_1_t05) {
			t.Errorf("expected t4+t5 (part 3), got %v", vs)
		}
	})
	t.Run("well-formed does not fire", func(t *testing.T) {
		t.Parallel()
		if vs := xmpPacketSerializationValidity(wellFormed, 2); len(vs) != 0 {
			t.Errorf("well-formed packet must not fire, got %v", vs)
		}
	})
	t.Run("parts 1 and 4 never fire", func(t *testing.T) {
		t.Parallel()
		for _, p := range []int{1, 4} {
			if vs := xmpPacketSerializationValidity(malformed, p); len(vs) != 0 {
				t.Errorf("part %d has no §6.6.2.1 t4/t5, got %v", p, vs)
			}
		}
	})
}
