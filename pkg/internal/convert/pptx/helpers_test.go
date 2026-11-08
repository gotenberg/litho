package pptx

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// nsDecl declares the namespaces the test parts use.
const nsDecl = `xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" ` +
	`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" ` +
	`xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" ` +
	`xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006"`

// relsNS is the package relationships namespace.
const relsNS = `xmlns="http://schemas.openxmlformats.org/package/2006/relationships"`

// The relationship types of the test parts.
const (
	tRelSlide   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide"
	tRelLayout  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout"
	tRelMaster  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster"
	tRelTheme   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme"
	tRelNotes   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/notesSlide"
	tRelNotesM  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/notesMaster"
	tRelImage   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/image"
	tRelTblSty  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/tableStyles"
	tRelLink    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink"
	tRelChart   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/chart"
	tRelCoreDoc = "http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties"
)

// testTheme is a theme of Office 2007's colours, "Test Sans" for both
// fonts and a three-entry style matrix.
const testTheme = `<a:theme ` + nsDecl + ` name="T"><a:themeElements><a:clrScheme name="T">` +
	`<a:dk1><a:sysClr val="windowText" lastClr="000000"/></a:dk1><a:lt1><a:sysClr val="window" lastClr="FFFFFF"/></a:lt1>` +
	`<a:dk2><a:srgbClr val="1F497D"/></a:dk2><a:lt2><a:srgbClr val="EEECE1"/></a:lt2>` +
	`<a:accent1><a:srgbClr val="4F81BD"/></a:accent1><a:accent2><a:srgbClr val="C0504D"/></a:accent2><a:accent3><a:srgbClr val="9BBB59"/></a:accent3>` +
	`<a:accent4><a:srgbClr val="8064A2"/></a:accent4><a:accent5><a:srgbClr val="4BACC6"/></a:accent5><a:accent6><a:srgbClr val="F79646"/></a:accent6>` +
	`<a:hlink><a:srgbClr val="0000FF"/></a:hlink><a:folHlink><a:srgbClr val="800080"/></a:folHlink></a:clrScheme>` +
	`<a:fontScheme name="T"><a:majorFont><a:latin typeface="Test Sans"/><a:ea typeface=""/><a:cs typeface=""/></a:majorFont>` +
	`<a:minorFont><a:latin typeface="Test Sans"/><a:ea typeface=""/><a:cs typeface=""/></a:minorFont></a:fontScheme>` +
	`<a:fmtScheme name="T"><a:fillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill>` +
	`<a:gradFill><a:gsLst><a:gs pos="0"><a:schemeClr val="phClr"><a:tint val="50000"/></a:schemeClr></a:gs><a:gs pos="100000"><a:schemeClr val="phClr"/></a:gs></a:gsLst><a:lin ang="5400000"/></a:gradFill>` +
	`<a:solidFill><a:schemeClr val="phClr"><a:shade val="50000"/></a:schemeClr></a:solidFill></a:fillStyleLst>` +
	`<a:lnStyleLst><a:ln w="9525"><a:solidFill><a:schemeClr val="phClr"><a:shade val="95000"/></a:schemeClr></a:solidFill></a:ln>` +
	`<a:ln w="25400"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln><a:ln w="38100"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln></a:lnStyleLst>` +
	`<a:effectStyleLst><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle></a:effectStyleLst>` +
	`<a:bgFillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"><a:tint val="50000"/></a:schemeClr></a:solidFill>` +
	`<a:solidFill><a:schemeClr val="phClr"><a:shade val="50000"/></a:schemeClr></a:solidFill></a:bgFillStyleLst></a:fmtScheme></a:themeElements></a:theme>`

// spGroup opens every shape tree.
const spGroup = `<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/>`

// phShape returns a placeholder shape of type typ ("" for none) and idx
// (-1 for none), placed at x, y, w, h EMU when w is positive, with the
// text body body ("" for none).
func phShape(name, typ string, idx int, x, y, w, h int, body string) string {
	ph := `<p:ph`
	if typ != "" {
		ph += ` type="` + typ + `"`
	}
	if idx >= 0 {
		ph += fmt.Sprintf(` idx="%d"`, idx)
	}
	ph += `/>`
	sp := `<p:spPr/>`
	if w > 0 {
		sp = fmt.Sprintf(`<p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm></p:spPr>`, x, y, w, h)
	}
	out := `<p:sp><p:nvSpPr><p:cNvPr id="9" name="` + name + `"/><p:cNvSpPr/><p:nvPr>` + ph + `</p:nvPr></p:nvSpPr>` + sp
	if body != "" {
		out += `<p:txBody>` + body + `</p:txBody>`
	}
	return out + `</p:sp>`
}

