package ods

import "testing"

// TestRead_WrapIgnoresTheDefaultStyle checks that a cell wraps only where
// its own style chain says so: the table-cell default style's wrap does
// not reach the Default cell style, as Calc prints it.
//
// # Corpus evidence
//
//   - probe, ods: ods-mixed.ods, whose default style says wrap, prints 16
//     pages in the pinned LibreOffice, 31 with its cells wrapped.
func TestRead_WrapIgnoresTheDefaultStyle(t *testing.T) {
	t.Parallel()
	doc, _ := read(t, flat{
		styles: `<style:default-style style:family="table-cell"><style:table-cell-properties fo:wrap-option="wrap"/></style:default-style>` +
			`<style:style style:name="Default" style:family="table-cell"/>` +
			`<style:style style:name="Wrapped" style:family="table-cell" style:parent-style-name="Default"><style:table-cell-properties fo:wrap-option="wrap"/></style:style>` +
			`<style:style style:name="Child" style:family="table-cell" style:parent-style-name="Wrapped"/>`,
		body: table("S", `<table:table-row><table:table-cell office:value-type="string"><text:p>a</text:p></table:table-cell>`+
			`<table:table-cell table:style-name="Wrapped" office:value-type="string"><text:p>b</text:p></table:table-cell>`+
			`<table:table-cell table:style-name="Child" office:value-type="string"><text:p>c</text:p></table:table-cell></table:table-row>`),
	}.bytes())
	if doc.Default.Align.Wrap {
		t.Error("the Default cell style wraps, want the default style's wrap ignored")
	}
	sh := doc.Sheets[0]
	for col, want := range []bool{false, true, true} {
		c := cellAt(sh, 0, col)
		if c == nil {
			t.Fatalf("no cell at column %d", col)
		}
		got := doc.Default.Align.Wrap
		if c.Style != nil {
			got = c.Style.Align.Wrap
		}
		if got != want {
			t.Errorf("column %d wraps %v, want %v", col, got, want)
		}
	}
}
