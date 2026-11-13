package main

import (
	"archive/zip"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"
)

// cfbStreamDef is one directory entry of a compound file a test builds: a
// storage (data nil) or a stream of at least 4096 bytes, so it lives in the
// regular sectors.
type cfbStreamDef struct {
	name string
	data []byte
}

// buildCFB writes a compound file of 512-byte sectors: the FAT in sector 0,
// the directory in sector 1, the streams after, each a chain of its own.
func buildCFB(t *testing.T, entries ...cfbStreamDef) string {
	t.Helper()
	const sector, end, fatSect, free = 512, 0xFFFFFFFE, 0xFFFFFFFD, 0xFFFFFFFF
	fat := []uint32{fatSect, end}
	dir := make([]byte, sector*((len(entries)+1+3)/4))
	if len(dir) > sector {
		t.Fatal("the test builder keeps the directory to one sector")
	}
	writeEntry := func(i int, name string, kind byte, start, size uint32) {
		e := dir[i*128 : (i+1)*128]
		u := utf16.Encode([]rune(name))
		for j, c := range u {
			binary.LittleEndian.PutUint16(e[2*j:], c)
		}
		binary.LittleEndian.PutUint16(e[0x40:], uint16(2*len(u)+2)) //nolint:gosec // short test names
		e[0x42] = kind
		binary.LittleEndian.PutUint32(e[0x74:], start)
		binary.LittleEndian.PutUint32(e[0x78:], size)
	}
	writeEntry(0, "Root Entry", cfbRoot, end, 0)
	var body []byte
	next := uint32(2)
	for i, e := range entries {
		if e.data == nil {
			writeEntry(i+1, e.name, cfbStorage, end, 0)
			continue
		}
		n := uint32((len(e.data) + sector - 1) / sector) //nolint:gosec // small test streams
		writeEntry(i+1, e.name, cfbStream, next, uint32(len(e.data)))
		for k := range n {
			if k == n-1 {
				fat = append(fat, end)
			} else {
				fat = append(fat, next+k+1)
			}
		}
		padded := make([]byte, int(n)*sector)
		copy(padded, e.data)
		body = append(body, padded...)
		next += n
	}
	if len(fat) > sector/4 {
		t.Fatal("the test builder keeps the FAT to one sector")
	}
	fatSector := make([]byte, sector)
	for i := range sector / 4 {
		v := uint32(free)
		if i < len(fat) {
			v = fat[i]
		}
		binary.LittleEndian.PutUint32(fatSector[4*i:], v)
	}
	h := make([]byte, 512)
	copy(h, cfbMagic)
	binary.LittleEndian.PutUint16(h[0x1A:], 3)
	binary.LittleEndian.PutUint16(h[0x1C:], 0xFFFE)
	binary.LittleEndian.PutUint16(h[0x1E:], 9)
	binary.LittleEndian.PutUint16(h[0x20:], 6)
	binary.LittleEndian.PutUint32(h[0x2C:], 1)
	binary.LittleEndian.PutUint32(h[0x30:], 1)
	binary.LittleEndian.PutUint32(h[0x38:], 4096)
	binary.LittleEndian.PutUint32(h[0x3C:], end)
	binary.LittleEndian.PutUint32(h[0x44:], end)
	for i := range 109 {
		v := uint32(free)
		if i == 0 {
			v = 0
		}
		binary.LittleEndian.PutUint32(h[0x4C+4*i:], v)
	}
	data := append(append(append(h, fatSector...), dir...), body...)
	p := filepath.Join(t.TempDir(), "doc.bin")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func wordStream(nFib, flags uint16) []byte {
	b := make([]byte, 4096)
	binary.LittleEndian.PutUint16(b, 0xA5EC)
	binary.LittleEndian.PutUint16(b[2:], nFib)
	binary.LittleEndian.PutUint16(b[10:], flags)
	return b
}

func workbookStream(next uint16) []byte {
	b := make([]byte, 4096)
	binary.LittleEndian.PutUint16(b, 0x0809)
	binary.LittleEndian.PutUint16(b[2:], 16)
	binary.LittleEndian.PutUint16(b[4:], 0x0600)
	binary.LittleEndian.PutUint16(b[20:], next)
	return b
}

func TestBinaryQualifies(t *testing.T) {
	t.Parallel()

	stream := make([]byte, 4096)
	cases := []struct {
		name    string
		check   func(string) bool
		entries []cfbStreamDef
		want    bool
	}{
		{"Word 97", docQualifies, []cfbStreamDef{{"WordDocument", wordStream(0x00C1, 0)}}, true},
		{"Word 95", docQualifies, []cfbStreamDef{{"WordDocument", wordStream(0x0065, 0)}}, false},
		{"encrypted Word", docQualifies, []cfbStreamDef{{"WordDocument", wordStream(0x00C1, 0x0100)}}, false},
		{"Word with macros", docQualifies, []cfbStreamDef{{"WordDocument", wordStream(0x00C1, 0)}, {"Macros", nil}}, false},
		{"Word with objects", docQualifies, []cfbStreamDef{{"WordDocument", wordStream(0x00C1, 0)}, {"ObjectPool", nil}}, false},
		{"BIFF8", xlsQualifies, []cfbStreamDef{{"Workbook", workbookStream(0x00E1)}}, true},
		{"encrypted BIFF8", xlsQualifies, []cfbStreamDef{{"Workbook", workbookStream(0x002F)}}, false},
		{"Excel with an object", xlsQualifies, []cfbStreamDef{{"Workbook", workbookStream(0x00E1)}, {"MBD0001", nil}}, false},
		{"BIFF5", xlsQualifies, []cfbStreamDef{{"Book", workbookStream(0x00E1)}}, false},
		{"PowerPoint 97", pptQualifies, []cfbStreamDef{{"PowerPoint Document", stream}, {"Current User", stream}}, true},
		{"encrypted PowerPoint", pptQualifies, []cfbStreamDef{{"PowerPoint Document", stream}, {"Current User", stream}, {"EncryptedSummary", stream}}, false},
	}
	for _, c := range cases {
		if got := c.check(buildCFB(t, c.entries...)); got != c.want {
			t.Errorf("%s: qualifies %v, want %v", c.name, got, c.want)
		}
	}
	notCFB := filepath.Join(t.TempDir(), "x.doc")
	if err := os.WriteFile(notCFB, []byte("PK not a compound file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if docQualifies(notCFB) || xlsQualifies(notCFB) || pptQualifies(notCFB) {
		t.Error("a file that is not a compound file qualified")
	}
}

func writeZip(t *testing.T, files map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "doc.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPackageQualifies(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		check func(string) bool
		files map[string]string
		want  bool
	}{
		{"docx", docxQualifies, map[string]string{"word/document.xml": ""}, true},
		{"docx with macros", docxQualifies, map[string]string{"word/document.xml": "", "word/vbaProject.bin": ""}, false},
		{"docx with an EMF", docxQualifies, map[string]string{"word/document.xml": "", "word/media/a.EMF": ""}, false},
		{"docx with a chart", docxQualifies, map[string]string{"word/document.xml": "", "word/charts/chart1.xml": ""}, false},
		{"xlsx", xlsxQualifies, map[string]string{"xl/workbook.xml": ""}, true},
		{"xlsx with ActiveX", xlsxQualifies, map[string]string{"xl/workbook.xml": "", "xl/activeX/a.xml": ""}, false},
		{"pptx with a chart's workbook", pptxQualifies, map[string]string{
			"ppt/presentation.xml": "", "ppt/embeddings/a.xlsx": "",
			"ppt/charts/_rels/chart1.xml.rels": `Target="../embeddings/a.xlsx"`,
		}, true},
		{"pptx with an object", pptxQualifies, map[string]string{
			"ppt/presentation.xml": "", "ppt/embeddings/a.bin": "",
			"ppt/slides/_rels/slide1.xml.rels": `Target="../embeddings/a.bin"`,
		}, false},
		{"pptx with a video", pptxQualifies, map[string]string{"ppt/presentation.xml": "", "ppt/media/a.mp4": ""}, false},
		{"odt", odtQualifies, map[string]string{"content.xml": "<office:text>"}, true},
		{"odt with an object", odtQualifies, map[string]string{"content.xml": "<office:text>", "Object 1/content.xml": ""}, false},
		{"ods with a chart", odsQualifies, map[string]string{
			"content.xml": "<office:spreadsheet>", "Object 1/content.xml": "",
			"META-INF/manifest.xml": `<manifest:file-entry manifest:full-path="Object 1/" manifest:media-type="application/vnd.oasis.opendocument.chart"/>`,
		}, true},
		{"ods with a formula object", odsQualifies, map[string]string{
			"content.xml": "<office:spreadsheet>", "Object 1/content.xml": "",
			"META-INF/manifest.xml": `<manifest:file-entry manifest:media-type="application/vnd.oasis.opendocument.formula" manifest:full-path="Object 1/"/>`,
		}, false},
		{"odp with macros", odpQualifies, map[string]string{"content.xml": "<office:presentation>", "Basic/a.xml": ""}, false},
	}
	for _, c := range cases {
		if got := c.check(writeZip(t, c.files)); got != c.want {
			t.Errorf("%s: qualifies %v, want %v", c.name, got, c.want)
		}
	}
}

func TestPlainIDAndWalkOrder(t *testing.T) {
	t.Parallel()

	if got := plainID("v2FxTestFiles/O12 templates/Calendar (2007).dotx"); got != "v2FxTestFiles/O12_templates/Calendar_2007_.dotx" {
		t.Errorf("plainID: %s", got)
	}
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"b.txt": "", "a/z.txt": "", "c.txt": "", "a/b/y.txt": ""})
	var got []string
	if err := walkFiles(root, func(p string) error {
		rel, _ := filepath.Rel(root, p)
		got = append(got, filepath.ToSlash(rel))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"b.txt", "c.txt", "a/z.txt", "a/b/y.txt"}; !slices.Equal(got, want) {
		t.Errorf("walk order %v, want a directory's files before its subdirectories %v", got, want)
	}
}

