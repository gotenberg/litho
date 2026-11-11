package sheet

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

func TestPrinted(t *testing.T) {
	names := []string{"A", "B", "C", "a"}
	hidden := []bool{false, true, false, false}
	tests := []struct {
		name string
		opts office.Options
		want []bool
		err  string
	}{
		{"default leaves the hidden out", office.Options{}, []bool{true, false, true, true}, ""},
		{"hidden sheets", office.Options{HiddenSheets: true}, []bool{true, true, true, true}, ""},
		{"names, exact, in book order", office.Options{Sheets: []string{"C", "B"}}, []bool{false, true, true, false}, ""},
		{"a name matches its case", office.Options{Sheets: []string{"a"}}, []bool{false, false, false, true}, ""},
		{"a name twice", office.Options{Sheets: []string{"A", "A"}}, []bool{true, false, false, false}, ""},
		{"numbers count the hidden", office.Options{SheetNumbers: []int{2, 4}}, []bool{false, true, false, true}, ""},
		{"names override hidden sheets", office.Options{Sheets: []string{"A"}, HiddenSheets: true}, []bool{true, false, false, false}, ""},
		{"a missing name", office.Options{Sheets: []string{"A", "Z"}}, nil, `no sheet named "Z"`},
		{"a number past the book", office.Options{SheetNumbers: []int{5}}, nil, "no sheet 5: it has 4"},
		{"number 0", office.Options{SheetNumbers: []int{0}}, nil, "no sheet 0"},
	}
	for _, tt := range tests {
		got, err := Printed(names, hidden, tt.opts)
		if tt.err != "" {
			if !errors.Is(err, office.ErrInvalidOptions) || !strings.Contains(err.Error(), tt.err) {
				t.Errorf("%s: err = %v, want %q", tt.name, err, tt.err)
			}
			continue
		}
		if err != nil || !slices.Equal(got, tt.want) {
			t.Errorf("%s: Printed = %v, %v, want %v", tt.name, got, err, tt.want)
		}
	}
	// A book that hides every sheet prints its first.
	if got, err := Printed([]string{"X", "Y"}, []bool{true, true}, office.Options{}); err != nil || !slices.Equal(got, []bool{true, false}) {
		t.Errorf("all hidden: %v, %v", got, err)
	}
	if _, err := Printed([]string{"X"}, nil, office.Options{}); !errors.Is(err, office.ErrInvalidOptions) {
		t.Errorf("unequal lengths: %v", err)
	}
}

// selectionBook returns a book of five sheets, One to Five, Two hidden:
// One links to Three and Two, reads Three in a formula, and counts the
// pages in its footer.
func selectionBook() *Document {
	one := &Sheet{Name: "One", Rows: []Row{{Index: 0, Cells: []Cell{
		{Column: 0, Value: Value{Kind: Text, Text: "to three"}, Href: "#Three!A1"},
		{Column: 1, Value: Value{Kind: Text, Text: "to two"}, Href: "#Two!A1"},
		{Column: 2, Value: Value{Kind: Number, Number: 1}, Formula: "Three!A1*2"},
	}}}}
	one.HeaderFooter.Odd.Footer[1] = []Span{{Text: "P:"}, {Field: FieldPage}, {Text: "/"}, {Field: FieldPages}}
	doc := newDoc(one)
	for i, name := range []string{"Two", "Three", "Four", "Five"} {
		sh := &Sheet{Name: name, Hidden: name == "Two", DefaultColumnWidth: 64, DefaultRowHeight: 12.78, Page: a4(),
			Rows: []Row{{Index: 0, Cells: []Cell{numCell(0, float64(i+2), nil)}}}}
		doc.Sheets = append(doc.Sheets, sh)
	}
	return doc
}

