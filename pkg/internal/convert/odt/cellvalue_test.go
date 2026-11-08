package odt

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/layout"
)

func TestCellValueText_WriterShowsValues(t *testing.T) {
	t.Parallel()
	auto := `<number:number-style style:name="N1" number:language="fr" number:country="FR"><number:number number:decimal-places="2" number:min-integer-digits="1" number:grouping="true"/></number:number-style>` +
		`<number:date-style style:name="N2" number:language="fr" number:country="LU" number:format-source="language"><number:day/><number:text>/</number:text><number:month/><number:text>/</number:text><number:year/></number:date-style>` +
		`<style:style style:name="ce1" style:family="table-cell" style:data-style-name="N1"/>` +
		`<style:style style:name="ce2" style:family="table-cell" style:data-style-name="N2"/>`
	cell := func(attrs, text string) string {
		return `<table:table-cell ` + attrs + `><text:p>` + text + `</text:p></table:table-cell>`
	}
	body := `<table:table><table:table-column table:number-columns-repeated="6"/><table:table-row>` +
		cell(`table:style-name="ce1" office:value-type="float" office:value="500000"`, "") +
		cell(`table:style-name="ce2" office:value-type="date" office:date-value="2012-02-18T00:00:00.0Z"`, "") +
		cell(`office:value-type="string" office:string-value="USD"`, "hidden") +
		cell(`office:value-type="float" office:value="7"`, "kept") +
		cell(`table:style-name="ce1" office:value-type="float" office:value="0"`, "Banana") +
		cell(``, "plain") +
		`</table:table-row></table:table>`
	b := newTestBuilder(t, doc{contentAuto: auto, body: body})
	root, _ := b.document(odfBody(b))
	var got []string
	walkNodes(root, func(n *layout.Node) {
		if n.Role == "TD" {
			got = append(got, nodeText(n))
		}
	})
	want := "500 000,00|18/02/2012|USD|kept|Banana|plain"
	if strings.Join(got, "|") != want {
		t.Errorf("cells %q, want %q", strings.Join(got, "|"), want)
	}
}

// nodeText joins the text of n's descendants.
func nodeText(n *layout.Node) string {
	var b strings.Builder
	walkNodes(n, func(k *layout.Node) {
		if k.Kind == layout.Text {
			b.WriteString(k.Text)
		}
	})
	return b.String()
}
