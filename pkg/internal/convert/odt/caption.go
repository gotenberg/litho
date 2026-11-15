package odt

import (
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// captionOf returns the picture frame that the draw:text-box element box
// captions, and the caption's text; nil when it captions none beyond
// doubt.
//
// OpenDocument ties a caption to its picture by nesting: the caption
// frame's text box (ODF 1.3 Part 3, 10.4.3 draw:text-box) holds one
// paragraph, which holds the picture's own frame (10.4.2 draw:frame with
// a 10.4.4 draw:image) beside the caption's text, numbered by a sequence
// field (7.4.13 text:sequence) of a sequence that numbers figures
// ([office.FigureSequence]). A text box of more paragraphs, or one that
// holds two frames or no sequence, captions nothing. The caption is the
// paragraph's text without the picture's frame.
//
// # Corpus evidence
//
//   - probe, odt: LibreOffice of the pinned image prints two such caption
//     frames as their pictures with "Figure 1: ..." and "Illustration 1:
//     ..." under them; its PDF/UA export tags four Figures, none with
//     /Alt.
func captionOf(box *odf.Element) (*odf.Element, string) {
	var para *odf.Element
	for _, c := range box.Children {
		switch {
		case c.IsText():
			if strings.TrimSpace(c.Text) != "" {
				return nil, ""
			}
		case c.Is(odf.NSText, "p") && para == nil:
			para = c
		default:
			return nil, ""
		}
	}
	if para == nil {
		return nil, ""
	}
	var frame *odf.Element
	frames, sequence := 0, false
	var sb strings.Builder
	var walk func(e *odf.Element, depth int)
	walk = func(e *odf.Element, depth int) {
		if depth > odf.MaxDepth {
			return
		}
		for _, c := range e.Children {
			switch {
			case c.IsText():
				sb.WriteString(c.Text)
			case c.Is(odf.NSDraw, "frame"):
				frames++
				if c.Child(odf.NSDraw, "image") != nil {
					frame = c
				}
			case c.Name.Space == odf.NSDraw:
				frames++
			case c.Is(odf.NSText, "s"), c.Is(odf.NSText, "tab"), c.Is(odf.NSText, "line-break"):
				sb.WriteByte(' ')
			case c.Is(odf.NSText, "note"), c.Is(odf.NSOffice, "annotation"):
			default:
				if c.Is(odf.NSText, "sequence") && office.FigureSequence(c.A(odf.NSText, "name")) {
					sequence = true
				}
				walk(c, depth+1)
			}
		}
	}
	walk(para, 0)
	if frames != 1 || frame == nil || !sequence {
		return nil, ""
	}
	if caption := office.Caption(sb.String()); caption != "" {
		return frame, caption
	}
	return nil, ""
}
