package main

import (
	"fmt"
	"path"
	"strings"
)

// Relationship types and content types the hostile documents add (ECMA-376
// Part 1, 15 and Part 2, 9).
const (
	relImage     = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/image"
	relHyperlink = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink"
	relNumbering = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering"
	relStyles    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles"
	relDrawing   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/drawing"
	relWorksheet = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet"
	relSlide     = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide"

	ctDrawing   = "application/vnd.openxmlformats-officedocument.drawing+xml"
	ctNumbering = "application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"
	ctWStyles   = "application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"
	ctXStyles   = "application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"

	nsA   = `xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"`
	nsR   = `xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`
	nsPic = `xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"`
	nsWP  = `xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"`

	emptyRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`
)

// mainPart returns the part a format's hostile content goes into: the
// document body, the first sheet, the first slide.
func mainPart(format string) string {
	switch format {
	case "docx":
		return "word/document.xml"
	case "xlsx":
		return "xl/worksheets/sheet1.xml"
	default:
		return "ppt/slides/slide1.xml"
	}
}

// mediaDir returns the directory a format keeps its media in.
func mediaDir(format string) string {
	switch format {
	case "docx":
		return "word/media"
	case "xlsx":
		return "xl/media"
	default:
		return "ppt/media"
	}
}

// relsOf returns the relationships part of a part (ECMA-376 Part 2, 9.3.3).
func relsOf(part string) string {
	return path.Join(path.Dir(part), "_rels", path.Base(part)+".rels")
}

// relTarget returns the target of part relative to the part from.
func relTarget(from, part string) string {
	dir := path.Dir(from)
	if strings.HasPrefix(part, dir+"/") {
		return strings.TrimPrefix(part, dir+"/")
	}
	return "../" + strings.TrimPrefix(part, path.Dir(dir)+"/")
}

// addRel adds a relationship to the relationships part of from and returns
// its ID, one no other relationship there uses.
func addRel(parts map[string][]byte, from, typ, target string, external bool) string {
	name := relsOf(from)
	rels, ok := parts[name]
	if !ok {
		rels = []byte(emptyRels)
	}
	s := string(rels)
	var id string
	for i := 1; ; i++ {
		id = fmt.Sprintf("rIdH%d", i)
		if !strings.Contains(s, `"`+id+`"`) {
			break
		}
	}
	mode := ""
	if external {
		mode = ` TargetMode="External"`
	}
	rel := fmt.Sprintf(`<Relationship Id="%s" Type="%s" Target="%s"%s/>`, id, typ, xmlEscape(target), mode)
	parts[name] = []byte(insertBefore(s, "</Relationships>", rel))
	return id
}

// addOverride names the content type of a part in [Content_Types].xml.
func addOverride(parts map[string][]byte, part, contentType string) {
	s := string(parts[contentTypesPart])
	if strings.Contains(s, `PartName="/`+part+`"`) {
		return
	}
	parts[contentTypesPart] = []byte(insertBefore(s, "</Types>", fmt.Sprintf(`<Override PartName="/%s" ContentType="%s"/>`, part, contentType)))
}

// addDefault names the content type of an extension in [Content_Types].xml.
func addDefault(parts map[string][]byte, ext, contentType string) {
	s := string(parts[contentTypesPart])
	if strings.Contains(strings.ToLower(s), `extension="`+ext+`"`) {
		return
	}
	parts[contentTypesPart] = []byte(insertBefore(s, "</Types>", fmt.Sprintf(`<Default Extension="%s" ContentType="%s"/>`, ext, contentType)))
}

// insertBefore inserts add before the last occurrence of mark, or appends
// it when s holds none.
func insertBefore(s, mark, add string) string {
	i := strings.LastIndex(s, mark)
	if i < 0 {
		return s + add
	}
	return s[:i] + add + s[i:]
}

// xmlEscape escapes the five XML special characters.
func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;").Replace(s)
}

// withBody returns the main part of format with content added to its body:
// before the document's last section properties, at the end of the slide's
// shape tree, or as the sheet's rows, replacing the ones it has.
func withBody(format string, xml, content string) (string, error) {
	switch format {
	case "docx":
		if i := strings.LastIndex(xml, "<w:sectPr"); i >= 0 && i > strings.LastIndex(xml, "</w:p>") {
			return xml[:i] + content + xml[i:], nil
		}
		if !strings.Contains(xml, "</w:body>") {
			return "", fmt.Errorf("the %s main part has no w:body", format)
		}
		return insertBefore(xml, "</w:body>", content), nil
	case "xlsx":
		start := strings.Index(xml, "<sheetData")
		if start < 0 {
			return "", fmt.Errorf("the %s main part has no sheetData", format)
		}
		if end := strings.Index(xml, "</sheetData>"); end >= 0 {
			return xml[:start] + "<sheetData>" + content + xml[end:], nil
		}
		end := strings.Index(xml[start:], "/>")
		return xml[:start] + "<sheetData>" + content + "</sheetData>" + xml[start+end+2:], nil
	default:
		if !strings.Contains(xml, "</p:spTree>") {
			return "", fmt.Errorf("the %s main part has no p:spTree", format)
		}
		return insertBefore(xml, "</p:spTree>", content), nil
	}
}

// setBody adds content to the body of the main part of parts.
func setBody(parts map[string][]byte, format, content string) error {
	main := mainPart(format)
	xml, err := withBody(format, string(parts[main]), content)
	if err != nil {
		return err
	}
	parts[main] = []byte(xml)
	return nil
}

// textBlock returns one block of body content holding text: a paragraph, a
// text box, or the sheet's first row.
func textBlock(format, text string) string {
	switch format {
	case "docx":
		return `<w:p><w:r><w:t xml:space="preserve">` + text + `</w:t></w:r></w:p>`
	case "xlsx":
		return `<row r="1"><c r="A1" t="inlineStr"><is><t xml:space="preserve">` + text + `</t></is></c></row>`
	default:
		return shapeText(1, 0, 0, 8000000, 1000000, text)
	}
}

// shapeText returns a slide text box with one paragraph of text.
func shapeText(id int, x, y, cx, cy int64, text string) string {
	return fmt.Sprintf(`<p:sp><p:nvSpPr><p:cNvPr id="%d" name="t%d"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr>`+
		`<p:spPr><a:xfrm %s><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr>`+
		`<p:txBody><a:bodyPr %s/><a:lstStyle %s/><a:p %s><a:r><a:rPr lang="en-US"/><a:t>%s</a:t></a:r></a:p></p:txBody></p:sp>`,
		1000+id, id, nsA, x, y, cx, cy, nsA, nsA, nsA, text)
}

// picture is one image a hostile document shows: a part it adds (data set),
// a part another entry provides (data nil), or an external target.
type picture struct {
	name     string // the part, or the external URL
	data     []byte
	external bool
}

// addPictures shows pics in the main part of parts, each an inch square,
// adding the media, relationships and, for a sheet, the drawing they need.
func addPictures(parts map[string][]byte, format string, pics []picture) error {
	from := mainPart(format)
	if format == "xlsx" {
		from = "xl/drawings/drawingH1.xml"
	}
	var blocks strings.Builder
	for i, p := range pics {
		target := p.name
		if !p.external {
			if p.data != nil {
				parts[p.name] = p.data
			}
			ext := strings.TrimPrefix(path.Ext(p.name), ".")
			if ext != "" {
				addDefault(parts, ext, imageType(ext))
			}
			target = relTarget(from, p.name)
		}
		id := addRel(parts, from, relImage, target, p.external)
		attr := "r:embed"
		if p.external {
			attr = "r:link"
		}
		blocks.WriteString(pictureXML(format, i+1, attr, id))
	}
	if format != "xlsx" {
		return setBody(parts, format, blocks.String())
	}
	parts[from] = []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<xdr:wsDr xmlns:xdr="http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing" ` + nsA + ` ` + nsR + `>` +
		blocks.String() + `</xdr:wsDr>`)
	addOverride(parts, from, ctDrawing)
	sheet := mainPart(format)
	id := addRel(parts, sheet, relDrawing, relTarget(sheet, from), false)
	xml := string(parts[sheet])
	mark := "</worksheet>"
	for _, m := range []string{"<legacyDrawing", "<tableParts", "<extLst"} {
		if i := strings.Index(xml, m); i >= 0 {
			mark = m
			break
		}
	}
	i := strings.Index(xml, mark)
	if i < 0 {
		return fmt.Errorf("the sheet has no %s", mark)
	}
	parts[sheet] = []byte(xml[:i] + `<drawing r:id="` + id + `" ` + nsR + `/>` + xml[i:])
	return nil
}

