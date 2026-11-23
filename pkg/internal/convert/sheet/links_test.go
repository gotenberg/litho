package sheet

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// TestConvert_SheetLinks locks the links into the book: to the page that
// prints their sheet, whether a workbook or an OpenDocument spreadsheet
// names it; a link to no sheet is dropped.
func TestConvert_SheetLinks(t *testing.T) {
	a := &Sheet{Name: "A", Rows: []Row{{Index: 0, Cells: []Cell{
		{Column: 0, Value: Value{Kind: Text, Text: "x"}, Href: "#Other!$B$2"},
		{Column: 1, Value: Value{Kind: Text, Text: "y"}, Href: "#'Other'.B2"},
		{Column: 2, Value: Value{Kind: Text, Text: "z"}, Href: "#Nowhere!A1"},
		{Column: 3, Value: Value{Kind: Text, Text: "w"}, Href: "https://example.com/"},
	}}}}
	b := &Sheet{Name: "Other", Rows: []Row{{Index: 1, Cells: []Cell{textCell(1, "target", nil)}}}}
	doc := newDoc(a)
	b.DefaultColumnWidth, b.DefaultRowHeight, b.Page = 64, 12.78, a4()
	doc.Sheets = append(doc.Sheets, b)
	out := convert(t, doc, office.Options{})
	if len(out.Pages) != 2 {
		t.Fatalf("%d pages, want 2", len(out.Pages))
	}
	var dests []int
	uris := 0
	for _, l := range out.Pages[0].Links {
		switch {
		case l.Dest != nil:
			dests = append(dests, l.Dest.Page)
		case l.URI == "https://example.com/":
			uris++
		default:
			t.Errorf("link %+v", l)
		}
	}
	if len(dests) != 2 || dests[0] != 2 || dests[1] != 2 || uris != 1 {
		t.Errorf("destinations %v and %d web links, want [2 2] and 1", dests, uris)
	}
}

// TestConvert_LinkToSheetNotPrinted locks that under Calc's rules a link
// to a cell of a sheet that prints no page leads to the first page, and
// that under Excel's it is dropped (see [targetPage]).
func TestConvert_LinkToSheetNotPrinted(t *testing.T) {
	for _, excel := range []bool{false, true} {
		a := &Sheet{Name: "A", Rows: []Row{{Index: 0, Cells: []Cell{
			{Column: 0, Value: Value{Kind: Text, Text: "x"}, Href: "#'1.1.1.1'.C1"},
		}}}}
		b := &Sheet{Name: "1.1.1.1", Hidden: true, Rows: []Row{{Index: 0, Cells: []Cell{textCell(2, "target", nil)}}}}
		doc := newDoc(a)
		doc.ExcelRules = excel
		b.DefaultColumnWidth, b.DefaultRowHeight, b.Page = 64, 12.78, a4()
		doc.Sheets = append(doc.Sheets, b)
		out := convert(t, doc, office.Options{})
		var dests []int
		for _, l := range out.Pages[0].Links {
			if l.Dest != nil {
				dests = append(dests, l.Dest.Page)
			}
		}
		if want := !excel; (len(dests) == 1 && dests[0] == 1) != want {
			t.Errorf("Excel's rules %v: destinations %v", excel, dests)
		}
	}
}
