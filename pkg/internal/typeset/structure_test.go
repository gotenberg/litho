package typeset_test

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

func writeVersion(t *testing.T, doc *typeset.Document, v typeset.Version) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := typeset.Write(context.Background(), doc, &buf, typeset.Options{Version: v}); err != nil {
		t.Fatalf("Write(%s): %v", v, err)
	}
	return buf.Bytes()
}

// semanticDoc is a tagged document with a table whose cells span and head,
// a numbered list, a block quote holding code, a table row group and an
// internal link to a heading, and the structure elements it uses.
func semanticDoc(t *testing.T) (*typeset.Document, map[string]*typeset.StructElem) {
	t.Helper()
	el := map[string]*typeset.StructElem{
		"H1":    {Role: "H1"},
		"TH":    {Role: "TH", ColSpan: 2, Scope: typeset.ScopeColumn},
		"TD":    {Role: "TD", RowSpan: 3},
		"Lbl":   {Role: "Lbl"},
		"Code":  {Role: "Code"},
		"Link":  {Role: "Link", Alt: "Go to the title"},
		"Quote": {Role: "Quote"},
	}
	tr := &typeset.StructElem{Role: "TR", Kids: []*typeset.StructElem{el["TH"], el["TD"]}}
	thead := &typeset.StructElem{Role: "THead", Kids: []*typeset.StructElem{tr}}
	el["L"] = &typeset.StructElem{Role: "L", Numbering: typeset.NumberingOrdered, Kids: []*typeset.StructElem{
		{Role: "LI", Kids: []*typeset.StructElem{el["Lbl"]}},
	}}
	el["BlockQuote"] = &typeset.StructElem{Role: "BlockQuote", Kids: []*typeset.StructElem{
		{Role: "P", Kids: []*typeset.StructElem{el["Quote"]}},
	}}
	el["Document"] = &typeset.StructElem{Role: "Document", Lang: "en", Kids: []*typeset.StructElem{
		el["H1"],
		{Role: "Table", Kids: []*typeset.StructElem{thead}},
		el["L"], el["BlockQuote"], el["Code"], el["Link"],
	}}
	texts := []typeset.Fragment{}
	for i, name := range []string{"H1", "TH", "TD", "Lbl", "Quote", "Code", "Link"} {
		r := run(t, sans, 12, typeset.Point{X: 40, Y: float64(60 + 20*i)}, name)
		r.Parent = el[name]
		texts = append(texts, r)
	}
	page := letter(texts...)
	page.Links = []typeset.Link{{
		Rect:   typeset.Rect{X: 40, Y: 170, Width: 40, Height: 14},
		Dest:   &typeset.Dest{Page: 1, X: 40, Y: 48, Elem: el["H1"]},
		Parent: el["Link"],
	}}
	return &typeset.Document{
		Metadata:  typeset.Metadata{Title: "Semantics", Author: "An Author", Creator: "test", Lang: "en"},
		Structure: el["Document"],
		Pages:     []*typeset.Page{page},
		Outline:   []typeset.Outline{{Title: "Semantics", Dest: typeset.Dest{Page: 1, X: 40, Y: 48, Elem: el["H1"]}}},
	}, el
}

func mustContain(t *testing.T, out []byte, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !bytes.Contains(out, []byte(w)) {
			t.Errorf("output lacks %q", w)
		}
	}
}

func mustLack(t *testing.T, out []byte, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if bytes.Contains(out, []byte(w)) {
			t.Errorf("output holds %q", w)
		}
	}
}

func TestWriteTableAndListAttributes(t *testing.T) {
	t.Parallel()
	doc, _ := semanticDoc(t)
	out := writeVersion(t, doc, typeset.Version17)
	mustContain(t, out,
		"/A << /ColSpan 2 /O /Table /Scope /Column>>",
		"/A << /O /Table /RowSpan 3>>",
		// PDF 1.7 has no Ordered value: the nearest one is Decimal.
		"/A << /ListNumbering /Decimal /O /List>>",
	)
	out = writeVersion(t, doc, typeset.Version20)
	mustContain(t, out, "/A << /ListNumbering /Ordered /O /List>>")
	out = writeVersion(t, doc, typeset.Version14)
	// Scope is a PDF 1.5 attribute.
	mustContain(t, out, "/A << /ColSpan 2 /O /Table>>")
}

