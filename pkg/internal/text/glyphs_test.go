package text

import "testing"

// TestFaceByKey_FindsTheParsedFont finds a face of the index by its key
// and reads its glyphs from the index's cache.
func TestFaceByKey_FindsTheParsedFont(t *testing.T) {
	t.Parallel()
	f := newTestFonts(t,
		fontFile{name: "a.ttf", data: familyFace("Alpha", "Regular", 400, false)},
		fontFile{name: "b.ttf", data: familyFace("Beta", "Regular", 400, false)},
	)
	b := f.Match(Style{Families: []string{"Beta"}, Size: 10})
	got := f.FaceByKey(b.Key())
	if got != b {
		t.Fatalf("FaceByKey(%q) = %v, want the Beta face", b.Key(), got)
	}
	if got.GlyphFace() == nil {
		t.Fatal("no parsed font")
	}
	if f.FaceByKey("nowhere#0") != nil {
		t.Fatal("a key no face has found one")
	}
}
