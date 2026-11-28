package text

import "testing"

func TestLibreOfficeSubstitute(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		family   string
		fixed    bool
		declared string
		want     string
	}{
		{"Comic Sans MS", false, "", "Z003"},
		{"Consolas", false, "", "DejaVu Sans Mono"},
		{"Garamond", false, "", "Noto Serif"},
		{"Georgia", false, "", "Noto Serif"},
		{"Aptos", false, "", "Noto Sans"},
		{"Impact", false, "", "Noto Sans"},
		{"Segoe UI", false, "", "Noto Sans"},
		{"Times-Roman", false, "", "Noto Sans"},
		{"Palatino Linotype", false, "", "P052"},
		{"Century Schoolbook", false, "", "C059"},
		{"Bookman Old Style", false, "", "URW Bookman"},
		// A declared serif or sans-serif generic replaces the family's own,
		// a URW stand-in aside; monospace, cursive and fantasy do not.
		{"Comic Sans MS", false, "serif", "Noto Serif"},
		{"Consolas", false, "sans-serif", "Noto Sans"},
		{"Aptos", false, "serif", "Noto Serif"},
		{"Aptos", false, "monospace", "Noto Sans"},
		{"Comic Sans MS", false, "fantasy", "Z003"},
		{"Palatino Linotype", false, "sans-serif", "P052"},
		// A fixed pitch takes the monospace face over either.
		{"Aptos", true, "", "DejaVu Sans Mono"},
		{"Aptos", true, "serif", "DejaVu Sans Mono"},
		{"Comic Sans MS", true, "", "DejaVu Sans Mono"},
		{"Palatino Linotype", true, "", "P052"},
	} {
		if got := LibreOfficeSubstitute(tc.family, tc.fixed, tc.declared); got != tc.want {
			t.Errorf("LibreOfficeSubstitute(%q, %v, %q) = %q, want %q", tc.family, tc.fixed, tc.declared, got, tc.want)
		}
	}
}

// substituteSink keeps the results of the allocation test alive.
var substituteSink string

func TestLibreOfficeSubstitute_AllocatesNothing(t *testing.T) {
	if n := testing.AllocsPerRun(100, func() {
		substituteSink = LibreOfficeSubstitute("Comic Sans MS", false, "serif")
		substituteSink = LibreOfficeSubstitute("Liberation Serif", false, "")
	}); n != 0 {
		t.Errorf("%v allocations a call, want 0", n)
	}
	for _, name := range []string{"Comic Sans MS", " \"Palatino  Linotype\" ", "DejaVu\tSans", "Times-Roman"} {
		var buf [64]byte
		if got, want := string(asciiFamilyKey(buf[:0], name)), familyKey(name); got != want {
			t.Errorf("asciiFamilyKey(%q) = %q, want %q", name, got, want)
		}
	}
}