func TestWriteLinkContents(t *testing.T) {
	t.Parallel()
	doc, el := semanticDoc(t)
	out := writeVersion(t, doc, typeset.Version17)
	mustContain(t, out, "/Contents (Go to the title)")
	doc.Pages[0].Links[0].Contents = "Title"
	out = writeVersion(t, doc, typeset.Version17)
	mustContain(t, out, "/Contents (Title)")
	mustLack(t, out, "/Contents (Go to the title)")
	// With neither, the target stands in.
	el["Link"].Alt = ""
	doc.Pages[0].Links[0].Contents = ""
	out = writeVersion(t, doc, typeset.Version17)
	mustContain(t, out, "/Contents (Page 1)")
	doc.Pages[0].Links[0].Dest, doc.Pages[0].Links[0].URI = nil, "https://example.com/"
	out = writeVersion(t, doc, typeset.Version17)
	mustContain(t, out, "/Contents (https://example.com/)")
}

func TestWriteVersion14(t *testing.T) {
	t.Parallel()
	doc, _ := semanticDoc(t)
	out := writeVersion(t, doc, typeset.Version14)
	if !bytes.HasPrefix(out, []byte("%PDF-1.4")) {
		t.Errorf("header %q", out[:8])
	}
	mustLack(t, out, "/Tabs", "/NS ", "/SD ")
	// The row group is a PDF 1.5 type: the role map names a PDF 1.4 one.
	mustContain(t, out, "/RoleMap << /THead /NonStruct>>", "/Dest [")
	if _, err := open(t, out).Metadata(context.Background()); err != nil {
		t.Fatalf("Metadata: %v", err)
	}
}

func TestWriteVersion17(t *testing.T) {
	t.Parallel()
	doc, _ := semanticDoc(t)
	out := writeVersion(t, doc, typeset.Version17)
	mustContain(t, out, "/Tabs /S", "/S /BlockQuote", "/S /Code", "/Info ")
	// Every role is a PDF 1.7 standard type: no role map.
	mustLack(t, out, "/RoleMap", "/NS ", "/SD ")
}

func TestWriteVersion20(t *testing.T) {
	t.Parallel()
	doc, _ := semanticDoc(t)
	out := writeVersion(t, doc, typeset.Version20)
	if !bytes.HasPrefix(out, []byte("%PDF-2.0")) {
		t.Errorf("header %q", out[:8])
	}
	mustContain(t, out,
		"/NS (http://iso.org/pdf2/ssn)", "/Namespaces [",
		// Removed PDF 1.7 types become their PDF 2.0 replacements: a block
		// quote a Div, code under a grouping element a P, a quote inside a
		// paragraph a Span.
		"/S /Div", "/S /P", "/S /Span",
		"/Subtype /XML",
	)
	mustLack(t, out, "/S /BlockQuote", "/S /Code", "/S /Quote", "/Info ", "/RoleMap")
	// The link and the outline entry go to the heading through a structure
	// destination beside the explicit one.
	if n := len(regexp.MustCompile(`/S /GoTo /SD \[\d+ 0 R /XYZ`).FindAll(out, -1)); n != 2 {
		t.Errorf("%d go-to actions with a structure destination, want 2", n)
	}
	if n := len(regexp.MustCompile(`/NS \d+ 0 R`).FindAll(out, -1)); n != 15 {
		t.Errorf("%d elements name the namespace, want all 15", n)
	}
	meta, err := open(t, out).Metadata(context.Background())
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	for k, want := range map[string]string{"Title": "Semantics", "Author": "An Author", "Creator": "test", "Producer": "litho"} {
		if meta[k] != want {
			t.Errorf("Metadata[%s] = %q, want %q", k, meta[k], want)
		}
	}
	if !bytes.Equal(out, writeVersion(t, doc, typeset.Version20)) {
		t.Error("two writes of the same document differ")
	}
}