func TestWPTRules(t *testing.T) {
	t.Parallel()

	for text, want := range map[string]bool{
		"writing-mode: horizontal-tb":   false,
		"writing-mode:horizontal-tb":    false,
		"WRITING-MODE : Horizontal-TB":  false,
		"writing-mode: vertical-rl":     true,
		"writing-mode:  sideways-lr":    true,
		"no writing mode declared here": false,
	} {
		if got := wptOtherWritingMode(text); got != want {
			t.Errorf("%q: other writing mode %v, want %v", text, got, want)
		}
	}
	urls := cssURLs(`a { background: url( 'x.png' ) } b { background: url("y.png") } c { background: url(z.png ) } @import "d.css"; @import 'e.css';`)
	if want := []string{"x.png", "y.png", "z.png ", "d.css", "e.css"}; !slices.Equal(urls, want) {
		t.Errorf("css urls %q, want %q", urls, want)
	}
	if fz, ok := parseFuzzy([]string{"a-ref.html:maxDifference=0-2;totalPixels=0-40", "maxDifference=1;totalPixels=5"}, "dir/a-ref.html"); !ok || !slices.Equal(fz, []int{0, 2, 0, 40}) {
		t.Errorf("fuzzy for a named reference: %v, %v", fz, ok)
	}
	if fz, ok := parseFuzzy([]string{"maxDifference=1;totalPixels=5"}, "b-ref.html"); !ok || !slices.Equal(fz, []int{1, 1, 5, 5}) {
		t.Errorf("fuzzy for any reference: %v, %v", fz, ok)
	}
	if _, ok := parseFuzzy([]string{"nonsense"}, "b-ref.html"); ok {
		t.Error("an unparsed fuzziness was read")
	}
	s := scanWPT(`<html class="reftest-print"><link rel="match" href="a-ref.html"><link rel=stylesheet href=s.css>` +
		`<meta name="flags" content="ahem dom"><style>p { background: url(i.png) }</style><p style="color: red" background="b.png">`)
	if !slices.Equal(s.match, []string{"a-ref.html"}) || !s.printClass || !s.flags["dom"] ||
		!slices.Equal(s.urls, []string{"s.css", "b.png"}) || len(s.styles) != 2 {
		t.Errorf("scan %+v", s)
	}
	if xhtParsesAsHTML(`<div/>`) || !xhtParsesAsHTML(`<br/><img src="a"/>`) {
		t.Error("the XHTML self-closing rule")
	}
	if !resvgRejected([]byte(`<use href="other.svg#a"/>`), nil) || resvgRejected([]byte(`<use href="#a"/>`), nil) {
		t.Error("the resvg href rule")
	}
}