// para returns a text body of one paragraph per text.
func para(texts ...string) string {
	out := `<a:bodyPr/><a:lstStyle/>`
	for _, t := range texts {
		out += `<a:p><a:r><a:rPr lang="en-US"/><a:t>` + t + `</a:t></a:r></a:p>`
	}
	return out
}

// defaultMasterTree is a master's shapes: title, body, date, footer and
// slide number placeholders, as PowerPoint's 4:3 default master places
// them.
var defaultMasterTree = phShape("Title Placeholder 1", "title", -1, 457200, 274638, 8229600, 1143000, `<a:bodyPr anchor="ctr"><a:normAutofit/></a:bodyPr><a:lstStyle/><a:p><a:endParaRPr/></a:p>`) +
	phShape("Text Placeholder 2", "body", 1, 457200, 1600200, 8229600, 4525963, `<a:bodyPr><a:normAutofit/></a:bodyPr><a:lstStyle/><a:p><a:endParaRPr/></a:p>`) +
	phShape("Date Placeholder 3", "dt", 2, 457200, 6356350, 2133600, 365125, `<a:bodyPr anchor="ctr"/><a:lstStyle><a:lvl1pPr><a:defRPr sz="1200"/></a:lvl1pPr></a:lstStyle><a:p><a:fld id="{1}" type="datetime1"><a:t>1/1/2026</a:t></a:fld></a:p>`) +
	phShape("Footer Placeholder 4", "ftr", 3, 3124200, 6356350, 2895600, 365125, `<a:bodyPr anchor="ctr"/><a:lstStyle><a:lvl1pPr algn="ctr"><a:defRPr sz="1200"/></a:lvl1pPr></a:lstStyle><a:p><a:r><a:t>MASTER-FOOTER</a:t></a:r></a:p>`) +
	phShape("Slide Number Placeholder 5", "sldNum", 4, 6553200, 6356350, 2133600, 365125, `<a:bodyPr anchor="ctr"/><a:lstStyle><a:lvl1pPr algn="r"><a:defRPr sz="1200"/></a:lvl1pPr></a:lstStyle><a:p><a:fld id="{2}" type="slidenum"><a:t>‹#›</a:t></a:fld></a:p>`)

// defaultTxStyles are the master's text styles: a centred 44-point title
// in the major font without bullets, a 32 and 28-point body with bullets,
// an 18-point other style.
const defaultTxStyles = `<p:txStyles><p:titleStyle><a:lvl1pPr algn="ctr"><a:buNone/><a:defRPr sz="4400"><a:solidFill><a:schemeClr val="tx1"/></a:solidFill><a:latin typeface="+mj-lt"/></a:defRPr></a:lvl1pPr></p:titleStyle>` +
	`<p:bodyStyle><a:lvl1pPr marL="342900" indent="-342900"><a:spcBef><a:spcPct val="20000"/></a:spcBef><a:buFont typeface="Arial"/><a:buChar char="•"/><a:defRPr sz="3200"><a:solidFill><a:schemeClr val="tx1"/></a:solidFill><a:latin typeface="+mn-lt"/></a:defRPr></a:lvl1pPr>` +
	`<a:lvl2pPr marL="742950" indent="-285750"><a:buFont typeface="Arial"/><a:buChar char="–"/><a:defRPr sz="2800"/></a:lvl2pPr></p:bodyStyle>` +
	`<p:otherStyle><a:lvl1pPr><a:defRPr sz="1800"><a:solidFill><a:schemeClr val="tx1"/></a:solidFill><a:latin typeface="+mn-lt"/></a:defRPr></a:lvl1pPr></p:otherStyle></p:txStyles>`

