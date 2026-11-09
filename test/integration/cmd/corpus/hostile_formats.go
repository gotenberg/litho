package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Counts of the format-specific floods: far past what a real document
// holds, short of what the input cap refuses outright.
const (
	shapeFlood    = 100_000
	pathFlood     = 500_000
	paraFlood     = 20_000
	mergeFlood    = 100_000
	condFlood     = 20_000
	sectionFlood  = 9_000
	formulaParens = 100_000
	maxInt32      = "2147483647"
	minInt32      = "-2147483648"
)

// ensurePart adds part to parts, typed ct, with a typ relationship to it
// from the part named from, unless parts already holds it.
func ensurePart(parts map[string][]byte, from, typ, part, ct string, data []byte) {
	if _, ok := parts[part]; ok {
		return
	}
	parts[part] = data
	addRel(parts, from, typ, relTarget(from, part), false)
	addOverride(parts, part, ct)
}

// firstPart returns the first part, by name, starting with prefix.
func firstPart(parts map[string][]byte, prefix string) (string, error) {
	var names []string
	for n := range parts {
		if strings.HasPrefix(n, prefix) && strings.HasSuffix(n, ".xml") {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no part starting with %s", prefix)
	}
	sort.Strings(names)
	return names[0], nil
}

// replaceAll applies re to part, refusing a part re does not match.
func replaceAll(parts map[string][]byte, part string, re *regexp.Regexp, repl string) error {
	s := string(parts[part])
	if !re.MatchString(s) {
		return fmt.Errorf("%s has no %s", part, re)
	}
	parts[part] = []byte(re.ReplaceAllString(s, repl))
	return nil
}

// repeatBlocks returns n blocks f returns, concatenated.
func repeatBlocks(n int, f func(i int) string) string {
	var b strings.Builder
	for i := range n {
		b.WriteString(f(i))
	}
	return b.String()
}

const wNS = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"`

// docxHostile returns the hostile documents only WordprocessingML has.
func docxHostile() []hostileDoc {
	const main = "word/document.xml"
	para := func(ppr, text string) string {
		return `<w:p><w:pPr>` + ppr + `</w:pPr><w:r><w:t xml:space="preserve">` + text + `</w:t></w:r></w:p>`
	}
	return []hostileDoc{
		{"style-cycle", edited(func(parts map[string][]byte) error {
			parts["word/styles.xml"] = []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:styles ` + wNS + `>` +
				`<w:style w:type="paragraph" w:styleId="A"><w:name w:val="A"/><w:basedOn w:val="B"/><w:next w:val="B"/><w:link w:val="AC"/><w:rPr><w:b/></w:rPr></w:style>` +
				`<w:style w:type="paragraph" w:styleId="B"><w:name w:val="B"/><w:basedOn w:val="A"/><w:next w:val="A"/><w:rPr><w:i/></w:rPr></w:style>` +
				`<w:style w:type="paragraph" w:styleId="S"><w:name w:val="S"/><w:basedOn w:val="S"/></w:style>` +
				`<w:style w:type="character" w:styleId="AC"><w:name w:val="AC"/><w:basedOn w:val="AC"/><w:link w:val="A"/></w:style>` +
				`<w:style w:type="table" w:styleId="T"><w:name w:val="T"/><w:basedOn w:val="T"/></w:style>` +
				`<w:style w:type="paragraph" w:default="1" w:styleId="A"><w:name w:val="A again"/><w:basedOn w:val="Missing"/></w:style>` +
				`</w:styles>`)
			ensurePart(parts, main, relStyles, "word/styles.xml", ctWStyles, parts["word/styles.xml"])
			return setBody(parts, "docx", para(`<w:pStyle w:val="A"/>`, "A based on B based on A")+
				para(`<w:pStyle w:val="S"/>`, "S based on itself")+
				`<w:p><w:r><w:rPr><w:rStyle w:val="AC"/></w:rPr><w:t>AC based on itself</w:t></w:r></w:p>`+
				`<w:tbl><w:tblPr><w:tblStyle w:val="T"/></w:tblPr><w:tblGrid><w:gridCol w:w="2000"/></w:tblGrid><w:tr><w:tc><w:p><w:r><w:t>T</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`)
		})},
		{"numbering-cycle", edited(func(parts map[string][]byte) error {
			var levels strings.Builder
			for l := range 9 {
				fmt.Fprintf(&levels, `<w:lvl w:ilvl="%d"><w:start w:val="%s"/><w:numFmt w:val="decimal"/><w:lvlRestart w:val="9"/><w:lvlText w:val="%%1.%%2.%%3.%%4.%%5.%%6.%%7.%%8.%%9.%%10."/></w:lvl>`, l, maxInt32)
			}
			parts["word/numbering.xml"] = []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:numbering ` + wNS + `>` +
				`<w:abstractNum w:abstractNumId="0"><w:numStyleLink w:val="LS"/></w:abstractNum>` +
				`<w:abstractNum w:abstractNumId="1"><w:styleLink w:val="LS"/>` + levels.String() + `</w:abstractNum>` +
				`<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>` +
				`<w:num w:numId="2"><w:abstractNumId w:val="1"/><w:lvlOverride w:ilvl="0"><w:startOverride w:val="-5"/></w:lvlOverride></w:num>` +
				`<w:num w:numId="3"><w:abstractNumId w:val="99"/></w:num>` +
				`</w:numbering>`)
			ensurePart(parts, main, relNumbering, "word/numbering.xml", ctNumbering, parts["word/numbering.xml"])
			style := `<w:style w:type="numbering" w:styleId="LS"><w:name w:val="LS"/><w:pPr><w:numPr><w:numId w:val="1"/></w:numPr></w:pPr></w:style>`
			if s, ok := parts["word/styles.xml"]; ok {
				parts["word/styles.xml"] = []byte(insertBefore(string(s), "</w:styles>", style))
			}
			var body strings.Builder
			for _, num := range []string{"1", "2", "3", "0"} {
				for _, lvl := range []string{"0", "4", "8", "9", maxInt32, "-1"} {
					body.WriteString(para(`<w:numPr><w:ilvl w:val="`+lvl+`"/><w:numId w:val="`+num+`"/></w:numPr>`, "num "+num+" level "+lvl))
				}
			}
			body.WriteString(repeatBlocks(2000, func(int) string {
				return para(`<w:numPr><w:ilvl w:val="8"/><w:numId w:val="2"/></w:numPr>`, "counter past 2^31")
			}))
			return setBody(parts, "docx", body.String())
		})},
		{"page-flood", withBodyContent("docx", func() string {
			return repeatBlocks(pageFlood, func(int) string { return `<w:p><w:r><w:br w:type="page"/></w:r></w:p>` })
		})},
		{"huge-values", withBodyContent("docx", func() string {
			return para(`<w:spacing w:before="`+maxInt32+`" w:line="`+maxInt32+`" w:lineRule="exact"/><w:ind w:left="`+minInt32+`" w:hanging="`+maxInt32+`"/>`+
				`<w:tabs><w:tab w:val="right" w:pos="`+maxInt32+`"/><w:tab w:val="left" w:pos="`+minInt32+`"/></w:tabs>`, "huge spacing and indents") +
				`<w:p><w:r><w:rPr><w:sz w:val="` + maxInt32 + `"/><w:w w:val="600"/><w:spacing w:val="` + minInt32 + `"/><w:position w:val="` + maxInt32 + `"/></w:rPr><w:t>huge run</w:t></w:r></w:p>` +
				`<w:tbl><w:tblPr><w:tblW w:w="` + maxInt32 + `" w:type="dxa"/><w:tblInd w:w="` + minInt32 + `" w:type="dxa"/></w:tblPr><w:tblGrid><w:gridCol w:w="` + maxInt32 + `"/></w:tblGrid>` +
				`<w:tr><w:trPr><w:trHeight w:val="` + maxInt32 + `" w:hRule="exact"/></w:trPr><w:tc><w:tcPr><w:gridSpan w:val="` + maxInt32 + `"/></w:tcPr><w:p/></w:tc></w:tr></w:tbl>` +
				para(`<w:sectPr><w:pgSz w:w="`+maxInt32+`" w:h="`+maxInt32+`"/><w:pgMar w:top="`+minInt32+`" w:right="`+maxInt32+`" w:bottom="0" w:left="`+maxInt32+`" w:header="0" w:footer="0" w:gutter="`+maxInt32+`"/>`+
					`<w:cols w:num="`+maxInt32+`" w:space="`+maxInt32+`"/><w:lnNumType w:countBy="1"/></w:sectPr>`, "huge section") +
				para(`<w:sectPr><w:pgSz w:w="0" w:h="0"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="0" w:footer="0" w:gutter="0"/><w:cols w:num="0"/></w:sectPr>`, "zero-size section")
		})},
		{"deep-tables", withBodyContent("docx", func() string {
			open := `<w:tbl><w:tblGrid><w:gridCol w:w="5000"/></w:tblGrid><w:tr><w:tc>`
			return strings.Repeat(open, structureNesting) + `<w:p><w:r><w:t>deep</w:t></w:r></w:p>` +
				strings.Repeat(`<w:p/></w:tc></w:tr></w:tbl>`, structureNesting) + `<w:p/>`
		})},
		{"deep-content-controls", withBodyContent("docx", func() string {
			return strings.Repeat(`<w:sdt><w:sdtPr/><w:sdtContent>`, structureNesting) + `<w:p><w:r><w:t>deep</w:t></w:r></w:p>` +
				strings.Repeat(`</w:sdtContent></w:sdt>`, structureNesting)
		})},
		{"field-nesting", withBodyContent("docx", func() string {
			begin := `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> IF 1 = 1 </w:instrText></w:r>`
			end := `<w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>x</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r>`
			return `<w:p>` + strings.Repeat(begin, structureNesting) + strings.Repeat(end, structureNesting) + `</w:p>` +
				`<w:p>` + strings.Repeat(begin, 1000) + `</w:p>` +
				`<w:p>` + strings.Repeat(`<w:r><w:fldChar w:fldCharType="end"/></w:r>`, 1000) + `</w:p>`
		})},
		{"section-flood", withBodyContent("docx", func() string {
			return repeatBlocks(sectionFlood, func(i int) string {
				return para(`<w:sectPr><w:type w:val="continuous"/><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720" w:gutter="0"/><w:cols w:num="`+fmt.Sprint(1+i%3)+`"/></w:sectPr>`, "section")
			})
		})},
	}
}

