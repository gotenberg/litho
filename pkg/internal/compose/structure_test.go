package compose_test

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

func writeVersion(t *testing.T, doc *compose.Document, v compose.Version) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := compose.Write(context.Background(), doc, &buf, compose.Options{Version: v}); err != nil {
		t.Fatalf("Write(%s): %v", v, err)
	}
	return buf.Bytes()
}

// semanticDoc is a tagged document with a table whose cells span and head,
// a numbered list, a block quote holding code, a table row group and an
// internal link to a heading, and the structure elements it uses.
func semanticDoc(t *testing.T) (*compose.Document, map[string]*compose.StructElem) {
	t.Helper()
	el := map[string]*compose.StructElem{
		"H1":    {Role: "H1"},
		"TH":    {Role: "TH", ColSpan: 2, Scope: compose.ScopeColumn},
		"TD":    {Role: "TD", RowSpan: 3},
		"Lbl":   {Role: "Lbl"},
		"Code":  {Role: "Code"},
		"Link":  {Role: "Link", Alt: "Go to the title"},
		"Quote": {Role: "Quote"},
	}
	tr := &compose.StructElem{Role: "TR", Kids: []*compose.StructElem{el["TH"], el["TD"]}}
	thead := &compose.StructElem{Role: "THead", Kids: []*compose.StructElem{tr}}
	el["L"] = &compose.StructElem{Role: "L", Numbering: compose.NumberingOrdered, Kids: []*compose.StructElem{
		{Role: "LI", Kids: []*compose.StructElem{el["Lbl"]}},
	}}
	el["BlockQuote"] = &compose.StructElem{Role: "BlockQuote", Kids: []*compose.StructElem{
		{Role: "P", Kids: []*compose.StructElem{el["Quote"]}},
	}}
	el["Document"] = &compose.StructElem{Role: "Document", Lang: "en", Kids: []*compose.StructElem{
		el["H1"],
		{Role: "Table", Kids: []*compose.StructElem{thead}},
		el["L"], el["BlockQuote"], el["Code"], el["Link"],
	}}
	texts := []compose.Fragment{}
	for i, name := range []string{"H1", "TH", "TD", "Lbl", "Quote", "Code", "Link"} {
		r := run(t, sans, 12, compose.Point{X: 40, Y: float64(60 + 20*i)}, name)
		r.Parent = el[name]
		texts = append(texts, r)
	}
	page := letter(texts...)
	page.Links = []compose.Link{{
		Rect:   compose.Rect{X: 40, Y: 170, Width: 40, Height: 14},
		Dest:   &compose.Dest{Page: 1, X: 40, Y: 48, Elem: el["H1"]},
		Parent: el["Link"],
	}}
	return &compose.Document{
		Metadata:  compose.Metadata{Title: "Semantics", Author: "An Author", Creator: "test", Lang: "en"},
		Structure: el["Document"],
		Pages:     []*compose.Page{page},
		Outline:   []compose.Outline{{Title: "Semantics", Dest: compose.Dest{Page: 1, X: 40, Y: 48, Elem: el["H1"]}}},
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
	out := writeVersion(t, doc, compose.Version17)
	mustContain(t, out,
		"/A << /ColSpan 2 /O /Table /Scope /Column>>",
		"/A << /O /Table /RowSpan 3>>",
		// PDF 1.7 has no Ordered value: the nearest one is Decimal.
		"/A << /ListNumbering /Decimal /O /List>>",
	)
	out = writeVersion(t, doc, compose.Version20)
	mustContain(t, out, "/A << /ListNumbering /Ordered /O /List>>")
	out = writeVersion(t, doc, compose.Version14)
	// Scope is a PDF 1.5 attribute.
	mustContain(t, out, "/A << /ColSpan 2 /O /Table>>")
}

func TestWriteLinkContents(t *testing.T) {
	t.Parallel()
	doc, el := semanticDoc(t)
	out := writeVersion(t, doc, compose.Version17)
	mustContain(t, out, "/Contents (Go to the title)")
	doc.Pages[0].Links[0].Contents = "Title"
	out = writeVersion(t, doc, compose.Version17)
	mustContain(t, out, "/Contents (Title)")
	mustLack(t, out, "/Contents (Go to the title)")
	el["Link"].Alt = ""
	doc.Pages[0].Links[0].Contents = ""
	out = writeVersion(t, doc, compose.Version17)
	mustLack(t, out, "/Contents (")
}

func TestWriteVersion14(t *testing.T) {
	t.Parallel()
	doc, _ := semanticDoc(t)
	out := writeVersion(t, doc, compose.Version14)
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
	out := writeVersion(t, doc, compose.Version17)
	mustContain(t, out, "/Tabs /S", "/S /BlockQuote", "/S /Code", "/Info ")
	// Every role is a PDF 1.7 standard type: no role map.
	mustLack(t, out, "/RoleMap", "/NS ", "/SD ")
}

func TestWriteVersion20(t *testing.T) {
	t.Parallel()
	doc, _ := semanticDoc(t)
	out := writeVersion(t, doc, compose.Version20)
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
	if !bytes.Equal(out, writeVersion(t, doc, compose.Version20)) {
		t.Error("two writes of the same document differ")
	}
}

func TestWriteRoleMapsUnknownRoles(t *testing.T) {
	t.Parallel()
	custom := &compose.StructElem{Role: "Sidebar"}
	root := &compose.StructElem{Role: "Document", Kids: []*compose.StructElem{custom}}
	r := run(t, sans, 12, compose.Point{X: 40, Y: 60}, "Aside")
	r.Parent = custom
	doc := &compose.Document{Structure: root, Pages: []*compose.Page{letter(r)}}
	for _, v := range []compose.Version{compose.Version14, compose.Version17, compose.Version20} {
		out := writeVersion(t, doc, v)
		mustContain(t, out, "/RoleMap << /Sidebar /NonStruct>>", "/S /Sidebar")
		if v == compose.Version20 && bytes.Contains(out, []byte("/S /Sidebar /Type /StructElem /NS")) {
			t.Error("a role no namespace defines names the PDF 2.0 namespace")
		}
	}
}

func TestValidateRefusesBadSemantics(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(doc *compose.Document, el map[string]*compose.StructElem){
		"span past the cap": func(_ *compose.Document, el map[string]*compose.StructElem) {
			el["TH"].ColSpan = compose.MaxCellSpan + 1
		},
		"negative span":     func(_ *compose.Document, el map[string]*compose.StructElem) { el["TD"].RowSpan = -1 },
		"unknown scope":     func(_ *compose.Document, el map[string]*compose.StructElem) { el["TH"].Scope = 9 },
		"unknown numbering": func(_ *compose.Document, el map[string]*compose.StructElem) { el["L"].Numbering = 99 },
	} {
		doc, el := semanticDoc(t)
		mutate(doc, el)
		if err := doc.Validate(); !errors.Is(err, compose.ErrInvalidElement) {
			t.Errorf("%s: Validate = %v, want ErrInvalidElement", name, err)
		}
	}
	doc, _ := semanticDoc(t)
	doc.Outline[0].Dest.Elem = &compose.StructElem{Role: "H1"}
	if err := doc.Validate(); !errors.Is(err, compose.ErrInvalidDest) {
		t.Errorf("outline to an element outside the tree: Validate = %v, want ErrInvalidDest", err)
	}
	doc, _ = semanticDoc(t)
	doc.Pages[0].Links[0].Dest.Elem = &compose.StructElem{Role: "H1"}
	if err := doc.Validate(); !errors.Is(err, compose.ErrInvalidDest) {
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
	compose.PruneStructure(doc)
	if d := doc.Pages[0].Links[0].Dest; d.Elem != nil || d.Page != 1 {
		t.Errorf("link destination %+v, want page 1 with no element", d)
	}
	if shared.Elem != el["H1"] {
		t.Error("PruneStructure changed a destination the caller holds")
	}
	if doc.Outline[0].Dest.Elem != nil {
		t.Error("outline destination keeps a pruned element")
	}
	if err := doc.Validate(); err != nil {
		t.Errorf("Validate after pruning: %v", err)
	}
}

func TestPruneStructureKeepsTheGridOfATable(t *testing.T) {
	t.Parallel()
	full := &compose.StructElem{Role: "TD"}
	empty := &compose.StructElem{Role: "TD"}
	emptyRow := &compose.StructElem{Role: "TR", Kids: []*compose.StructElem{{Role: "TD", Kids: []*compose.StructElem{{Role: "P"}}}}}
	row := &compose.StructElem{Role: "TR", Kids: []*compose.StructElem{full, empty}}
	table := &compose.StructElem{Role: "Table", Kids: []*compose.StructElem{row, emptyRow}}
	emptyTable := &compose.StructElem{Role: "Table", Kids: []*compose.StructElem{{Role: "TR", Kids: []*compose.StructElem{{Role: "TD"}}}}}
	root := &compose.StructElem{Role: "Document", Kids: []*compose.StructElem{table, emptyTable}}
	r := run(t, sans, 12, compose.Point{X: 40, Y: 60}, "a")
	r.Parent = full
	doc := &compose.Document{Structure: root, Pages: []*compose.Page{letter(r)}}
	compose.PruneStructure(doc)
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