// defaultLayoutTree is a "Title and Content" layout's shapes.
var defaultLayoutTree = phShape("Title 1", "title", -1, 0, 0, 0, 0, para("Click")) + phShape("Content Placeholder 2", "", 1, 0, 0, 0, 0, para("Click")) +
	phShape("Footer Placeholder 4", "ftr", 11, 0, 0, 0, 0, `<a:bodyPr/><a:lstStyle/><a:p><a:r><a:t>LAYOUT-FOOTER</a:t></a:r></a:p>`)

// testSlide is one slide of a test deck: its shape tree content, the
// attributes of p:sld, what follows its p:cSld, its relationships and the
// shape tree content of its notes ("" for none).
type testSlide struct {
	tree, attrs, cSldAttrs, bg, after, rels, notes string
}

// testDeck is a test presentation; the zero value of a field takes the
// default master, layout and theme.
type testDeck struct {
	slides []testSlide
	// masterTree, masterBg, masterExtra and masterAttrs replace the
	// master's shapes, background, text styles and p:sldMaster attributes.
	masterTree, masterBg, masterExtra, masterAttrs string
	// layoutTree, layoutBg and layoutAttrs replace the layout's.
	layoutTree, layoutBg, layoutAttrs string
	// sldSz and presExtra set the slide size element and add children to
	// p:presentation after it.
	sldSz, presExtra string
	// parts are extra parts: ppt/tableStyles.xml is related from the
	// presentation, any other part only stored.
	parts map[string]string
}

