package pptx

import "strings"

// The built-in table styles litho draws, by ID (ECMA-376 Part 1,
// 20.1.4.2.26: the GUIDs of the table styles PowerPoint ships, defined
// in the presetTableStyles.xml of the specification's Part 1 annex): the
// two styles without colour and the Medium Style 2 family, plain and
// for each accent. The scheme colour each Medium Style 2 variant is drawn
// in is the value.
var builtinMedium2 = map[string]string{
	"{073A0DAA-6AF3-43AB-8588-CEC1D06C72B9}": "dk1",
	"{5C22544A-7EE6-4342-B048-85BDC9FD1C3A}": "accent1",
	"{21E4AEA4-8DFA-4A89-87EB-49C32662AFE8}": "accent2",
	"{F5AB1C69-6EDB-4FF4-983F-18BD219EF322}": "accent3",
	"{00A15C55-8517-42AA-B614-E9B94910E393}": "accent4",
	"{7DF18680-E054-41AD-8BC1-D1AEF772440D}": "accent5",
	"{93296810-A885-4BE3-A3E7-6D5BEEA58F35}": "accent6",
}

// The IDs of the styles without colour: "No Style, No Grid" and "No
// Style, Table Grid".
const (
	idNoStyleNoGrid    = "{2D5ABB26-0587-4C30-8999-92F81FD0307C}"
	idNoStyleTableGrid = "{5940675A-B579-460E-94D1-54222C63F5DA}"
)

// tableStyleNS declares the DrawingML namespace on a built-in style.
const tableStyleNS = `xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"`

// builtinTableStyleXML returns the a:tblStyle of a built-in style ID, false
// for an ID it does not know.
//
// Medium Style 2 is drawn as Impress draws it, which departs from the
// specification's definition: every border, the header's and the total
// row's included, is a 1-point line of light 1, and the header, total
// row and first and last columns print in light 1 without bold, on the
// colour; the table is the colour tinted to 20%, the odd bands to 40%.
//
// # Corpus evidence
//
//   - litho: Gotenberg's deck.pptx, a "Medium Style 2 - Accent 1" table:
//     Impress fills the header 4F81BD, the first band D0D8E7, the second
//     E9ECF3, draws 0.84-point white rules between all rows and columns,
//     and sets the header's "R0C0" in regular Carlito, as wide as the
//     body's "R1C0".
func builtinTableStyleXML(id string) (string, bool) {
	id = strings.ToUpper(id)
	text := `<a:tcTxStyle><a:fontRef idx="minor"><a:prstClr val="black"/></a:fontRef><a:schemeClr val="dk1"/></a:tcTxStyle>`
	switch id {
	case idNoStyleNoGrid:
		return `<a:tblStyle ` + tableStyleNS + ` styleId="` + id + `"><a:wholeTbl>` + text + `<a:tcStyle><a:tcBdr/><a:fill><a:noFill/></a:fill></a:tcStyle></a:wholeTbl></a:tblStyle>`, true
	case idNoStyleTableGrid:
		ln := `<a:ln w="12700" cmpd="sng"><a:solidFill><a:schemeClr val="tx1"/></a:solidFill></a:ln>`
		var bdr strings.Builder
		for _, s := range []string{"left", "right", "top", "bottom", "insideH", "insideV"} {
			bdr.WriteString("<a:" + s + ">" + ln + "</a:" + s + ">")
		}
		return `<a:tblStyle ` + tableStyleNS + ` styleId="` + id + `"><a:wholeTbl>` + text + `<a:tcStyle><a:tcBdr>` + bdr.String() + `</a:tcBdr><a:fill><a:noFill/></a:fill></a:tcStyle></a:wholeTbl></a:tblStyle>`, true
	}
	clr, ok := builtinMedium2[id]
	if !ok {
		return "", false
	}
	fill := func(tint string) string {
		if tint == "" {
			return `<a:fill><a:solidFill><a:schemeClr val="` + clr + `"/></a:solidFill></a:fill>`
		}
		return `<a:fill><a:solidFill><a:schemeClr val="` + clr + `"><a:tint val="` + tint + `"/></a:schemeClr></a:solidFill></a:fill>`
	}
	ln := `<a:ln w="12700" cmpd="sng"><a:solidFill><a:schemeClr val="lt1"/></a:solidFill></a:ln>`
	var bdr strings.Builder
	for _, s := range []string{"left", "right", "top", "bottom", "insideH", "insideV"} {
		bdr.WriteString("<a:" + s + ">" + ln + "</a:" + s + ">")
	}
	light := `<a:tcTxStyle><a:fontRef idx="minor"><a:prstClr val="black"/></a:fontRef><a:schemeClr val="lt1"/></a:tcTxStyle>`
	var sb strings.Builder
	sb.WriteString(`<a:tblStyle ` + tableStyleNS + ` styleId="` + id + `">`)
	sb.WriteString(`<a:wholeTbl>` + text + `<a:tcStyle><a:tcBdr>` + bdr.String() + `</a:tcBdr>` + fill("20000") + `</a:tcStyle></a:wholeTbl>`)
	sb.WriteString(`<a:band1H><a:tcStyle><a:tcBdr/>` + fill("40000") + `</a:tcStyle></a:band1H><a:band2H><a:tcStyle><a:tcBdr/></a:tcStyle></a:band2H>`)
	sb.WriteString(`<a:band1V><a:tcStyle><a:tcBdr/>` + fill("40000") + `</a:tcStyle></a:band1V><a:band2V><a:tcStyle><a:tcBdr/></a:tcStyle></a:band2V>`)
	for _, part := range []string{"lastCol", "firstCol", "lastRow", "firstRow"} {
		sb.WriteString(`<a:` + part + `>` + light + `<a:tcStyle><a:tcBdr/>` + fill("") + `</a:tcStyle></a:` + part + `>`)
	}
	sb.WriteString(`</a:tblStyle>`)
	return sb.String(), true
}
