package pdf

import "testing"

func TestPermissions_Mask_RoundTrip(t *testing.T) {
	cases := []Permissions{
		{Print: true, ModifyContent: true, ExtractText: true, ModifyAnnotations: true, FillForms: true, Assemble: true, PrintHighQuality: true},
		{}, // all denied
		{Print: true, ExtractText: true},
		{FillForms: true, Assemble: true, PrintHighQuality: true},
	}
	for _, p := range cases {
		m := p.Mask()
		// Reserved low bits 1-2 must be clear (ISO 32000-2 Table 22).
		if m&0b11 != 0 {
			t.Errorf("%+v: reserved bits 1-2 set in mask %#x", p, uint32(m))
		}
		// Decoding the mask the way Reader.Permissions does must recover p.
		got := Permissions{
			Print:             m&4 != 0,
			ModifyContent:     m&8 != 0,
			ExtractText:       m&16 != 0,
			ModifyAnnotations: m&32 != 0,
			FillForms:         m&256 != 0,
			Assemble:          m&1024 != 0,
			PrintHighQuality:  m&2048 != 0,
		}
		if got != p {
			t.Errorf("round-trip mismatch: Mask(%+v) decoded to %+v", p, got)
		}
	}

	// All permissions granted is the conventional 0xFFFFFFFC value.
	all := Permissions{
		Print:             true,
		ModifyContent:     true,
		ExtractText:       true,
		ModifyAnnotations: true,
		FillForms:         true,
		Assemble:          true,
		PrintHighQuality:  true,
	}
	if got := uint32(all.Mask()); got != 0xFFFFFFFC {
		t.Errorf("all-permissions Mask() = %#x, want 0xFFFFFFFC", got)
	}
}