func TestWriteRoleMapsUnknownRoles(t *testing.T) {
	t.Parallel()
	custom := &typeset.StructElem{Role: "Sidebar"}
	root := &typeset.StructElem{Role: "Document", Kids: []*typeset.StructElem{custom}}
	r := run(t, sans, 12, typeset.Point{X: 40, Y: 60}, "Aside")
	r.Parent = custom
	doc := &typeset.Document{Structure: root, Pages: []*typeset.Page{letter(r)}}
	for _, v := range []typeset.Version{typeset.Version14, typeset.Version17, typeset.Version20} {
		out := writeVersion(t, doc, v)
		mustContain(t, out, "/RoleMap << /Sidebar /NonStruct>>", "/S /Sidebar")
		if v == typeset.Version20 && bytes.Contains(out, []byte("/S /Sidebar /Type /StructElem /NS")) {
			t.Error("a role no namespace defines names the PDF 2.0 namespace")
		}
	}
}

func TestValidateRefusesBadSemantics(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(doc *typeset.Document, el map[string]*typeset.StructElem){
		"span past the cap": func(_ *typeset.Document, el map[string]*typeset.StructElem) {
			el["TH"].ColSpan = typeset.MaxCellSpan + 1
		},
		"negative span":     func(_ *typeset.Document, el map[string]*typeset.StructElem) { el["TD"].RowSpan = -1 },
		"unknown scope":     func(_ *typeset.Document, el map[string]*typeset.StructElem) { el["TH"].Scope = 9 },
		"unknown numbering": func(_ *typeset.Document, el map[string]*typeset.StructElem) { el["L"].Numbering = 99 },
		"unknown print role": func(_ *typeset.Document, el map[string]*typeset.StructElem) {
			el["Lbl"].PrintField.Role = 99
		},
		"unknown check state": func(_ *typeset.Document, el map[string]*typeset.StructElem) {
			el["Lbl"].PrintField.Checked = 9
		},
		"description past the cap": func(_ *typeset.Document, el map[string]*typeset.StructElem) {
			el["Lbl"].PrintField.Desc = strings.Repeat("a", typeset.MaxFieldText+1)
		},
		"description not UTF-8": func(_ *typeset.Document, el map[string]*typeset.StructElem) {
			el["Lbl"].PrintField.Desc = "\xff"
		},
	} {
		doc, el := semanticDoc(t)
		mutate(doc, el)
		if err := doc.Validate(t.Context()); !errors.Is(err, typeset.ErrInvalidElement) {
			t.Errorf("%s: Validate = %v, want ErrInvalidElement", name, err)
		}
	}
	doc, _ := semanticDoc(t)
	doc.Outline[0].Dest.Elem = &typeset.StructElem{Role: "H1"}
	if err := doc.Validate(t.Context()); !errors.Is(err, typeset.ErrInvalidDest) {
		t.Errorf("outline to an element outside the tree: Validate = %v, want ErrInvalidDest", err)
	}
	doc, _ = semanticDoc(t)
	doc.Pages[0].Links[0].Dest.Elem = &typeset.StructElem{Role: "H1"}
	if err := doc.Validate(t.Context()); !errors.Is(err, typeset.ErrInvalidDest) {
		t.Errorf("link to an element outside the tree: Validate = %v, want ErrInvalidDest", err)
	}
}

func TestPruneStructureDropsDanglingDests(t *testing.T) {
	t.Parallel()
	doc, el := semanticDoc(t)
	// The heading's text goes: the heading is pruned, and the destinations
	// that named it keep their page position.
	doc.Pages[0].Content = doc.Pages[0].Content[1:]
	shared := doc.Pages[0].Links[0].Dest
	typeset.PruneStructure(doc)
	if d := doc.Pages[0].Links[0].Dest; d.Elem != nil || d.Page != 1 {
		t.Errorf("link destination %+v, want page 1 with no element", d)
	}
	if shared.Elem != el["H1"] {
		t.Error("PruneStructure changed a destination the caller holds")
	}
	if doc.Outline[0].Dest.Elem != nil {
		t.Error("outline destination keeps a pruned element")
	}
	if err := doc.Validate(t.Context()); err != nil {
		t.Errorf("Validate after pruning: %v", err)
	}
}