// TestWPTCandidateReasons pins the reason a test is kept out under, which
// `corpus update` tallies, and the rules a new area lifts.
func TestWPTCandidateReasons(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write := func(rel, text string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	const match = `<link rel="match" href="a-ref.html">`
	write("css/x/a-ref.html", `<p>reference</p>`)
	write("css/x/chained-ref.html", `<link rel="match" href="a-ref.html">`)
	write("css/x/fonts/f.woff", "font")
	write("css/x/plain.html", `<p>no reference</p>`)
	write("css/x/script.html", match+`<script>x()</script>`)
	write("css/x/chained.html", `<link rel="match" href="chained-ref.html">`)
	write("css/x/up.html", match+`<img src="../y.png">`)
	write("css/x/face.html", match+`<style>@font-face { font-family: F; src: url(fonts/f.woff) }</style>`)
	write("css/x/face-gone.html", match+`<style>@font-face { font-family: F; src: url(fonts/g.woff) }</style>`)
	write("css/x/flat.html", match+`<div style="transform: rotate(10deg)">`)
	write("css/x/deep.html", match+`<div style="transform: rotateY(10deg)">`)
	write("css/x/tall.html", match+`<div style="writing-mode: vertical-rl">`)
	transforms := wptAreas["css/css-transforms"]
	for _, c := range []struct {
		rel   string
		rules wptAreaRules
		want  string
	}{
		{"css/x/plain.html", wptAreaRules{}, wptNotReftest},
		{"css/x/script.html", wptFontFaceRules, `<script\b`},
		{"css/x/chained.html", wptAreaRules{}, "chained reference"},
		{"css/x/up.html", wptAreaRules{}, "resource outside the test's directory"},
		{"css/x/face.html", wptAreaRules{}, "@font-face"},
		{"css/x/face.html", wptFontFaceRules, ""},
		{"css/x/face-gone.html", wptFontFaceRules, "missing resource"},
		{"css/x/flat.html", transforms, ""},
		{"css/x/deep.html", transforms, wptTransform3D.String()},
		{"css/x/deep.html", wptProbes[1].rules, ""},
		{"css/x/flat.html", wptProbes[1].rules, "lacks " + wptTransform3D.String()[len("(?i)"):]},
		{"css/x/tall.html", wptAreas["css/css-writing-modes"], "vertical-(rl|lr)"},
		{"css/x/tall.html", wptProbes[0].rules, ""},
	} {
		e, why, err := wptCandidate(root, c.rel, c.rules)
		if err != nil {
			t.Fatalf("%s: %v", c.rel, err)
		}
		if why != strings.TrimPrefix(c.want, "(?i)") {
			t.Errorf("%s: reason %q, want %q", c.rel, why, strings.TrimPrefix(c.want, "(?i)"))
		}
		if why == "" && (e.ID != c.rel || len(e.Refs) != 1) {
			t.Errorf("%s: entry %+v", c.rel, e)
		}
	}
	if got := wptTopReasons(map[string]int{"b": 2, "a": 2, "c": 5, "d": 1}, 3); !slices.Equal(got, []string{"c", "a", "b"}) {
		t.Errorf("top reasons %v", got)
	}
}

// TestWPTProbesLeaveTheirArea pins that a probe and the area it singles a
// part out of hold no test in common: the area's rules forbid what the
// probe requires.
func TestWPTProbesLeaveTheirArea(t *testing.T) {
	t.Parallel()

	for _, p := range wptProbes {
		if _, ok := wptAreaCap[p.dir]; !ok || !slices.Contains(wptDirs, p.dir) {
			t.Errorf("probe %s: %s is not a walked area", p.name, p.dir)
		}
		if p.rules.required == nil {
			t.Fatalf("probe %s requires nothing", p.name)
		}
	}
	for text, probe := range map[string]int{
		"writing-mode: vertical-rl": 0, "writing-mode:sideways-lr": 0,
		"transform: translateZ(1px)": 1, "perspective: 10px": 1, "transform: rotateX(1deg)": 1,
	} {
		p := wptProbes[probe]
		if !p.rules.required.MatchString(text) {
			t.Errorf("probe %s does not require %q", p.name, text)
		}
		if wptForbids(text, wptAreas[p.dir]) == "" {
			t.Errorf("area %s admits %q, which probe %s requires", p.dir, text, p.name)
		}
	}
	for _, text := range []string{"transform: rotate(1deg)", "transform: translate(1px, 2px) scale(2)", "rotate: 45deg"} {
		if wptTransform3D.MatchString(text) {
			t.Errorf("%q read as a 3D transform", text)
		}
	}
}
