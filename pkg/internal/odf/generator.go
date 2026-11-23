package odf

import (
	"encoding/xml"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/intconv"
)

// producer is what LibreOffice reads of a document's meta:generator: an
// OpenOffice.org or Apache OpenOffice build number (UPD), the major and
// minor version of a LibreOffice, or neither.
type producer struct {
	upd, build   int
	lo           bool
	major, minor int
}

// readProducer parses gen as LibreOffice's import does
// (SvXMLMetaDocumentContext::setBuildId): the build is the second
// product's "<UPD>m<milestone>$Build-<n>" ("OpenOffice.org_project/
// 330m20$Build-9567"), StarOffice and StarSuite 6 and 7 and
// OpenOffice.org 1 standing for UPD 645 when they carry none; a
// LibreOffice version is read from "LibreOffice/<major>.<minor>" when
// the generator names "LibreOffice_project/", which every LibreOffice
// since 3.3 writes.
func readProducer(gen string) producer {
	var p producer
	if sp := strings.IndexByte(gen, ' '); sp >= 0 {
		rest := gen[sp:]
		if sl := strings.IndexByte(rest, '/'); sl >= 0 {
			rest = rest[sl+1:]
			if m := strings.IndexByte(rest, 'm'); m >= 0 {
				if _, b, ok := strings.Cut(rest[m:], "$Build-"); ok {
					upd, err1 := intconv.Atoi(rest[:m])
					build, err2 := intconv.Atoi(b)
					if err1 == nil && err2 == nil {
						p.upd, p.build = upd, build
					}
				}
			}
		}
	}
	if p.upd == 0 {
		for _, pre := range []string{"StarOffice 7", "StarSuite 7", "StarOffice 6", "StarSuite 6", "OpenOffice.org 1"} {
			if strings.HasPrefix(gen, pre) {
				p.upd, p.build = 645, 8687
			}
		}
	}
	if strings.Contains(gen, "LibreOffice_project/") {
		for _, pre := range []string{"LibreOffice/", "LibreOfficeDev/"} {
			if v, ok := strings.CutPrefix(gen, pre); ok {
				major, rest, _ := strings.Cut(v, ".")
				mj, err1 := intconv.Atoi(major)
				mn, err2 := intconv.Atoi(leadingDigits(rest))
				if err1 == nil && err2 == nil {
					p.lo, p.major, p.minor = true, mj, mn
				}
			}
		}
	}
	return p
}

// openOffice reports a build of OpenOffice.org before Apache OpenOffice
// 4.0 (1.x, 2.x, 3.0 to 3.4) and one of Apache OpenOffice 4, as
// LibreOffice's SvXMLImport::getGeneratorVersion sorts the UPDs.
func (p producer) openOffice() (old, aoo4 bool) {
	switch {
	case p.upd >= 640 && p.upd <= 645, p.upd == 680, p.upd == 300 && p.build <= 9379,
		p.upd == 310, p.upd == 320, p.upd == 330, p.upd == 340:
		return true, false
	case p.upd == 400, p.upd == 401, p.upd >= 410:
		return false, true
	}
	return false, false
}

// LegacyShapeColours returns the fill and line colours LibreOffice gives
// the default graphic style of a text document an older producer wrote
// without them ("" for a colour it does not set), as its import of a
// style:default-style of the graphic family does
// (XMLGraphicsDefaultStyle::SetDefaults, fdo#75872): #99ccff and black
// for OpenOffice.org and LibreOffice before 4.1, #cfe7f5 and #808080 for
// Apache OpenOffice 4. A colour the default style states, and any
// document of another producer, keep theirs: the shape then takes the
// application's own defaults.
//
// # Corpus evidence
//
//   - libreoffice/odt: odfimport/fdo75872_ooo33.odt (OpenOffice.org 3.3)
//     prints its unstyled rectangle filled #99ccff and outlined black,
//     odfimport/fdo75872_aoo40.odt (OpenOffice 4.0) #cfe7f5 and #808080.
//   - probe, odt: through gotenberg/gotenberg:9.4.1, fdo75872_aoo40.odt
//     with its generator set to OpenOffice.org 2.4, 3.3 or 3.4 or to
//     LibreOffice 3.6 or 4.0 with their LibreOffice_project prints
//     #99ccff and black; to OpenOffice 4.1.7 #cfe7f5 and #808080; to no
//     generator, LibreOffice 7.4, OpenOffice.org/1.1.5 without a build,
//     StarOffice/7 with a build but no $Build- part or Calligra the
//     defaults #729fcf and #3465a4; with its default graphic style
//     taken out, the defaults; with a fill colour in that style, that
//     colour and #808080; with a line colour, #cfe7f5 and that colour.
//   - probe, odp: Impress draws an unstyled rectangle of a presentation
//     in its own default colours whatever the generator.
func LegacyShapeColours(p *Package) (fill, stroke string) {
	pr := readProducer(strings.TrimSpace(Section(p.Meta, "meta").Child(NSMeta, "generator").TextContent()))
	old, aoo4 := pr.openOffice()
	switch {
	case aoo4:
		return "#cfe7f5", "#808080"
	case old, pr.lo && (pr.major < 4 || pr.major == 4 && pr.minor < 1):
		return "#99ccff", "#000000"
	}
	return "", ""
}

// SetDefaultGraphicColours gives the default style of the graphic family
// the fill and line colours it does not state, the default style then
// read as if it held them; "" sets nothing. It does nothing without such
// a default style. It is meant for the builder of a [Styles], before it
// is shared.
func (s *Styles) SetDefaultGraphicColours(fill, stroke string) {
	d := s.defaults[FamilyGraphic]
	if d == nil || fill == "" && stroke == "" {
		return
	}
	nd := *d
	nd.Children = append([]*Element(nil), d.Children...)
	i := -1
	for k, c := range nd.Children {
		if c.Is(NSStyle, "graphic-properties") {
			i = k
			break
		}
	}
	var gp Element
	if i >= 0 {
		gp = *nd.Children[i]
	} else {
		gp = Element{Name: xml.Name{Space: NSStyle, Local: "graphic-properties"}}
	}
	gp.Attr = append([]xml.Attr(nil), gp.Attr...)
	for _, a := range []struct{ space, local, v string }{{NSDraw, "fill-color", fill}, {NSSVG, "stroke-color", stroke}} {
		if _, ok := gp.Lookup(a.space, a.local); a.v != "" && !ok {
			gp.Attr = append(gp.Attr, xml.Attr{Name: xml.Name{Space: a.space, Local: a.local}, Value: a.v})
		}
	}
	if i >= 0 {
		nd.Children[i] = &gp
	} else {
		nd.Children = append(nd.Children, &gp)
	}
	s.defaults[FamilyGraphic] = &nd
}

// leadingDigits returns the ASCII digits s starts with.
func leadingDigits(s string) string {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return s[:i]
}