// pictureXML returns the markup of picture n of a format, its image the
// relationship id names through attr (r:embed or r:link).
func pictureXML(format string, n int, attr, id string) string {
	blip := fmt.Sprintf(`<a:blip %s="%s" %s/>`, attr, id, nsR)
	geom := `<a:prstGeom prst="rect"><a:avLst/></a:prstGeom>`
	switch format {
	case "docx":
		return fmt.Sprintf(`<w:p><w:r><w:drawing><wp:inline %s><wp:extent cx="914400" cy="914400"/><wp:docPr id="%d" name="h%d"/>`+
			`<a:graphic %s><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic %s>`+
			`<pic:nvPicPr><pic:cNvPr id="%d" name="h%d"/><pic:cNvPicPr/></pic:nvPicPr><pic:blipFill>%s<a:stretch><a:fillRect/></a:stretch></pic:blipFill>`+
			`<pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="914400" cy="914400"/></a:xfrm>%s</pic:spPr></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>`,
			nsWP, 1000+n, n, nsA, nsPic, 1000+n, n, blip, geom)
	case "xlsx":
		return fmt.Sprintf(`<xdr:oneCellAnchor><xdr:from><xdr:col>%d</xdr:col><xdr:colOff>0</xdr:colOff><xdr:row>%d</xdr:row><xdr:rowOff>0</xdr:rowOff></xdr:from>`+
			`<xdr:ext cx="914400" cy="914400"/><xdr:pic><xdr:nvPicPr><xdr:cNvPr id="%d" name="h%d"/><xdr:cNvPicPr/></xdr:nvPicPr>`+
			`<xdr:blipFill>%s<a:stretch><a:fillRect/></a:stretch></xdr:blipFill><xdr:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="914400" cy="914400"/></a:xfrm>%s</xdr:spPr></xdr:pic><xdr:clientData/></xdr:oneCellAnchor>`,
			(n-1)%8, 2+(n-1)/8*6, 1000+n, n, blip, geom)
	default:
		return fmt.Sprintf(`<p:pic><p:nvPicPr><p:cNvPr id="%d" name="h%d"/><p:cNvPicPr/><p:nvPr/></p:nvPicPr>`+
			`<p:blipFill>%s<a:stretch><a:fillRect/></a:stretch></p:blipFill><p:spPr><a:xfrm %s><a:off x="%d" y="%d"/><a:ext cx="914400" cy="914400"/></a:xfrm>%s</p:spPr></p:pic>`,
			1000+n, n, blip, nsA, int64((n-1)%8)*1000000, int64((n-1)/8)*1000000, geom)
	}
}

