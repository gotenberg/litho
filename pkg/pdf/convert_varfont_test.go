package pdf

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// varFontDir returns a font directory holding the variable test fonts:
// LithoVar (TrueType, wght 100 to 900, named instances Light at 300 and
// Bold at 733.33 with the PostScript name LithoVar-BoldInstance, opsz 8 to
// 144) and LithoVarCFF (CFF2, wght 200 to 900, named instances Regular at
// 450 and Bold at 742.1, PostScript name LithoVarCFF-BoldInstance).
func varFontDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, src := range map[string]string{
		"var.ttf":  "../internal/typography/varfont/testdata/varfont.ttf",
		"cff2.otf": "../internal/typography/varfont/testdata/varfont_cff2.otf",
	} {
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// baseFontsOf returns the BaseFont names of a PDF 1.4 file, whose
// dictionaries no object stream compresses, without their subset tags.
func baseFontsOf(t *testing.T, data []byte) []string {
	t.Helper()
	var out []string
	for _, m := range regexp.MustCompile(`/BaseFont\s*/(?:[A-Z]{6}\+)?([^\s/<>\[\]()]+)`).FindAllSubmatch(data, -1) {
		if name := string(m[1]); !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// TestConvert_InstalledVariableFonts pins what a conversion embeds for an
// installed variable font: HTML the named instance nearest the weight, as
// Chromium draws one, its CFF2 one with CFF outlines; an ODT document the
// named instance with opsz at the font size, as LibreOffice draws one.
func TestConvert_InstalledVariableFonts(t *testing.T) {
	t.Parallel()
	dir := varFontDir(t)
	ctx := context.Background()
	html := `<p style="font-family: LithoVar; font-weight: 300">AB</p>` +
		`<p style="font-family: LithoVar; font-weight: 700">AB</p>` +
		`<p style="font-family: LithoVarCFF; font-weight: 800">OH</p>`
	var out bytes.Buffer
	if _, err := FromHTML(strings.NewReader(html), WithConvertFontDirs(dir), WithConvertVersion("1.4")).WriteTo(ctx, &out); err != nil {
		t.Fatal(err)
	}
	want := []string{"LithoVar-BoldInstance", "LithoVar-Light", "LithoVarCFF-BoldInstance"}
	if got := baseFontsOf(t, out.Bytes()); !slices.Equal(got, want) {
		t.Errorf("HTML embeds %q, want %q", got, want)
	}
	if !bytes.Contains(out.Bytes(), []byte("/CIDFontType0C")) {
		t.Error("the CFF2 font is not embedded as CIDFontType0C")
	}

	odt := wordODT(`<style:style style:name="B" style:family="text"><style:text-properties fo:font-weight="bold"/></style:style>`,
		`<text:p><text:span text:style-name="B">AB</text:span></text:p>`)
	odt = strings.Replace(odt, `<office:automatic-styles>`,
		`<office:font-face-decls><style:font-face style:name="V" svg:font-family="LithoVar"/></office:font-face-decls><office:automatic-styles>`, 1)
	odt = strings.Replace(odt, `<style:text-properties fo:font-weight="bold"/>`, `<style:text-properties style:font-name="V" fo:font-size="10pt" fo:font-weight="bold"/>`, 1)
	if !strings.Contains(odt, `xmlns:svg=`) {
		odt = strings.Replace(odt, `<office:document `, `<office:document xmlns:svg="urn:oasis:names:tc:opendocument:xmlns:svg-compatible:1.0" `, 1)
	}
	out.Reset()
	if _, err := FromODT(strings.NewReader(odt), WithConvertFontDirs(dir), WithConvertVersion("1.4")).WriteTo(ctx, &out); err != nil {
		t.Fatal(err)
	}
	if got := baseFontsOf(t, out.Bytes()); !slices.Contains(got, "LithoVar_733333wght_100wdth_0slnt_10opsz_0TEST") {
		t.Errorf("ODT embeds %q, want the Bold instance at opsz 10", got)
	}
}
