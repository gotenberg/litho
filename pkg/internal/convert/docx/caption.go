package docx

import (
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// markCaptions records, in d.captions, the caption of each picture among
// the block-level elements kids that a caption paragraph names beyond
// doubt, for a picture its source does not describe
// (office.Options.DerivedAltText).
//
// ECMA-376 ties no caption to a picture: a caption is a paragraph whose
// number a SEQ field counts (ECMA-376 Part 1, 17.16.5, SEQ), standing as
// the paragraph after, or before, the paragraph that holds its picture.
// So a caption paragraph, one holding
// a SEQ field of a sequence that numbers figures ([office.FigureSequence]),
// names the picture of the paragraph beside it that holds one inline
// picture and nothing else shown. A caption beside two such paragraphs,
// and a picture beside two captions, name none: either could be the
// other's. A caption is the paragraph's text, its field results
// included.
//
// # Corpus evidence
//
//   - litho/docx fields-seq-captions, sink-paper, sink-report;
//     libreoffice/docx FigureAsLabelPicture.docx: every figure caption
//     follows the paragraph of its picture alone; the table captions
//     follow text or nothing.
func (d *document) markCaptions(kids []*ooxml.Element) {
	var paras []*ooxml.Element
	for _, k := range kids {
		if k.Space != w {
			continue
		}
		switch k.Local {
		case "p":
			paras = append(paras, k)
		case "tbl", "sdt", "customXml", "altChunk":
			// Another block parts two paragraphs: neither is beside the
			// other.
			paras = append(paras, nil)
		}
	}
	pictures := make([]*ooxml.Element, len(paras))
	for i, p := range paras {
		pictures[i] = pictureAlone(p)
	}
	claims := map[*ooxml.Element]int{}
	named := map[*ooxml.Element]string{}
	for i, p := range paras {
		if p == nil || !captionParagraph(p) {
			continue
		}
		var prev, next *ooxml.Element
		if i > 0 {
			prev = pictures[i-1]
		}
		if i+1 < len(paras) {
			next = pictures[i+1]
		}
		if (prev == nil) == (next == nil) {
			continue
		}
		pic := prev
		if pic == nil {
			pic = next
		}
		claims[pic]++
		named[pic] = office.Caption(shownText(p))
	}
	for pic, n := range claims {
		if n != 1 || named[pic] == "" {
			continue
		}
		if d.captions == nil {
			d.captions = map[*ooxml.Element]string{}
		}
		d.captions[pic] = named[pic]
	}
}

// pictureAlone returns the drawing of the paragraph p when p shows one
// inline picture and nothing else: the wp:inline of a w:drawing whose
// graphic is a picture (ECMA-376 Part 1, 20.4.2.8 inline; 20.2.2.5 pic),
// or the v:shape of a w:pict or w:object holding a v:imagedata (Part 4,
// 19.1.2.11 imagedata). It returns nil for any other paragraph: one with
// text, with a floating drawing, with two pictures, or with none.
func pictureAlone(p *ooxml.Element) *ooxml.Element {
	if p == nil {
		return nil
	}
	var found *ooxml.Element
	count := 0
	text := false
	var walk func(e *ooxml.Element, depth int)
	walk = func(e *ooxml.Element, depth int) {
		if depth > maxNesting {
			return
		}
		for _, k := range e.Kids() {
			switch {
			case k.Is(w, "t"):
				if strings.TrimSpace(k.Text) != "" {
					text = true
				}
			case k.Is(w, "drawing"):
				for _, g := range k.Kids() {
					count++
					data := g.Child(ooxml.NSA, "graphic").Child(ooxml.NSA, "graphicData")
					if g.Is(ooxml.NSWP, "inline") && ooxml.GraphicURI(data) == uriPicture {
						found = g
					}
				}
			case k.Is(w, "pict"), k.Is(w, "object"):
				count++
				for _, sp := range k.Kids() {
					if sp.Is(ooxml.NSV, "shape") && sp.Child(ooxml.NSV, "imagedata") != nil {
						found = sp
					}
				}
			case k.Is(w, "pPr"), k.Is(w, "rPr"), k.Is(w, "del"), k.Is(w, "moveFrom"):
			default:
				walk(k, depth+1)
			}
		}
	}
	walk(p, 0)
	if text || count != 1 {
		return nil
	}
	return found
}

// captionParagraph reports whether the paragraph p holds a SEQ field of a
// figure sequence, simple or complex (ECMA-376 Part 1, 17.16.19
// fldSimple; 17.16.23 instrText).
func captionParagraph(p *ooxml.Element) bool {
	found := false
	var walk func(e *ooxml.Element, depth int)
	walk = func(e *ooxml.Element, depth int) {
		if depth > maxNesting || found {
			return
		}
		for _, k := range e.Kids() {
			switch {
			case k.Is(w, "fldSimple"):
				if seqFigure(attrOr(k, "instr")) {
					found = true
					return
				}
				walk(k, depth+1)
			case k.Is(w, "instrText"):
				if seqFigure(k.Text) {
					found = true
					return
				}
			case k.Is(w, "drawing"), k.Is(w, "pict"), k.Is(w, "object"), k.Is(w, "del"), k.Is(w, "moveFrom"):
			default:
				walk(k, depth+1)
			}
		}
	}
	walk(p, 0)
	return found
}

// seqFigure reports whether a field instruction is a SEQ field of a
// sequence that numbers figures.
func seqFigure(instr string) bool {
	f := strings.Fields(instr)
	return len(f) >= 2 && strings.EqualFold(f[0], "SEQ") && office.FigureSequence(f[1])
}

// shownText returns the text the paragraph p shows: its w:t runs in order,
// field results included, deleted text and the content of drawings left
// out.
func shownText(p *ooxml.Element) string {
	var sb strings.Builder
	var walk func(e *ooxml.Element, depth int)
	walk = func(e *ooxml.Element, depth int) {
		if depth > maxNesting || sb.Len() > office.MaxAltText {
			return
		}
		for _, k := range e.Kids() {
			switch {
			case k.Is(w, "t"):
				sb.WriteString(k.Text)
			case k.Is(w, "tab"):
				sb.WriteByte(' ')
			case k.Is(w, "drawing"), k.Is(w, "pict"), k.Is(w, "object"), k.Is(w, "del"), k.Is(w, "moveFrom"), k.Is(w, "instrText"), k.Is(w, "pPr"):
			default:
				walk(k, depth+1)
			}
		}
	}
	walk(p, 0)
	return sb.String()
}
