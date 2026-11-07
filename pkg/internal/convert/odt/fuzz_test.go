package odt

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// FuzzBuild runs the content parser, a flat document's body turned into
// the layout tree, on arbitrary XML: it must neither panic nor loop.
func FuzzBuild(f *testing.F) {
	flat := func(body string) []byte {
		return []byte(`<office:document ` + nsDecl + `><office:styles>` + listStyles + `</office:styles><office:body><office:text>` + body + `</office:text></office:body></office:document>`)
	}
	f.Add(flat(`<text:p>a<text:span>b</text:span><text:s text:c="3"/><text:tab/><text:line-break/></text:p>`))
	f.Add(flat(`<text:list text:style-name="LN"><text:list-item><text:p>x</text:p><text:list><text:list-item><text:p>y</text:p></text:list-item></text:list></text:list-item></text:list>`))
	f.Add(flat(`<table:table><table:table-column table:number-columns-repeated="3"/><table:table-row><table:table-cell table:number-columns-spanned="2"><text:p>c</text:p></table:table-cell></table:table-row></table:table>`))
	f.Add(flat(`<text:p>n<text:note><text:note-citation>1</text:note-citation><text:note-body><text:p>b</text:p></text:note-body></text:note></text:p>`)[:120])
	f.Fuzz(func(t *testing.T, data []byte) {
		pkg, err := odf.Open(context.Background(), data, odf.Options{})
		if err != nil {
			return
		}
		body := odf.Section(pkg.Content, "body").Child(odf.NSOffice, "text")
		if body == nil {
			return
		}
		b := &builder{
			ctx: context.Background(), pkg: pkg, styles: odf.NewStyles(pkg),
			metricsCache: map[string][2]float64{}, warned: map[string]bool{}, pics: map[string]picResult{},
			meta: map[*layout.Node]paraMeta{}, tabsRelative: true,
		}
		b.pages = newPageFlow(b, office.Options{})
		root, first := b.document(body)
		if root == nil || first.Width <= 0 {
			t.Fatalf("no root or page: %v %+v", root, first)
		}
	})
}
