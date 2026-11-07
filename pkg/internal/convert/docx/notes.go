package docx

import (
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// noteCall builds a footnote or endnote reference (17.11.14, 17.11.7): the
// call with the note's number, and the note's body, laid out in the
// footnote area of the call's page for a footnote, at the end of the
// document for an endnote. A customMarkFollows reference draws no number:
// the text after it is the mark.
func (d *document) noteCall(s *story, c *ooxml.Element, st *css.Style, sink *inlineSink) {
	foot := c.Local == "footnoteReference"
	notes, part := d.endnotes, d.endnotesPart
	if foot {
		notes, part = d.footnotes, d.footnotesPart
	}
	body := notes[attrOr(c, "id")]
	if body == nil || s.note != "" {
		return
	}
	mark := ""
	if custom := hasAttr(c, "customMarkFollows") && ooxml.OnOff(attrOr(c, "customMarkFollows"), true); !custom {
		if foot {
			d.footnoteNumber++
			mark = strconv.Itoa(d.footnoteNumber)
		} else {
			d.endnoteNumber++
			mark = formatNumber(d.endnoteNumber, "lowerRoman")
		}
	}
	ns := &story{part: part, note: mark}
	d.nested++
	content := finish(d.blocks(ns, nil, body, 1))
	d.nested--
	noteBody := &layout.Node{Kind: layout.Element, Style: d.blockStyle(), Role: "Note", Children: content}
	call := &layout.Node{Kind: layout.Element, Style: st}
	if mark != "" {
		call.Children = []*layout.Node{{Kind: layout.Text, Style: st, Text: mark}}
	}
	if foot {
		call.Footnote = noteBody
	} else {
		id := d.newID("en")
		noteBody.ID = id
		call.Href = "#" + id
		d.endnoteBodies = append(d.endnoteBodies, noteBody)
	}
	if len(call.Children) == 0 && !foot {
		return
	}
	sink.add(call)
}
