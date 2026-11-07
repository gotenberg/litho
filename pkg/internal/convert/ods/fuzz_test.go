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