// deckFiles returns the parts of a test deck.
func deckFiles(d testDeck) map[string]string {
	if d.masterTree == "" {
		d.masterTree = defaultMasterTree
	}
	if d.masterExtra == "" {
		d.masterExtra = defaultTxStyles
	}
	if d.layoutTree == "" {
		d.layoutTree = defaultLayoutTree
	}
	if d.sldSz == "" {
		d.sldSz = `<p:sldSz cx="9144000" cy="6858000"/>`
	}
	files := map[string]string{
		"_rels/.rels": `<Relationships ` + relsNS + `><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="ppt/presentation.xml"/>` +
			`<Relationship Id="rId2" Type="` + tRelCoreDoc + `" Target="docProps/core.xml"/></Relationships>`,
		"docProps/core.xml":    `<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Test deck</dc:title><dc:creator>litho</dc:creator></cp:coreProperties>`,
		"ppt/theme/theme1.xml": testTheme,
		"ppt/slideMasters/slideMaster1.xml": `<p:sldMaster ` + nsDecl + ` ` + d.masterAttrs + `><p:cSld>` + d.masterBg + `<p:spTree>` + spGroup + d.masterTree + `</p:spTree></p:cSld>` +
			`<p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/>` +
			`<p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:sldLayoutIdLst>` + d.masterExtra + `</p:sldMaster>`,
		"ppt/slideMasters/_rels/slideMaster1.xml.rels": `<Relationships ` + relsNS + `><Relationship Id="rId1" Type="` + tRelLayout + `" Target="../slideLayouts/slideLayout1.xml"/>` +
			`<Relationship Id="rId2" Type="` + tRelTheme + `" Target="../theme/theme1.xml"/></Relationships>`,
		"ppt/slideLayouts/slideLayout1.xml": `<p:sldLayout ` + nsDecl + ` ` + d.layoutAttrs + `><p:cSld name="Title and Content">` + d.layoutBg + `<p:spTree>` + spGroup + d.layoutTree + `</p:spTree></p:cSld>` +
			`<p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>`,
		"ppt/slideLayouts/_rels/slideLayout1.xml.rels": `<Relationships ` + relsNS + `><Relationship Id="rId1" Type="` + tRelMaster + `" Target="../slideMasters/slideMaster1.xml"/></Relationships>`,
		"ppt/notesMasters/notesMaster1.xml": `<p:notesMaster ` + nsDecl + `><p:cSld><p:spTree>` + spGroup +
			phShape("Notes Placeholder", "body", 3, 685800, 4400550, 5486400, 3600450, `<a:bodyPr/><a:lstStyle/><a:p><a:endParaRPr/></a:p>`) + `</p:spTree></p:cSld>` +
			`<p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/>` +
			`<p:notesStyle><a:lvl1pPr><a:defRPr sz="1200"><a:solidFill><a:schemeClr val="tx1"/></a:solidFill><a:latin typeface="+mn-lt"/></a:defRPr></a:lvl1pPr></p:notesStyle></p:notesMaster>`,
		"ppt/notesMasters/_rels/notesMaster1.xml.rels": `<Relationships ` + relsNS + `><Relationship Id="rId1" Type="` + tRelTheme + `" Target="../theme/theme1.xml"/></Relationships>`,
	}
	var pres, prels strings.Builder
	pres.WriteString(`<p:presentation ` + nsDecl + `><p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst><p:notesMasterIdLst><p:notesMasterId r:id="rId3"/></p:notesMasterIdLst><p:sldIdLst>`)
	prels.WriteString(`<Relationships ` + relsNS + `><Relationship Id="rId1" Type="` + tRelMaster + `" Target="slideMasters/slideMaster1.xml"/>` +
		`<Relationship Id="rId2" Type="` + tRelTheme + `" Target="theme/theme1.xml"/><Relationship Id="rId3" Type="` + tRelNotesM + `" Target="notesMasters/notesMaster1.xml"/>`)
	for i, s := range d.slides {
		n := i + 1
		fmt.Fprintf(&pres, `<p:sldId id="%d" r:id="rIdS%d"/>`, 255+n, n)
		fmt.Fprintf(&prels, `<Relationship Id="rIdS%d" Type="%s" Target="slides/slide%d.xml"/>`, n, tRelSlide, n)
		files[fmt.Sprintf("ppt/slides/slide%d.xml", n)] = `<p:sld ` + nsDecl + ` ` + s.attrs + `><p:cSld ` + s.cSldAttrs + `>` + s.bg + `<p:spTree>` + spGroup + s.tree + `</p:spTree></p:cSld>` + s.after + `</p:sld>`
		rels := `<Relationship Id="rIdL" Type="` + tRelLayout + `" Target="../slideLayouts/slideLayout1.xml"/>` + s.rels
		if s.notes != "" {
			rels += fmt.Sprintf(`<Relationship Id="rIdN" Type="%s" Target="../notesSlides/notesSlide%d.xml"/>`, tRelNotes, n)
			files[fmt.Sprintf("ppt/notesSlides/notesSlide%d.xml", n)] = `<p:notes ` + nsDecl + `><p:cSld><p:spTree>` + spGroup + s.notes + `</p:spTree></p:cSld></p:notes>`
			files[fmt.Sprintf("ppt/notesSlides/_rels/notesSlide%d.xml.rels", n)] = `<Relationships ` + relsNS + `><Relationship Id="rId1" Type="` + tRelNotesM + `" Target="../notesMasters/notesMaster1.xml"/></Relationships>`
		}
		files[fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", n)] = `<Relationships ` + relsNS + `>` + rels + `</Relationships>`
	}
	pres.WriteString(`</p:sldIdLst>` + d.sldSz + `<p:notesSz cx="6858000" cy="9144000"/>` + d.presExtra + `</p:presentation>`)
	names := make([]string, 0, len(d.parts))
	for n := range d.parts {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		files[n] = d.parts[n]
		if n == "ppt/tableStyles.xml" {
			prels.WriteString(`<Relationship Id="rIdT" Type="` + tRelTblSty + `" Target="tableStyles.xml"/>`)
		}
	}
	prels.WriteString(`</Relationships>`)
	files["ppt/presentation.xml"] = pres.String()
	files["ppt/_rels/presentation.xml.rels"] = prels.String()
	files["[Content_Types].xml"] = contentTypes(files)
	return files
}

// partTypes maps a part name's folder to its content type, which
// LibreOffice needs to recognise a presentation.
var partTypes = map[string]string{
	"ppt/slides/":        "application/vnd.openxmlformats-officedocument.presentationml.slide+xml",
	"ppt/slideLayouts/":  "application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml",
	"ppt/slideMasters/":  "application/vnd.openxmlformats-officedocument.presentationml.slideMaster+xml",
	"ppt/notesMasters/":  "application/vnd.openxmlformats-officedocument.presentationml.notesMaster+xml",
	"ppt/notesSlides/":   "application/vnd.openxmlformats-officedocument.presentationml.notesSlide+xml",
	"ppt/theme/":         "application/vnd.openxmlformats-officedocument.theme+xml",
	"ppt/charts/":        "application/vnd.openxmlformats-officedocument.drawingml.chart+xml",
	"ppt/tableStyles":    "application/vnd.openxmlformats-officedocument.presentationml.tableStyles+xml",
	"ppt/presentation":   "application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml",
	"docProps/core":      "application/vnd.openxmlformats-package.core-properties+xml",
	"ppt/diagrams/drawi": "application/vnd.ms-office.drawingml.diagramDrawing+xml",
	"ppt/diagrams/data":  "application/vnd.openxmlformats-officedocument.drawingml.diagramData+xml",
}

// contentTypes returns the [Content_Types].xml of files.
func contentTypes(files map[string]string) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="xml" ContentType="application/xml"/><Default Extension="png" ContentType="image/png"/>` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if strings.Contains(n, "_rels/") || !strings.HasSuffix(n, ".xml") {
			continue
		}
		for prefix, ct := range partTypes {
			if strings.HasPrefix(n, prefix) {
				fmt.Fprintf(&sb, `<Override PartName="/%s" ContentType="%s"/>`, n, ct)
				break
			}
		}
	}
	return sb.String() + `</Types>`
}

// zipFiles returns a zip archive of files, in name order, stored without
// compression so the fuzz target spends its time in the reader, each
// entry's sizes and checksum in its header, as LibreOffice needs for a
// stored entry.
func zipFiles(t testing.TB, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		data := []byte(files[n])
		w, err := zw.CreateRaw(&zip.FileHeader{
			Name: n, Method: zip.Store, CRC32: crc32.ChecksumIEEE(data),
			CompressedSize64: uint64(len(data)), UncompressedSize64: uint64(len(data)),
			Modified: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// readDeck reads a test deck into the model with the test fonts.
func readDeck(t testing.TB, d testDeck) (*slides.Document, []compose.Warning) {
	t.Helper()
	doc, warnings, err := load(context.Background(), bytes.NewReader(zipFiles(t, deckFiles(d))), testFonts(t), office.Options{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return doc, warnings
}

// oneSlide reads a deck of one slide of shape tree content tree.
func oneSlide(t testing.TB, tree string) *slides.Slide {
	t.Helper()
	doc, _ := readDeck(t, testDeck{slides: []testSlide{{tree: tree}}})
	if len(doc.Slides) != 1 {
		t.Fatalf("%d slides, want 1", len(doc.Slides))
	}
	return doc.Slides[0]
}

// shapeNamed returns the first shape of s named name, searching groups,
// nil for none.
func shapeNamed(shapes []*slides.Shape, name string) *slides.Shape {
	for _, sh := range shapes {
		if sh.Name == name {
			return sh
		}
		if k := shapeNamed(sh.Kids, name); k != nil {
			return k
		}
	}
	return nil
}

// testSans builds a "Test Sans" face covering printable ASCII, every glyph
// half an em wide.
func testSans() []byte {
	glyphs := []fontbuild.Glyph{{Name: "space", Unicode: ' ', Advance: 500, Empty: true}}
	for r := rune('!'); r <= '~'; r++ {
		glyphs = append(glyphs, fontbuild.Glyph{Name: fmt.Sprintf("uni%04X", r), Unicode: r, Advance: 500})
	}
	return fontbuild.TrueType{
		PostScriptName: "TestSans-Regular",
		Family:         "Test Sans",
		Subfamily:      "Regular",
		NotdefAdvance:  500,
		Glyphs:         glyphs,
	}.Build()
}

var (
	sharedFontsOnce sync.Once
	sharedFonts     *text.Fonts
	sharedFontsErr  error
)

// testFonts returns an index of the test face, built once for the
// package.
func testFonts(t testing.TB) *text.Fonts {
	t.Helper()
	sharedFontsOnce.Do(func() {
		dir, err := os.MkdirTemp("", "litho-pptx-fonts")
		if err != nil {
			sharedFontsErr = err
			return
		}
		if err := os.WriteFile(filepath.Join(dir, "test_sans.ttf"), testSans(), 0o600); err != nil {
			sharedFontsErr = err
			return
		}
		sharedFonts, sharedFontsErr = text.NewFonts(context.Background(), nil, []string{dir})
	})
	if sharedFontsErr != nil {
		t.Fatalf("indexing the test font: %v", sharedFontsErr)
	}
	return sharedFonts
}