const sNS = `xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"`

var (
	reDimension   = regexp.MustCompile(`<dimension [^>]*/>`)
	rePageSetup   = regexp.MustCompile(`<pageSetup [^>]*/>|<pageMargins [^>]*/>`)
	reSheetFormat = regexp.MustCompile(`<sheetFormatPr [^>]*/>|<cols>.*?</cols>`)
	reSldSz       = regexp.MustCompile(`<p:sldSz [^>]*/>`)
)

// xlsxCell returns the row r holding one cell of attributes attrs and inner
// markup inner at ref.
func xlsxRow(r, attrs, cells string) string {
	return `<row r="` + r + `"` + attrs + `>` + cells + `</row>`
}

// afterSheetData inserts add right after the sheet's cells, where the
// merges, conditional formats and page setup follow them (ECMA-376 Part 1,
// 18.3.1.99).
func afterSheetData(parts map[string][]byte, add string) error {
	main := mainPart("xlsx")
	s := string(parts[main])
	i := strings.Index(s, "</sheetData>")
	if i < 0 {
		return fmt.Errorf("%s has no sheetData", main)
	}
	i += len("</sheetData>")
	parts[main] = []byte(s[:i] + add + s[i:])
	return nil
}

// beforeSheetData inserts add right before the sheet's cells, where the
// format and the columns precede them.
func beforeSheetData(parts map[string][]byte, add string) error {
	main := mainPart("xlsx")
	s := string(parts[main])
	i := strings.Index(s, "<sheetData")
	if i < 0 {
		return fmt.Errorf("%s has no sheetData", main)
	}
	parts[main] = []byte(s[:i] + add + s[i:])
	return nil
}

