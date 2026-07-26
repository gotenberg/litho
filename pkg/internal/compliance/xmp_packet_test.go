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

// TestXmpWellFormed_ControlChars pins the one relaxation of XML 1.0 §2.2 that
// litho grants: a C0 control character, raw or written as a numeric character
// reference, does not by itself make a packet malformed. veraPDF parses with
// FIX_CONTROL_CHARS on by default and substitutes a space for each such
// character, so reporting §6.6.2.1 t4 / §6.7.2.1 t4 on that construct alone
// diverges from every XMPCore-based validator. Real files depend on it: the
// govdocs corpus file thread_300_300842.pdf holds "&#x5;" inside a
// dc:description, and veraPDF reports schema violations against that packet's
// properties, proving it parsed. Defects that are not control characters still
// fail.
func TestXmpWellFormed_ControlChars(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data string
		want bool
	}{
		{"hex reference to control char", `<a>&#x5;x</a>`, true},
		{"decimal reference to control char", `<a>&#5;x</a>`, true},
		{"raw control char", "<a>\x05x</a>", true},
		{"legal reference untouched", `<a>&#x41;</a>`, true},
		{"tab reference untouched", `<a>&#x9;</a>`, true},
		{"malformed reference is not a control char", `<a>&#xZZ;</a>`, false},
		{"mismatched tag still fails", "<a>\x05</b>", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := xmpWellFormed([]byte(tc.data)); got != tc.want {
				t.Errorf("xmpWellFormed(%q) = %v, want %v", tc.data, got, tc.want)
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
	t.Run("malformed fires t4+t5 part 4", func(t *testing.T) {
		t.Parallel()
		vs := xmpPacketSerializationValidity(malformed, 4)
		if !hasRule(vs, Rule_19005_4_6_7_2_1_t04) || !hasRule(vs, Rule_19005_4_6_7_2_1_t05) {
			t.Errorf("expected §6.7.2.1 t4+t5 (part 4), got %v", vs)
		}
	})
	t.Run("malformed fires single 6.7.9-t01 part 1", func(t *testing.T) {
		t.Parallel()
		// PDF/A-1 §6.7.9 t01 has only the well-formedness half, no t5 encoding rule.
		vs := xmpPacketSerializationValidity(malformed, 1)
		if !hasRule(vs, Rule_19005_1_6_7_9_t01) {
			t.Errorf("expected §6.7.9 t01 (part 1), got %v", vs)
		}
		if len(vs) != 1 {
			t.Errorf("part 1 fires only the single well-formedness rule, got %v", vs)
		}
		if vs := xmpPacketSerializationValidity(wellFormed, 1); len(vs) != 0 {
			t.Errorf("well-formed part 1 must not fire, got %v", vs)
		}
	})
}