// TestConvert_SheetSelection locks what follows the sheets that print:
// the outline, the links into the book, the page count of a footer, and
// the formulas, which still read a sheet that does not print.
func TestConvert_SheetSelection(t *testing.T) {
	outline := func(t *testing.T, opts office.Options) []string {
		t.Helper()
		out := convert(t, selectionBook(), opts)
		var titles []string
		for _, o := range out.Outline {
			titles = append(titles, o.Title)
		}
		return titles
	}
	if got := outline(t, office.Options{}); !slices.Equal(got, []string{"One", "Three", "Four", "Five"}) {
		t.Errorf("default outline %v", got)
	}
	if got := outline(t, office.Options{HiddenSheets: true}); !slices.Equal(got, []string{"One", "Two", "Three", "Four", "Five"}) {
		t.Errorf("hidden sheets outline %v", got)
	}
	if got := outline(t, office.Options{SheetNumbers: []int{5, 2}}); !slices.Equal(got, []string{"Two", "Five"}) {
		t.Errorf("numbers outline %v", got)
	}

	out := convert(t, selectionBook(), office.Options{Sheets: []string{"Two", "One"}})
	if len(out.Pages) != 2 {
		t.Fatalf("%d pages, want 2", len(out.Pages))
	}
	page := strings.Join(texts(out.Pages[0]), "")
	// Three holds 3: the formula reads it though it does not print; the
	// footer counts the two pages that print.
	if !strings.Contains(page, "6") || !strings.Contains(page, "P:1/2") {
		t.Errorf("first page %q, want the formula's 6 and P:1/2", page)
	}
	var dests []int
	for _, l := range out.Pages[0].Links {
		if l.Dest == nil {
			t.Errorf("link %+v, want a destination in the book", l)
			continue
		}
		dests = append(dests, l.Dest.Page)
	}
	// The link to Three, which does not print, is dropped; the one to
	// Two lands on its page.
	if !slices.Equal(dests, []int{2}) {
		t.Errorf("destinations %v, want [2]", dests)
	}

	if _, err := Convert(context.Background(), selectionBook(), pinnedFonts(t), office.Options{Sheets: []string{"Six"}}); !errors.Is(err, office.ErrInvalidOptions) || !strings.Contains(err.Error(), `"Six"`) {
		t.Errorf("missing sheet: %v", err)
	}
}

// TestApplyFormulas_Outside locks the formulas that keep their stored
// result whatever the recalculation: those that read another workbook,
// the clock or a random number.
func TestApplyFormulas_Outside(t *testing.T) {
	cells := []Cell{
		numCell(0, 2, nil),
		{Column: 1, Value: Value{Kind: Number, Number: 11}, Formula: "'[1]Other'!A1+1"},
		{Column: 2, Value: Value{Kind: Number, Number: 12}, Formula: "of:=['file:///tmp/b.ods'#$Other.A1]+1"},
		{Column: 3, Value: Value{Kind: Number, Number: 13}, Formula: "NOW()"},
		{Column: 4, Value: Value{Kind: Number, Number: 14}, Formula: "TODAY()+A1"},
		{Column: 5, Value: Value{Kind: Number, Number: 15}, Formula: "RAND()"},
		{Column: 6, Value: Value{Kind: Number, Number: 16}, Formula: "[1]Other!A1"},
		{Column: 7, Value: Value{Kind: Number, Number: 17}, Formula: "A1+D1"},
	}
	doc := newDoc(&Sheet{Name: "S", Rows: []Row{{Index: 0, Cells: cells}}})
	b := newBuilder(context.Background(), doc, nil, office.Options{})
	got := b.applyFormulas(doc).Sheets[0].Rows[0].Cells
	for i, want := range []float64{2, 11, 12, 13, 14, 15, 16, 15} {
		if got[i].Value.Number != want {
			t.Errorf("cell %d (%q) = %v, want %v", i, got[i].Formula, got[i].Value, want)
		}
	}
	if len(b.warnings) != 1 || b.warnings[0].Code != CodeFormula || !strings.Contains(b.warnings[0].Message, "NOW, RAND, TODAY") {
		t.Errorf("warnings %+v", b.warnings)
	}
}
