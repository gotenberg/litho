package xlsx

import (
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// definedName is one of the print names of a workbook (ECMA-376 Part 1,
// 18.2.5 definedName, the built-in names of 18.2.6).
type definedName struct {
	name    string
	local   int
	formula string
}

// workbook reads the workbook part root of main: its properties, its
// defined names and its sheets in book order (ECMA-376 Part 1, 18.2.27).
func (r *reader) workbook(main string, root *ooxml.Element, doc *sheet.Document) error {
	r.doc = doc
	if pr := root.Child(nsMain, "workbookPr"); pr != nil {
		r.date1904 = boolAttr(pr, "date1904")
	}
	var names []definedName
	r.pivotCaches = r.pivotCacheParts(main, root)
	for _, d := range root.Child(nsMain, "definedNames").Kids() {
		if !d.Is(nsMain, "definedName") {
			continue
		}
		n := attr(d, "name")
		if len(r.allNames) < maxAreas {
			r.allNames = append(r.allNames, definedName{name: n, local: intAttr(d, "localSheetId", -1), formula: d.Text})
		}
		if !strings.EqualFold(n, "_xlnm.Print_Area") && !strings.EqualFold(n, "_xlnm.Print_Titles") {
			continue
		}
		names = append(names, definedName{name: strings.ToLower(n), local: intAttr(d, "localSheetId", -1), formula: d.Text})
	}
	for _, s := range root.Child(nsMain, "sheets").Kids() {
		if !s.Is(nsMain, "sheet") {
			continue
		}
		if len(doc.Sheets) >= maxSheets {
			return fmt.Errorf("%w: more than %d sheets", office.ErrInputTooLarge, maxSheets)
		}
		index := len(doc.Sheets)
		sh := &sheet.Sheet{Name: attr(s, "name")}
		switch attr(s, "state") {
		case "hidden", "veryHidden":
			sh.Hidden = true
		}
		id, _ := s.AttrNS(ooxml.NSR, "id")
		rel, ok := r.pkg.RelationshipByID(main, id)
		switch {
		case !ok || rel.External || !r.pkg.Has(rel.Target):
			r.warn("malformed-part", fmt.Sprintf("sheet %q has no part: it prints empty", sh.Name))
			r.defaultSetup(sh)
		case rel.Type == relWorksheet:
			if err := r.worksheet(rel.Target, sh); err != nil {
				return err
			}
		case rel.Type == relChartsheet:
			if err := r.chartsheet(rel.Target, sh); err != nil {
				return err
			}
		default:
			r.warn("unsupported-content", fmt.Sprintf("sheet %q is not a worksheet: it prints empty", sh.Name))
			r.defaultSetup(sh)
		}
		r.applyNames(names, index, sh)
		doc.Sheets = append(doc.Sheets, sh)
		if err := r.ctx.Err(); err != nil {
			return err
		}
	}
	if len(doc.Sheets) == 0 {
		return fmt.Errorf("%w: the workbook lists no sheet", office.ErrMalformed)
	}
	return nil
}

// applyNames sets the print areas and the print titles the defined names
// give sheet index sh (ECMA-376 Part 1, 18.2.6: _xlnm.Print_Area and
// _xlnm.Print_Titles, local to the sheet localSheetId names). A title
// area spanning every column repeats its rows; one spanning every row
// its columns. An area naming another sheet than its own is skipped.
func (r *reader) applyNames(names []definedName, index int, sh *sheet.Sheet) {
	for _, n := range names {
		sheets, areas := sheetAreas(n.formula)
		for i, a := range areas {
			owner := n.local
			if sheets[i] != "" {
				if n.local >= 0 && !strings.EqualFold(sheets[i], sh.Name) && n.local == index {
					continue
				}
				if n.local < 0 {
					owner = -1
					if strings.EqualFold(sheets[i], sh.Name) {
						owner = index
					}
				}
			}
			if owner != index {
				continue
			}
			if n.name == "_xlnm.print_area" {
				sh.Page.PrintAreas = append(sh.Page.PrintAreas, a)
				continue
			}
			switch {
			case a.FirstColumn == 0 && a.LastColumn == maxColumns-1:
				rows := a
				sh.Page.RepeatRows = &rows
			case a.FirstRow == 0 && a.LastRow == maxRows-1:
				cols := a
				sh.Page.RepeatColumns = &cols
			}
		}
	}
}