// setRelTargets rewrites, in the relationships part of from, the target of
// every relationship whose type ends with typeSuffix.
func setRelTargets(parts map[string][]byte, from, typeSuffix, target string) error {
	name := relsOf(from)
	s, ok := parts[name]
	if !ok {
		return fmt.Errorf("no %s", name)
	}
	var out strings.Builder
	rest := string(s)
	found := false
	for {
		i := strings.Index(rest, "<Relationship ")
		if i < 0 {
			out.WriteString(rest)
			break
		}
		j := strings.Index(rest[i:], ">")
		rel := rest[i : i+j+1]
		if strings.Contains(rel, typeSuffix+`"`) {
			if t := strings.Index(rel, `Target="`); t >= 0 {
				e := strings.Index(rel[t+8:], `"`)
				rel = rel[:t+8] + target + rel[t+8+e:]
				found = true
			}
		}
		out.WriteString(rest[:i] + rel)
		rest = rest[i+j+1:]
	}
	if !found {
		return fmt.Errorf("%s has no %s relationship", name, typeSuffix)
	}
	parts[name] = []byte(out.String())
	return nil
}

// imageType returns the media type of an image extension.
func imageType(ext string) string {
	if ext == "jpg" {
		return "image/jpeg"
	}
	return "image/" + ext
}

// addRelsBulk adds one relationship of type typ, from the part named from,
// to each of targets, at once, and returns their IDs: a flood of thousands of parts
// would make [addRel] quadratic.
func addRelsBulk(parts map[string][]byte, from, typ string, targets []string) []string {
	name := relsOf(from)
	rels, ok := parts[name]
	if !ok {
		rels = []byte(emptyRels)
	}
	ids := make([]string, len(targets))
	var b strings.Builder
	for i, t := range targets {
		ids[i] = fmt.Sprintf("rIdF%d", i)
		fmt.Fprintf(&b, `<Relationship Id="%s" Type="%s" Target="%s"/>`, ids[i], typ, xmlEscape(t))
	}
	parts[name] = []byte(insertBefore(string(rels), "</Relationships>", b.String()))
	return ids
}