// xlsxHostile returns the hostile documents only SpreadsheetML has.
func xlsxHostile() []hostileDoc {
	const workbook = "xl/workbook.xml"
	inline := func(ref, text string) string {
		return `<c r="` + ref + `" t="inlineStr"><is><t>` + text + `</t></is></c>`
	}
	return []hostileDoc{
		{"max-dimensions", edited(func(parts map[string][]byte) error {
			_ = replaceAll(parts, mainPart("xlsx"), reDimension, `<dimension ref="A1:XFD1048576"/>`) //nolint:errcheck // a sheet with no dimension is as hostile
			return setBody(parts, "xlsx", xlsxRow("1", "", inline("A1", "first"))+xlsxRow("1048576", "", inline("XFD1048576", "last")))
		})},
		{"formatted-sheet", edited(func(parts map[string][]byte) error {
			if err := beforeSheetData(parts, `<cols><col min="1" max="16384" width="9" style="0" customWidth="1"/></cols>`); err != nil {
				return err
			}
			return setBody(parts, "xlsx", xlsxRow("1", "", inline("A1", "one cell, every column styled"))+
				xlsxRow("1048576", ` s="0" customFormat="1" ht="15" customHeight="1"`, `<c r="XFD1048576" s="0"/>`))
		})},
		{"bad-indexes", withBodyContent("xlsx", func() string {
			return xlsxRow("1", "", `<c r="A1" t="s"><v>999999</v></c><c r="B1" t="s"><v>-1</v></c><c r="C1" s="99999"><v>1</v></c><c r="D1" s="-1"><v>2</v></c>`) +
				xlsxRow("2", "", `<c r="A2" t="e"><v>#GARBAGE!</v></c><c r="B2" t="b"><v>7</v></c><c r="C2"><v>1e400</v></c><c r="D2" t="d"><v>not a date</v></c><c r="E2"><v>NaN</v></c><c r="F2" t="n"><v></v></c><c r="G2" t="bogus"><v>1</v></c>`)
		})},
		{"bad-refs", withBodyContent("xlsx", func() string {
			return xlsxRow("3", "", inline("A3", "three")) + xlsxRow("1", "", inline("A1", "one")) + xlsxRow("3", "", inline("A3", "three again")) +
				xlsxRow("0", "", inline("A0", "row zero")) + xlsxRow("1048577", "", inline("A1048577", "past the last row")) +
				xlsxRow("-1", "", inline("A-1", "negative row")) + xlsxRow("5", "", inline("XFE5", "past the last column")+inline("ZZZZZZ5", "far column")+inline("", "no ref")+inline("5A", "reversed"))
		})},
		{"merge-flood", edited(func(parts map[string][]byte) error {
			if err := setBody(parts, "xlsx", xlsxRow("1", "", inline("A1", "merged"))); err != nil {
				return err
			}
			var m strings.Builder
			fmt.Fprintf(&m, `<mergeCells count="%d"><mergeCell ref="A1:XFD1048576"/>`, mergeFlood+1)
			for i := range mergeFlood {
				fmt.Fprintf(&m, `<mergeCell ref="A%d:Z%d"/>`, 1+i%1000, 1000+i%1000)
			}
			m.WriteString(`</mergeCells>`)
			return afterSheetData(parts, m.String())
		})},
		{"conditional-flood", edited(func(parts map[string][]byte) error {
			if err := setBody(parts, "xlsx", xlsxRow("1", "", `<c r="A1"><v>1</v></c>`)); err != nil {
				return err
			}
			return afterSheetData(parts, repeatBlocks(condFlood, func(i int) string {
				return fmt.Sprintf(`<conditionalFormatting sqref="A1:XFD1048576"><cfRule type="expression" dxfId="%d" priority="%d"><formula>TRUE</formula></cfRule>`+
					`<cfRule type="dataBar" priority="%d"><dataBar><cfvo type="min"/><cfvo type="max"/><color rgb="FF638EC6"/></dataBar></cfRule></conditionalFormatting>`, i, 2*i+1, 2*i+2)
			}))
		})},
		{"number-formats", edited(func(parts map[string][]byte) error {
			codes := []string{
				strings.Repeat("0", 10_000), strings.Repeat("[", 5000), strings.Repeat(";", 5000), `"unterminated`,
				"[$-F800]dddd, mmmm dd, yyyy", strings.Repeat(",", 5000) + "0", "0.0E+" + strings.Repeat("0", 1000),
				"?/" + strings.Repeat("?", 1000), "[h]:mm:ss." + strings.Repeat("0", 1000), "[Red][Blue][Green]0", `\`, "*", "_",
				"[>1E308]0;[<-1E308]0", "[DBNum9][$-x-sysdate]General", "@@@@@@@@" + strings.Repeat("@", 1000),
			}
			var fmts, xfs, cells strings.Builder
			fmt.Fprintf(&fmts, `<numFmts count="%d">`, len(codes))
			fmt.Fprintf(&xfs, `<cellXfs count="%d"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>`, len(codes)+1)
			for i, c := range codes {
				fmt.Fprintf(&fmts, `<numFmt numFmtId="%d" formatCode="%s"/>`, 164+i, xmlEscape(c))
				fmt.Fprintf(&xfs, `<xf numFmtId="%d" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>`, 164+i)
				fmt.Fprintf(&cells, `<row r="%d"><c r="A%d" s="%d"><v>1234.5678</v></c><c r="B%d" s="%d"><v>-1E308</v></c><c r="C%d" s="%d" t="inlineStr"><is><t>text</t></is></c></row>`,
					i+1, i+1, i+1, i+1, i+1, i+1, i+1)
			}
			fmts.WriteString(`</numFmts>`)
			xfs.WriteString(`</cellXfs>`)
			styles := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><styleSheet ` + sNS + `>` + fmts.String() +
				`<fonts count="1"><font><sz val="11"/><name val="Calibri"/></font></fonts>` +
				`<fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>` +
				`<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>` +
				`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` + xfs.String() +
				`<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`
			name := "xl/styles.xml"
			if _, ok := parts[name]; !ok {
				ensurePart(parts, workbook, relStyles, name, ctXStyles, nil)
			}
			parts[name] = []byte(styles)
			return setBody(parts, "xlsx", cells.String())
		})},
		{"huge-sizes", edited(func(parts map[string][]byte) error {
			main := mainPart("xlsx")
			parts[main] = []byte(reSheetFormat.ReplaceAllString(string(parts[main]), ""))
			parts[main] = []byte(rePageSetup.ReplaceAllString(string(parts[main]), ""))
			if err := beforeSheetData(parts, `<sheetFormatPr defaultColWidth="1E308" defaultRowHeight="-1" baseColWidth="255"/>`+
				`<cols><col min="1" max="1" width="1E308" customWidth="1"/><col min="2" max="2" width="NaN" customWidth="1"/><col min="3" max="3" width="-5" customWidth="1"/><col min="5" max="4" width="9"/></cols>`); err != nil {
				return err
			}
			if err := setBody(parts, "xlsx", xlsxRow("1", ` ht="1E308" customHeight="1"`, inline("A1", "huge")+inline("B1", "NaN")+inline("C1", "negative"))+
				xlsxRow("2", ` ht="-5" customHeight="1"`, inline("A2", "negative height"))); err != nil {
				return err
			}
			return afterSheetData(parts, `<pageMargins left="1E9" right="1E9" top="-1E9" bottom="NaN" header="0" footer="0"/>`+
				`<pageSetup paperSize="9999" scale="0" fitToWidth="`+maxInt32+`" fitToHeight="`+maxInt32+`" firstPageNumber="`+minInt32+`" useFirstPageNumber="1" orientation="portrait"/>`)
		})},
		{"sheet-flood", edited(func(parts map[string][]byte) error {
			targets := make([]string, pageFlood)
			for i := range targets {
				targets[i] = relTarget(workbook, mainPart("xlsx"))
			}
			var sheets strings.Builder
			for i, id := range addRelsBulk(parts, workbook, relWorksheet, targets) {
				fmt.Fprintf(&sheets, `<sheet name="H%d" sheetId="%d" r:id="%s" %s/>`, i, 100_000+i, id, nsR)
			}
			s := string(parts[workbook])
			if !strings.Contains(s, "</sheets>") {
				return fmt.Errorf("%s has no sheets", workbook)
			}
			parts[workbook] = []byte(insertBefore(s, "</sheets>", sheets.String()))
			return nil
		})},
		{"defined-name-cycle", edited(func(parts map[string][]byte) error {
			names := `<definedName name="a">b</definedName><definedName name="b">a</definedName>` +
				`<definedName name="_xlnm.Print_Area" localSheetId="0">a</definedName>` +
				`<definedName name="_xlnm.Print_Titles" localSheetId="0">Sheet1!$1:$1048576</definedName>`
			s := string(parts[workbook])
			if strings.Contains(s, "</definedNames>") {
				s = insertBefore(s, "</definedNames>", names)
			} else if i := strings.Index(s, "</sheets>"); i >= 0 {
				s = s[:i+len("</sheets>")] + "<definedNames>" + names + "</definedNames>" + s[i+len("</sheets>"):]
			} else {
				return fmt.Errorf("%s has no sheets", workbook)
			}
			parts[workbook] = []byte(s)
			return setBody(parts, "xlsx", xlsxRow("1", "", `<c r="A1"><f>a</f></c>`))
		})},
		{"formula-huge", withBodyContent("xlsx", func() string {
			return xlsxRow("1", "", `<c r="A1"><f>`+strings.Repeat("(", formulaParens)+"1"+strings.Repeat(")", formulaParens)+`</f></c>`+
				`<c r="B1"><f>`+strings.Repeat("A1+", 300_000)+`1</f><v>1</v></c><c r="C1"><f>C1</f><v>0</v></c>`)
		})},
	}
}

// pptxHostile returns the hostile documents only PresentationML has.
func pptxHostile() []hostileDoc {
	const (
		main = "ppt/slides/slide1.xml"
		pres = "ppt/presentation.xml"
	)
	rect := func(id int, xfrm, body string) string {
		return fmt.Sprintf(`<p:sp><p:nvSpPr><p:cNvPr id="%d" name="s%d"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm %s>%s</a:xfrm>%s</p:spPr></p:sp>`,
			2000+id, id, nsA, xfrm, body)
	}
	box := `<a:off x="0" y="0"/><a:ext cx="100000" cy="100000"/>`
	solid := `<a:prstGeom prst="rect" ` + nsA + `><a:avLst/></a:prstGeom><a:solidFill ` + nsA + `><a:srgbClr val="4472C4"/></a:solidFill>`
	return []hostileDoc{
		{"layout-cycle", edited(func(parts map[string][]byte) error {
			layout, err := firstPart(parts, "ppt/slideLayouts/slideLayout")
			if err != nil {
				return err
			}
			master, err := firstPart(parts, "ppt/slideMasters/slideMaster")
			if err != nil {
				return err
			}
			if err := setRelTargets(parts, layout, "/slideMaster", relTarget(layout, layout)); err != nil {
				return err
			}
			return setRelTargets(parts, master, "/slideLayout", relTarget(master, master))
		})},
		{"deep-groups", withBodyContent("pptx", func() string {
			var b strings.Builder
			for i := range structureNesting {
				fmt.Fprintf(&b, `<p:grpSp><p:nvGrpSpPr><p:cNvPr id="%d" name="g%d"/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>`+
					`<p:grpSpPr><a:xfrm %s><a:off x="0" y="0"/><a:ext cx="100" cy="100"/><a:chOff x="0" y="0"/><a:chExt cx="50" cy="50"/></a:xfrm></p:grpSpPr>`, 10_000+i, i, nsA)
			}
			b.WriteString(shapeText(1, 0, 0, 100, 100, "deep"))
			b.WriteString(strings.Repeat(`</p:grpSp>`, structureNesting))
			return b.String()
		})},
		{"huge-slide-size", edited(func(parts map[string][]byte) error {
			if err := replaceAll(parts, pres, reSldSz, `<p:sldSz cx="`+maxInt32+`" cy="1"/>`); err != nil {
				return err
			}
			return setBody(parts, "pptx", rect(1, `<a:off x="-9223372036854775808" y="0"/><a:ext cx="9223372036854775807" cy="9223372036854775807"/>`, solid)+
				rect(2, `<a:off x="0" y="0"/><a:ext cx="-914400" cy="-914400"/>`, solid)+
				rect(3, `<a:off x="0" y="0"/><a:ext cx="914400" cy="914400"/>`, solid)+
				strings.Replace(rect(4, box, solid), `<a:xfrm `, `<a:xfrm rot="`+maxInt32+`" `, 1))
		})},
		{"slide-flood", edited(func(parts map[string][]byte) error {
			s := string(parts[pres])
			if !strings.Contains(s, "</p:sldIdLst>") {
				return fmt.Errorf("%s has no sldIdLst", pres)
			}
			targets := make([]string, pageFlood)
			for i := range targets {
				targets[i] = relTarget(pres, main)
			}
			var ids strings.Builder
			for i, id := range addRelsBulk(parts, pres, relSlide, targets) {
				fmt.Fprintf(&ids, `<p:sldId id="%d" r:id="%s" %s/>`, 100_000+i, id, nsR)
			}
			parts[pres] = []byte(insertBefore(s, "</p:sldIdLst>", ids.String()))
			return nil
		})},
		{"shape-flood", withBodyContent("pptx", func() string {
			return repeatBlocks(shapeFlood, func(i int) string {
				return rect(i, fmt.Sprintf(`<a:off x="%d" y="%d"/><a:ext cx="50000" cy="50000"/>`, (i%300)*30000, (i/300)%200*30000), solid)
			})
		})},
		{"path-flood", withBodyContent("pptx", func() string {
			var p strings.Builder
			p.WriteString(`<a:custGeom ` + nsA + `><a:avLst/><a:gdLst/><a:ahLst/><a:cxnLst/><a:rect l="0" t="0" r="r" b="b"/><a:pathLst><a:path w="1000" h="1000"><a:moveTo><a:pt x="0" y="0"/></a:moveTo>`)
			for i := range pathFlood {
				if i%2 == 0 {
					fmt.Fprintf(&p, `<a:lnTo><a:pt x="%d" y="%d"/></a:lnTo>`, i%1000, (i*7)%1000)
				} else {
					fmt.Fprintf(&p, `<a:cubicBezTo><a:pt x="%d" y="0"/><a:pt x="0" y="%d"/><a:pt x="%d" y="%d"/></a:cubicBezTo>`, i%1000, i%997, i%991, i%983)
				}
			}
			p.WriteString(`<a:close/></a:path></a:pathLst></a:custGeom><a:solidFill ` + nsA + `><a:srgbClr val="ED7D31"/></a:solidFill>`)
			return rect(1, `<a:off x="0" y="0"/><a:ext cx="5000000" cy="5000000"/>`, p.String())
		})},
		{"autofit-flood", withBodyContent("pptx", func() string {
			return `<p:sp><p:nvSpPr><p:cNvPr id="3000" name="flood"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr>` +
				`<p:spPr><a:xfrm ` + nsA + `><a:off x="0" y="0"/><a:ext cx="2000000" cy="1000000"/></a:xfrm><a:prstGeom prst="rect" ` + nsA + `><a:avLst/></a:prstGeom></p:spPr>` +
				`<p:txBody><a:bodyPr ` + nsA + `><a:normAutofit fontScale="1000" lnSpcReduction="90000"/></a:bodyPr><a:lstStyle ` + nsA + `/>` +
				repeatBlocks(paraFlood, func(i int) string {
					return fmt.Sprintf(`<a:p %s><a:r><a:rPr lang="en-US" sz="4000"/><a:t>paragraph %d</a:t></a:r></a:p>`, nsA, i)
				}) + `</p:txBody></p:sp>`
		})},
		{"table-huge", withBodyContent("pptx", func() string {
			const n = 200
			var t strings.Builder
			t.WriteString(`<p:graphicFrame><p:nvGraphicFramePr><p:cNvPr id="4000" name="table"/><p:cNvGraphicFramePr/><p:nvPr/></p:nvGraphicFramePr>` +
				`<p:xfrm><a:off ` + nsA + ` x="0" y="0"/><a:ext ` + nsA + ` cx="8000000" cy="5000000"/></p:xfrm>` +
				`<a:graphic ` + nsA + `><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/table"><a:tbl><a:tblGrid>`)
			t.WriteString(strings.Repeat(`<a:gridCol w="40000"/>`, n))
			t.WriteString(`</a:tblGrid>`)
			for r := range n {
				t.WriteString(`<a:tr h="25000">`)
				for c := range n {
					switch {
					case r == 0 && c == 0:
						t.WriteString(`<a:tc gridSpan="` + maxInt32 + `" rowSpan="` + maxInt32 + `"><a:txBody><a:bodyPr/><a:lstStyle/><a:p><a:r><a:t>span</a:t></a:r></a:p></a:txBody><a:tcPr/></a:tc>`)
					default:
						t.WriteString(`<a:tc><a:txBody><a:bodyPr/><a:lstStyle/><a:p><a:r><a:t>x</a:t></a:r></a:p></a:txBody><a:tcPr/></a:tc>`)
					}
				}
				t.WriteString(`</a:tr>`)
			}
			t.WriteString(`</a:tbl></a:graphicData></a:graphic></p:graphicFrame>`)
			return t.String()
		})},
	}
}
