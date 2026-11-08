package compose

import (
	"reflect"
	"testing"
)

func TestSelectPages_KeepsTheOrderAndRenumbers(t *testing.T) {
	page := func(n int) *Page {
		h := &StructElem{Role: "H1"}
		return &Page{Width: 100, Height: 100, Content: []Fragment{&Text{Parent: h}}, Links: []Link{
			{URI: "https://example.com/"},
			{Dest: &Dest{Page: 1}},
			{Dest: &Dest{Page: 3}},
			{Dest: &Dest{Page: n}},
		}}
	}
	mk := func() *Document {
		doc := &Document{Pages: []*Page{page(1), page(2), page(3), page(4)}, Structure: &StructElem{Role: "Document"}}
		for _, pg := range doc.Pages {
			doc.Structure.Kids = append(doc.Structure.Kids, pg.Content[0].(*Text).Parent)
		}
		doc.Outline = []Outline{
			{Title: "One", Dest: Dest{Page: 1}, Kids: []Outline{{Title: "One.a", Dest: Dest{Page: 2}}, {Title: "One.b", Dest: Dest{Page: 3}}}},
			{Title: "Four", Dest: Dest{Page: 4}},
		}
		doc.Warnings = []Warning{{Code: "a", Page: 1}, {Code: "b", Page: 3}, {Code: "c"}}
		return doc
	}

	doc := mk()
	p2, p3 := doc.Pages[1], doc.Pages[2]
	SelectPages(doc, []int{3, 2})
	if len(doc.Pages) != 2 || doc.Pages[0] != p3 || doc.Pages[1] != p2 {
		t.Fatalf("pages %d, want page 3 then page 2", len(doc.Pages))
	}
	// Page 3 came first: links to 1 go, links to 3 lead to 1.
	var dests []int
	for _, l := range doc.Pages[0].Links {
		if l.Dest != nil {
			dests = append(dests, l.Dest.Page)
		}
	}
	if !reflect.DeepEqual(dests, []int{1, 1}) || len(doc.Pages[0].Links) != 3 {
		t.Errorf("first page links %v of %d, want [1 1] of 3", dests, len(doc.Pages[0].Links))
	}
	// "One" sat on page 1, dropped: its kids take its place; "Four" goes.
	if len(doc.Outline) != 2 || doc.Outline[0].Title != "One.a" || doc.Outline[0].Dest.Page != 2 || doc.Outline[1].Title != "One.b" || doc.Outline[1].Dest.Page != 1 {
		t.Errorf("outline %+v", doc.Outline)
	}
	if doc.Warnings[0].Page != 0 || doc.Warnings[1].Page != 1 || doc.Warnings[2].Page != 0 {
		t.Errorf("warnings %+v", doc.Warnings)
	}
	if len(doc.Structure.Kids) != 2 {
		t.Errorf("%d structure kids, want the 2 of the pages kept", len(doc.Structure.Kids))
	}

	twice := mk()
	SelectPages(twice, []int{2, 2})
	if len(twice.Pages) != 2 || twice.Pages[0] != twice.Pages[1] || len(twice.Pages[0].Links) != 2 || twice.Pages[0].Links[1].Dest.Page != 1 {
		t.Errorf("a page kept twice: %d pages, links %+v", len(twice.Pages), twice.Pages[0].Links)
	}

	all := mk()
	SelectPages(all, nil)
	if len(all.Pages) != 4 || len(all.Outline) != 2 || len(all.Structure.Kids) != 4 {
		t.Errorf("an empty selection changed the document")
	}
}