func TestPruneStructureKeepsTheGridOfATable(t *testing.T) {
	t.Parallel()
	full := &typeset.StructElem{Role: "TD"}
	empty := &typeset.StructElem{Role: "TD"}
	emptyRow := &typeset.StructElem{Role: "TR", Kids: []*typeset.StructElem{{Role: "TD", Kids: []*typeset.StructElem{{Role: "P"}}}}}
	row := &typeset.StructElem{Role: "TR", Kids: []*typeset.StructElem{full, empty}}
	table := &typeset.StructElem{Role: "Table", Kids: []*typeset.StructElem{row, emptyRow}}
	emptyTable := &typeset.StructElem{Role: "Table", Kids: []*typeset.StructElem{{Role: "TR", Kids: []*typeset.StructElem{{Role: "TD"}}}}}
	root := &typeset.StructElem{Role: "Document", Kids: []*typeset.StructElem{table, emptyTable}}
	r := run(t, sans, 12, typeset.Point{X: 40, Y: 60}, "a")
	r.Parent = full
	doc := &typeset.Document{Structure: root, Pages: []*typeset.Page{letter(r)}}
	typeset.PruneStructure(doc)
	if len(root.Kids) != 1 || root.Kids[0] != table {
		t.Fatalf("document kids %v, want the table with content alone", root.Kids)
	}
	if len(table.Kids) != 2 || len(row.Kids) != 2 || len(emptyRow.Kids) != 1 {
		t.Errorf("table rows %d, cells %d and %d: want the whole grid", len(table.Kids), len(row.Kids), len(emptyRow.Kids))
	}
	if len(emptyRow.Kids[0].Kids) != 0 {
		t.Error("the empty paragraph of an empty cell is kept")
	}
}

// TestWriteVersion20KeepsTableOfContents locks that a PDF 2.0 file writes a
// TOC and its TOCI in the default standard structure namespace, the PDF
// 1.7 one (ISO 32000-2, 14.8.6.1), so the TOC holds its Caption as ISO
// 32005, Table 5 lets it, where a Div may not; the Caption and the entry's
// paragraph stay in the PDF 2.0 namespace, and the entry's link names the
// heading it leads to in its /Ref (Table 355; ISO 14289-2, 8.2.5.8). A
// table whose entries name no target is written as a Div.
func TestWriteVersion20KeepsTableOfContents(t *testing.T) {
	t.Parallel()
	caption := &typeset.StructElem{Role: "Caption"}
	link := &typeset.StructElem{Role: "Link"}
	p := &typeset.StructElem{Role: "P", Kids: []*typeset.StructElem{link}}
	toci := &typeset.StructElem{Role: "TOCI", Kids: []*typeset.StructElem{p}}
	toc := &typeset.StructElem{Role: "TOC", Kids: []*typeset.StructElem{caption, toci}}
	h1 := &typeset.StructElem{Role: "H1"}
	root := &typeset.StructElem{Role: "Document", Kids: []*typeset.StructElem{toc, h1}}
	title := run(t, sans, 12, typeset.Point{X: 40, Y: 60}, "Contents")
	title.Parent = caption
	entry := run(t, sans, 12, typeset.Point{X: 40, Y: 80}, "One")
	entry.Parent = link
	heading := run(t, sans, 12, typeset.Point{X: 40, Y: 120}, "One")
	heading.Parent = h1
	pg := letter(title, entry, heading)
	pg.Links = []typeset.Link{{Rect: typeset.Rect{X: 40, Y: 70, Width: 30, Height: 12}, Dest: &typeset.Dest{Page: 1, Elem: h1}, Parent: link}}
	doc := &typeset.Document{Structure: root, Pages: []*typeset.Page{pg}}
	out := writeVersion(t, doc, typeset.Version20)
	mustContain(t, out, "/S /TOC /Type /StructElem", "/S /TOCI /Type /StructElem", "/S /Caption", "/S /P")
	mustLack(t, out, "/S /Div", "/S /TOC /Type /StructElem /NS", "/S /TOCI /Type /StructElem /NS", "/RoleMap")
	if n := len(regexp.MustCompile(`/NS \d+ 0 R`).FindAll(out, -1)); n != 5 {
		t.Errorf("%d elements name the PDF 2.0 namespace, want the Document, the Caption, the P, the Link and the H1", n)
	}
	if n := len(regexp.MustCompile(`/Ref \[\d+ 0 R\]`).FindAll(out, -1)); n != 1 {
		t.Errorf("%d /Ref entries, want the link's", n)
	}
	mustLack(t, writeVersion(t, doc, typeset.Version17), "/Ref ")
	// An entry that names no target turns the table into a Div.
	pg.Links = nil
	out = writeVersion(t, doc, typeset.Version20)
	mustContain(t, out, "/S /Div")
	mustLack(t, out, "/S /TOC", "/S /TOCI", "/Ref ")
}

