package ods

import (
	"context"
	"testing"
)

func FuzzRead(f *testing.F) {
	doc := flat{
		auto: `<number:number-style style:name="N1"><number:number number:decimal-places="2" number:min-integer-digits="1"/><style:map style:condition="value()&gt;=0" style:apply-style-name="N1"/></number:number-style>` +
			`<style:style style:name="ce1" style:family="table-cell" style:data-style-name="N1"><style:table-cell-properties fo:border="1pt double #000000" style:rotation-angle="45"/></style:style>`,
		master: `<style:master-page style:name="Default"><style:header><style:region-left><text:p><text:sheet-name/></text:p></style:region-left></style:header></style:master-page>`,
		body:   table("S", `<table:table-column table:number-columns-repeated="3"/><table:table-row table:number-rows-repeated="2"><table:table-cell table:style-name="ce1" office:value-type="float" office:value="1" table:number-columns-spanned="2"><text:p>1<text:s text:c="2"/></text:p></table:table-cell></table:table-row>`),
	}.bytes()
	f.Add(doc)
	f.Add(doc[:len(doc)/2])
	f.Add(flat{
		auto: `<style:style style:name="ce2" style:family="table-cell"><style:map style:condition="cell-content()&lt;=2" style:apply-style-name="Good" style:base-cell-address="S.A1"/></style:style>`,
		body: table("S", `<table:table-column table:default-cell-style-name="ce2"/><table:table-row><table:table-cell office:value-type="float" office:value="1"/></table:table-row>`) +
			`<table:table table:name="T"><table:table-row><table:table-cell office:value-type="float" office:value="1"/></table:table-row><calcext:conditional-formats><calcext:conditional-format calcext:target-range-address="T.A1:T.B4">` +
			`<calcext:condition calcext:apply-style-name="Bad" calcext:value="between([.$A$1],&quot;x&quot;)" calcext:base-cell-address="T.A1"/>` +
			`<calcext:color-scale><calcext:color-scale-entry calcext:value="50" calcext:type="percentile" calcext:color="#ff0000"/><calcext:color-scale-entry calcext:value="0" calcext:type="maximum" calcext:color="#0000ff"/></calcext:color-scale>` +
			`<calcext:data-bar calcext:axis-position="middle"><calcext:formatting-entry calcext:value="0" calcext:type="auto-minimum"/><calcext:formatting-entry calcext:value="[.A1]" calcext:type="number"/></calcext:data-bar>` +
			`</calcext:conditional-format></calcext:conditional-formats></table:table>`,
	}.bytes())
	f.Fuzz(func(t *testing.T, data []byte) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		doc, _, err := Read(ctx, data, nil, "")
		if err != nil {
			return
		}
		for _, sh := range doc.Sheets {
			if sh.DefaultColumnWidth <= 0 || sh.DefaultRowHeight <= 0 || sh.Page.Width <= 0 || sh.Page.Height <= 0 {
				t.Fatalf("sheet %q has unresolved sizes: %+v", sh.Name, sh.Page)
			}
		}
	})
}
