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
	callID, bodyID := d.newID("nc"), d.newID("n")
	ns := &story{part: part, note: mark, noteID: bodyID, noteBack: "#" + callID}
	d.nested++
	content := finish(d.blocks(ns, nil, body, 1))
	d.nested--
	noteBody := &layout.Node{Kind: layout.Element, Style: d.blockStyle(), Role: "Note", Children: content}
	if !ns.noteRef {
		noteBody.ID = bodyID
	}
	// The call links to its note, and the note's number back to the call,
	// as LibreOffice exports a note.
	// Each link is a Link element, as ISO 14289-1, 7.18.1 asks of every
	// link annotation (ISO 32000-1, 14.8.4.4.2); the note's body hangs
	// from the call's parent, not from the Link.
	call := &layout.Node{Kind: layout.Element, Style: st, ID: callID, Href: "#" + bodyID, Role: "Link"}
	if mark != "" {
		call.Children = []*layout.Node{{Kind: layout.Text, Style: st, Text: mark}}
	}
	if foot {
		call.Footnote = noteBody
	} else {
		d.endnoteBodies = append(d.endnoteBodies, noteBody)
	}
	if len(call.Children) == 0 && !foot {
		return
	}
	sink.add(call)
}