// printFieldDoc is a tagged page with a check box printed as content and a
// list box printed as text, each a Form element with PrintField
// attributes and no widget, and a text field whose Form element, which
// carries the attributes too, holds its widget.
func printFieldDoc(t *testing.T) *typeset.Document {
	t.Helper()
	box := &typeset.StructElem{Role: "Form", PrintField: typeset.PrintField{Role: typeset.PrintCheckBox, Checked: typeset.CheckOn, Desc: "Agree"}}
	list := &typeset.StructElem{Role: "Form", PrintField: typeset.PrintField{Role: typeset.PrintList, Desc: "Fruit"}}
	field := &typeset.StructElem{Role: "Form", PrintField: typeset.PrintField{Role: typeset.PrintText, Desc: "Name"}}
	root := &typeset.StructElem{Role: "Document", Kids: []*typeset.StructElem{
		{Role: "P", Kids: []*typeset.StructElem{box, list, field}},
	}}
	mark := &typeset.Path{Ops: []typeset.PathOp{
		{Kind: typeset.MoveTo, P: [3]typeset.Point{{X: 40, Y: 40}}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{{X: 50, Y: 50}}},
	}, Stroke: &typeset.Stroke{Color: typeset.Black, Width: 1}, Parent: box}
	item := run(t, sans, 12, typeset.Point{X: 40, Y: 80}, "Apple")
	item.Parent = list
	page := letter(mark, item)
	page.Fields = []typeset.Field{{Kind: typeset.FieldText, Rect: typeset.Rect{X: 40, Y: 100, Width: 100, Height: 16}, Name: "name", Value: "Ada", Parent: field}}
	return &typeset.Document{Metadata: typeset.Metadata{Title: "Printed", Lang: "en"}, Structure: root, Pages: []*typeset.Page{page}}
}

// TestWritePrintFieldAttributes writes the PrintField attributes of a Form
// element that holds content and no widget, as ISO 32000-1, Table 348
// spells them in a PDF 1.7 file and ISO 32000-2, Table 383 in a PDF 2.0
// one, leaves them out of the Form element that holds its widget, and out
// of a file that predates them.
func TestWritePrintFieldAttributes(t *testing.T) {
	t.Parallel()
	out := writeVersion(t, printFieldDoc(t), typeset.Version17)
	mustContain(t, out,
		"/A << /Desc (Agree) /O /PrintField /Role /cb /checked /on>>",
		"/A << /Desc (Fruit) /O /PrintField /Role /tv>>",
	)
	mustLack(t, out, "(Name)", "/Checked")
	out = writeVersion(t, printFieldDoc(t), typeset.Version20)
	mustContain(t, out,
		"/A << /Checked /on /Desc (Agree) /O /PrintField /Role /cb>>",
		"/A << /Desc (Fruit) /O /PrintField /Role /lb>>",
	)
	mustLack(t, out, "/checked")
	for _, v := range []typeset.Version{typeset.Version14, typeset.Version16} {
		mustLack(t, writeVersion(t, printFieldDoc(t), v), "/PrintField")
	}
}
