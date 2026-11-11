package odf

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

const ns = `xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" ` +
	`xmlns:style="urn:oasis:names:tc:opendocument:xmlns:style:1.0" ` +
	`xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0" ` +
	`xmlns:fo="urn:oasis:names:tc:opendocument:xmlns:xsl-fo-compatible:1.0" ` +
	`xmlns:svg="urn:oasis:names:tc:opendocument:xmlns:svg-compatible:1.0"`

// zipOf returns a zip of the entries, name then body, the first stored.
func zipOf(t testing.TB, entries ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for i := 0; i+1 < len(entries); i += 2 {
		m := zip.Deflate
		if i == 0 {
			m = zip.Store
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: entries[i], Method: m})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(entries[i+1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const minimalContent = `<office:document-content ` + ns + `><office:body><office:text><text:p>a</text:p></office:text></office:body></office:document-content>`

func TestOpen_Package(t *testing.T) {
	manifest := `<manifest:manifest xmlns:manifest="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0" xmlns:loext="urn:org:documentfoundation:names:experimental:office:xmlns:loext:1.0">` +
		`<manifest:file-entry manifest:full-path="/" manifest:media-type="application/vnd.oasis.opendocument.text"/>` +
		`<manifest:file-entry manifest:full-path="Pictures/a.png" manifest:media-type="image/png" manifest:size="12">` +
		`<manifest:encryption-data manifest:checksum-type="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0#sha256-1k" manifest:checksum="AAEC">` +
		`<manifest:algorithm manifest:algorithm-name="http://www.w3.org/2001/04/xmlenc#aes256-cbc" manifest:initialisation-vector="AwQ="/>` +
		`<manifest:start-key-generation manifest:start-key-generation-name="http://www.w3.org/2000/09/xmldsig#sha256" manifest:key-size="32"/>` +
		`<manifest:key-derivation manifest:key-derivation-name="PBKDF2" manifest:key-size="32" manifest:iteration-count="100000" manifest:salt="BQY=" loext:argon2-memory="65536"/>` +
		`</manifest:encryption-data></manifest:file-entry></manifest:manifest>`
	data := zipOf(t, "mimetype", MimeText, "META-INF/manifest.xml", manifest, "content.xml", minimalContent, "Pictures/a.png", "xx", "Pictures/b.png", "png")
	p, err := Open(context.Background(), data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if p.MimeType != MimeText || p.Flat || p.Content == nil || p.Styles != nil {
		t.Fatalf("package = %+v", p)
	}
	if len(p.Manifest) != 2 || p.Manifest[1].Size != 12 {
		t.Fatalf("manifest = %+v", p.Manifest)
	}
	e := p.Encryption("./Pictures/a.png")
	if e == nil || e.IterationCount != "100000" || e.KeySize != "32" || e.StartKeySize != "32" || e.InitialisationVector != "AwQ=" ||
		e.Salt != "BQY=" || e.Checksum != "AAEC" || e.Argon2Memory != "65536" || e.Size != "12" || !strings.HasSuffix(e.AlgorithmName, "aes256-cbc") ||
		!strings.HasSuffix(e.StartKeyGenerationName, "sha256") || e.KeyDerivationName != "PBKDF2" || !strings.HasSuffix(e.ChecksumType, "sha256-1k") {
		t.Fatalf("encryption = %+v", e)
	}
	if _, err := p.ReadFile(context.Background(), "Pictures/a.png"); !errors.Is(err, office.ErrEncrypted) {
		t.Errorf("encrypted entry: err = %v", err)
	}
	if b, err := p.ReadFile(context.Background(), "/Pictures/b.png"); err != nil || string(b) != "png" {
		t.Errorf("clear entry = %q, %v", b, err)
	}
	if _, err := p.ReadFile(context.Background(), "Pictures/c.png"); !IsNotExist(err) {
		t.Errorf("missing entry: err = %v", err)
	}
}

func TestOpen_Refusals(t *testing.T) {
	encManifest := `<manifest:manifest xmlns:manifest="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0">` +
		`<manifest:file-entry manifest:full-path="content.xml" manifest:media-type="text/xml"><manifest:encryption-data/></manifest:file-entry></manifest:manifest>`
	deep := strings.Repeat("<a>", MaxDepth+1) + strings.Repeat("</a>", MaxDepth+1)
	cases := []struct {
		name string
		data []byte
		want error
	}{
		{"no content", zipOf(t, "mimetype", MimeText), office.ErrMalformed},
		{"bad zip", []byte("PK\x03\x04garbage"), office.ErrMalformed},
		{"encrypted content", zipOf(t, "mimetype", MimeText, "META-INF/manifest.xml", encManifest, "content.xml", "xx"), office.ErrEncrypted},
		{"bad xml", zipOf(t, "mimetype", MimeText, "content.xml", "<a><b></a>"), office.ErrMalformed},
		{"too deep", zipOf(t, "mimetype", MimeText, "content.xml", deep), office.ErrInputTooLarge},
		{"flat not a document", []byte(`<office:document-content ` + ns + `/>`), office.ErrMalformed},
		{"empty", nil, office.ErrMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Open(context.Background(), tc.data, Options{}); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestParseXML_NoEntityExpansion(t *testing.T) {
	src := `<!DOCTYPE a [<!ENTITY x "boom">]><a>&x;</a>`
	if _, err := ParseXML(context.Background(), []byte(src)); !errors.Is(err, office.ErrMalformed) {
		t.Errorf("err = %v, want a refusal of the undefined entity", err)
	}
	el, err := ParseXML(context.Background(), []byte(`<a>x &amp; <b/>y</a>`))
	if err != nil {
		t.Fatal(err)
	}
	if got := el.TextContent(); got != "x & y" {
		t.Errorf("text = %q", got)
	}
}

func TestUnits(t *testing.T) {
	lengths := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"1in", 72, true},
		{"2.54cm", 72, true},
		{"10mm", 72 / 2.54, true},
		{"12pt", 12, true},
		{"1pc", 12, true},
		{"4px", 3, true},
		{"-0.5in", -36, true},
		{"3", 0, false},
		{"1em", 0, false},
		{"1e9in", 0, false},
	}
	for _, c := range lengths {
		got, ok := Length(c.in)
		if ok != c.ok || ok && (got-c.want > 1e-9 || c.want-got > 1e-9) {
			t.Errorf("Length(%q) = %v, %v", c.in, got, ok)
		}
	}
	if v, ok := Percent("115%"); !ok || v != 1.15 {
		t.Errorf("Percent = %v, %v", v, ok)
	}
	if c, ok := ParseColor("#0a0B0c"); !ok || c != (Color{10, 11, 12}) {
		t.Errorf("ParseColor = %v, %v", c, ok)
	}
	if _, ok := ParseColor("transparent"); ok {
		t.Error("transparent parsed as a colour")
	}
	for in, want := range map[string]string{"'Liberation Serif'": "Liberation Serif", `"A B", serif`: "A B", "Arial, sans": "Arial"} {
		if got := Unquote(in); got != want {
			t.Errorf("Unquote(%q) = %q", in, got)
		}
	}
}

func TestStyles_Resolve(t *testing.T) {
	styles := `<office:document-styles ` + ns + `>` +
		`<office:font-face-decls><style:font-face style:name="F1" svg:font-family="'Liberation Sans'" style:font-family-generic="swiss"/></office:font-face-decls>` +
		`<office:styles>` +
		`<style:default-style style:family="paragraph"><style:paragraph-properties fo:margin-left="1in" fo:margin-top="2pt"/></style:default-style>` +
		`<style:style style:name="Base" style:family="paragraph"><style:paragraph-properties fo:margin-left="2in"><style:tab-stops/></style:paragraph-properties></style:style>` +
		`<style:style style:name="Child" style:family="paragraph" style:parent-style-name="Base" style:default-outline-level="2"><style:paragraph-properties fo:margin-right="3in"/></style:style>` +
		`<style:style style:name="LoopA" style:family="paragraph" style:parent-style-name="LoopB"/>` +
		`<style:style style:name="LoopB" style:family="paragraph" style:parent-style-name="LoopA"/>` +
		`<text:list-style style:name="L1"><text:list-level-style-number text:level="2" style:num-format="a"><style:list-level-properties text:list-level-position-and-space-mode="label-alignment"><style:list-level-label-alignment text:label-followed-by="listtab" fo:margin-left="1cm"/></style:list-level-properties></text:list-level-style-number></text:list-style>` +
		`</office:styles>` +
		`<office:automatic-styles><style:style style:name="P1" style:family="paragraph" style:parent-style-name="Base"><style:paragraph-properties fo:margin-left="9in"/></style:style>` +
		`<style:page-layout style:name="pm1" style:page-usage="mirrored"><style:page-layout-properties fo:page-width="10cm"/><style:header-style><style:header-footer-properties fo:min-height="1cm"/></style:header-style></style:page-layout></office:automatic-styles>` +
		`<office:master-styles><style:master-page style:name="Standard" style:page-layout-name="pm1" style:next-style-name="Next"><style:header><text:p>H</text:p></style:header></style:master-page></office:master-styles>` +
		`</office:document-styles>`
	content := `<office:document-content ` + ns + `><office:automatic-styles><style:style style:name="P1" style:family="paragraph" style:parent-style-name="Child"/></office:automatic-styles><office:body><office:text/></office:body></office:document-content>`
	p, err := Open(context.Background(), zipOf(t, "mimetype", MimeText, "content.xml", content, "styles.xml", styles), Options{})
	if err != nil {
		t.Fatal(err)
	}
	s := NewStyles(p)
	if ff := s.Fonts["F1"]; ff.Family != "Liberation Sans" || ff.Generic != "swiss" {
		t.Errorf("font face = %+v", ff)
	}
	content1 := s.Resolve(ScopeContent, FamilyParagraph, "P1", ParagraphProps)
	if content1.Get("fo:margin-left") != "2in" || content1.Get("fo:margin-right") != "3in" || content1.Get("fo:margin-top") != "2pt" || content1.Child("style:tab-stops") == nil {
		t.Errorf("content P1 = %+v", content1)
	}
	if got := s.Resolve(ScopeStyles, FamilyParagraph, "P1", ParagraphProps).Get("fo:margin-left"); got != "9in" {
		t.Errorf("styles P1 margin-left = %q", got)
	}
	if got := s.Resolve(ScopeContent, FamilyParagraph, "Missing", ParagraphProps).Get("fo:margin-left"); got != "1in" {
		t.Errorf("missing style margin-left = %q", got)
	}
	if n := len(s.Chain(ScopeContent, FamilyParagraph, "LoopA")); n != 2 {
		t.Errorf("cyclic chain length = %d", n)
	}
	if st := s.Style(ScopeContent, FamilyParagraph, "Child"); st == nil || st.OutlineLevel != 2 {
		t.Errorf("outline level: %+v", st)
	}
	ls := s.List(ScopeContent, "L1")
	if ls == nil || ls.Levels[1] == nil || ls.Levels[1].Props.Get("text:label-followed-by") != "listtab" || ls.Levels[0] != nil {
		t.Errorf("list style = %+v", ls)
	}
	mp := s.Master("Standard")
	if mp == nil || mp.Next != "Next" || mp.Header == nil || s.PageLayouts["pm1"].Header.Get("fo:min-height") != "1cm" || s.PageLayouts["pm1"].PageUsage != "mirrored" {
		t.Errorf("master = %+v", mp)
	}
}

// TestOpen_StreamsTheContent pins the streamed parse of a package entry:
// content.xml holding a comment of random letters past
// office.MaxXMLTokenBytes is refused as too large, and one with a shorter
// comment opens.
func TestOpen_StreamsTheContent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		pad  int
		want error
	}{
		{"one token past the cap", office.MaxXMLTokenBytes + 1, office.ErrInputTooLarge},
		{"under the cap", 1 << 20, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := rand.New(rand.NewPCG(1, 2))
			noise := make([]byte, tc.pad)
			for i := range noise {
				noise[i] = 'a' + byte(r.IntN(26))
			}
			content := `<office:document-content ` + ns + `><!--` + string(noise) + `--><office:body/></office:document-content>`
			p, err := Open(context.Background(), zipOf(t, "mimetype", MimeText, "content.xml", content), Options{})
			if !errors.Is(err, tc.want) || (err == nil && !p.Content.Is(NSOffice, "document-content")) {
				t.Fatalf("Open = %v; want %v", err, tc.want)
			}
		})
	}
}
