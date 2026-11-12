package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func TestToUnicodeRunes(t *testing.T) {
	t.Parallel()
	cmap := []byte(`/CIDInit /ProcSet findresource begin
begincmap
1 begincodespacerange <0000> <FFFF> endcodespacerange
2 beginbfchar
<0003> <0041>
<0004> <D83DDE00>
endbfchar
3 beginbfrange
<0005> <0007> <0627>
<0008> <0009> [<05D0> <0066 0069>]
<00000A> <01000A> <0020>
endbfrange
endcmap`)
	got := toUnicodeRunes(cmap)
	want := []rune{'A', '😀', 'ا', 'ب', 'ة', 'א', 'f', 'i'}
	if !slices.Equal(got, want) {
		t.Errorf("toUnicodeRunes = %q, want %q", got, want)
	}
	if got := toUnicodeRunes([]byte("begincmap 1 beginbfrange <0001> endbfrange")); len(got) != 0 {
		t.Errorf("a truncated range gave %q", got)
	}
}

func TestCoverage_FoldsRunesIntoRanges(t *testing.T) {
	t.Parallel()
	c := coverage{{'A', 'C'}, {'a', 'a'}, {0x4E00, 0x9FFF}}
	for r, want := range map[rune]bool{'A': true, 'B': true, 'C': true, 'D': false, 'a': true, 'b': false, 0x4E00: true, 0x9FFF: true, 0xA000: false, 0: false} {
		if got := c.covers(r); got != want {
			t.Errorf("covers(%U) = %v, want %v", r, got, want)
		}
	}
	if (coverage(nil)).covers('A') {
		t.Error("an empty coverage covers A")
	}
}

// TestReferenceFaceText reads the characters of each face from the
// ToUnicode CMaps of a file whose font dictionaries sit in an object stream,
// and leaves out a face without one.
func TestReferenceFaceText(t *testing.T) {
	t.Parallel()
	qpdf, err := exec.LookPath("qpdf")
	if err != nil {
		t.Skip("qpdf not found")
	}
	d := pdfgen.New()
	toUnicode := d.Stream([]byte("begincmap\n1 beginbfrange\n<01> <02> [<0627> <0628>]\nendbfrange\nendcmap"))
	d.AddPage(pdfgen.D(
		"Type", pdfgen.N("Page"),
		"MediaBox", pdfgen.A(0, 0, 200, 200),
		"Contents", d.Stream([]byte("BT /F1 12 Tf 10 10 Td <0102> Tj ET")),
		"Resources", pdfgen.D("Font", pdfgen.D(
			"F1", d.Indirect(pdfgen.D("Type", pdfgen.N("Font"), "Subtype", pdfgen.N("TrueType"), "BaseFont", pdfgen.N("BCDEEE+Arial"), "ToUnicode", toUnicode)),
			"F2", d.Indirect(pdfgen.D("Type", pdfgen.N("Font"), "Subtype", pdfgen.N("TrueType"), "BaseFont", pdfgen.N("Calibri"))),
		)),
	))
	out, err := d.Save(pdfgen.SaveOptions{GenerateObjectStreams: true, CompressStreams: true})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "ref.pdf")
	if err := os.WriteFile(path, out.Bytes, 0o600); err != nil {
		t.Fatal(err)
	}
	drawn, err := referenceFaceText(context.Background(), qpdf, path)
	if err != nil {
		t.Fatal(err)
	}
	if got := drawn["Arial"]; !slices.Equal(got, []rune("اب")) {
		t.Errorf("Arial draws %q, want اب", got)
	}
	if _, ok := drawn["Calibri"]; ok || len(drawn) != 1 {
		t.Errorf("drawn = %q, want Arial alone", drawn)
	}
	missing, err := referenceMissingFaces(context.Background(), qpdf, path, testFixtureFaces())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(missing, []string{"Arial"}) {
		t.Errorf("missing = %q, want Arial, whose stand-in lacks Arabic", missing)
	}
}
