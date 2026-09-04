package stdfonts

import "testing"

// TestCMapNormalisesTheBaseFontName pins the name handling [CMap] documents: a
// PDF /BaseFont carries subset prefixes and style suffixes, and the lookup has
// to see through both to the family keyword.
func TestCMapNormalisesTheBaseFontName(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"Arial",
		"ABCDEF+Arial",     // subset prefix
		"ArialMT",          // PostScript suffix
		"Arial,Bold",       // style suffix
		"Arial-ItalicMT",   // both
		"arial",            // lower case
		"ABCDEF+ArialPSMT", // prefix and suffix together
		"Helvetica",        // the alias the switch treats as Arial
		"XYZABC+Helvetica", // alias with a prefix
	} {
		if got := CMap(name); got == nil {
			t.Errorf("CMap(%q) = nil, want the Arial table", name)
		}
	}
}

// TestCMapSeparatesTheBundledFamilies pins that the two tables are distinct:
// matching the wrong family would resolve a CID to the wrong character.
func TestCMapSeparatesTheBundledFamilies(t *testing.T) {
	t.Parallel()

	arial, calibri := CMap("Arial"), CMap("Calibri")
	if arial == nil || calibri == nil {
		t.Fatal("one of the bundled families is missing")
	}
	if len(arial) == len(calibri) {
		// Not a correctness requirement in itself, but equal sizes would make
		// the check below vacuous if the two ever became the same map.
		t.Logf("both tables hold %d entries", len(arial))
	}
	// Arial GID 179 is the left double quotation mark. Calibri does not key
	// that GID to the same rune, which is the whole reason for two tables.
	if arial[179] != '“' {
		t.Errorf("Arial GID 179 = %q, want %q", arial[179], '“')
	}
	if calibri[179] == arial[179] && calibri[179] != 0 {
		t.Errorf("the two families resolved GID 179 identically (%q)", arial[179])
	}
}

// TestCMapReturnsNilForAnUnbundledFamily pins the contract the caller depends
// on: nil means "fall back to the CID identity offset path", so a family with
// no table must not resolve to another family's.
//
// Times New Roman is the case to watch. The package doc named it for a while
// although no table for it is bundled, and a caller trusting that would have
// taken Arial's answers for Times glyph IDs.
func TestCMapReturnsNilForAnUnbundledFamily(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"",
		"TimesNewRoman",
		"TimesNewRomanPSMT",
		"ABCDEF+TimesNewRoman",
		"Courier",
		"CourierNew",
		"Georgia",
		"SomeFontNobodyShips",
	} {
		if got := CMap(name); got != nil {
			t.Errorf("CMap(%q) returned a table of %d entries, want nil", name, len(got))
		}
	}
}
