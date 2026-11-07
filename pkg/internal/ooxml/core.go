package ooxml

import (
	"context"
	"strings"
)

// CoreProperties are the document properties of docProps/core.xml
// (ECMA-376 Part 2, 11) the converters carry into the PDF. Dates are left
// out: the output depends on the content alone.
type CoreProperties struct {
	Title, Creator, Subject, Keywords, Description, Language string
}

// maxPropertyRunes caps a property's length. Anti-DoS guard, not
// spec-mandated: titles and keywords are a line or two.
const maxPropertyRunes = 4096

// CoreProperties reads the package's core properties part, the target of
// its core-properties relationship; a missing or broken part yields none.
func (p *Package) CoreProperties(ctx context.Context) CoreProperties {
	var cp CoreProperties
	name := p.RelatedPart("", RelCoreProperties)
	if name == "" {
		return cp
	}
	root, err := p.ParseXML(ctx, name, nil)
	if err != nil {
		return cp
	}
	for _, c := range root.Children {
		v := property(c.Text)
		switch {
		case c.Is(NSDC, "title"):
			cp.Title = v
		case c.Is(NSDC, "creator"):
			cp.Creator = v
		case c.Is(NSDC, "subject"):
			cp.Subject = v
		case c.Is(NSCP, "keywords"):
			cp.Keywords = v
		case c.Is(NSDC, "description"):
			cp.Description = v
		case c.Is(NSDC, "language"):
			cp.Language = v
		}
	}
	return cp
}

// property trims s, collapses its whitespace and caps its length.
func property(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > maxPropertyRunes {
		s = string(r[:maxPropertyRunes])
	}
	return s
}
