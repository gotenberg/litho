package odf

import (
	"context"
	"testing"
)

func FuzzOpen(f *testing.F) {
	f.Add(zipOf(f, "mimetype", MimeText, "content.xml", minimalContent, "styles.xml", `<office:document-styles `+ns+`/>`))
	f.Add(zipOf(f, "mimetype", MimeText, "content.xml", minimalContent)[:60])
	f.Add([]byte(`<office:document ` + ns + `><office:body><office:text/></office:body></office:document>`))
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := Open(context.Background(), data)
		if err != nil {
			return
		}
		s := NewStyles(p)
		_ = s.Resolve(ScopeContent, FamilyParagraph, "Standard", ParagraphProps).Get("fo:margin-left")
	})
}

func FuzzParseXML(f *testing.F) {
	f.Add([]byte(minimalContent))
	f.Add([]byte(minimalContent[:40]))
	f.Add([]byte(`<!DOCTYPE a><a xmlns:b="u"><b:c d="e">t&amp;</b:c></a>`))
	f.Fuzz(func(t *testing.T, data []byte) {
		el, err := ParseXML(context.Background(), data)
		if err != nil {
			return
		}
		_ = el.TextContent()
	})
}
