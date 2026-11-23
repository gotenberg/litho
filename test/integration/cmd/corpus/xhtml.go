package main

import (
	"bytes"
	"io"

	"golang.org/x/net/html"
)

// The markers of an XML CDATA section (XML 1.0, 2.7).
var (
	cdataOpen  = []byte("<![CDATA[")
	cdataClose = []byte("]]>")
)

// stripStyleCDATA removes the markers of the CDATA sections that sit in
// the style elements of an XHTML file, so that an HTML parser hands the
// style engine the sheet an XML parser hands it. An XML parser reads a
// section's content as character data and drops its markers (XML 1.0,
// 2.7); an HTML parser reads a style element as raw text (HTML Standard,
// 13.1.2), where the markers reach CSS as text and swallow the sheet.
// Nothing else changes: the section's content is the same text either way,
// since raw text decodes no reference and XML decodes none inside a
// section.
//
// It reports false, and returns src unchanged, when a marker sits
// anywhere else: in a script or the title, whose content an HTML parser
// reads otherwise, in the body, where an HTML parser reads a section as a
// bogus comment (13.2.5.42), or unbalanced, which no XML parser accepts.
// A marker inside a comment or an attribute value outside a style element
// is text to both parsers and stays. A file with no marker is returned as
// it is.
func stripStyleCDATA(src []byte) ([]byte, bool) {
	if !bytes.Contains(src, cdataOpen) && !bytes.Contains(src, cdataClose) {
		return src, true
	}
	z := html.NewTokenizer(bytes.NewReader(src))
	out := make([]byte, 0, len(src))
	inStyle := false
	for {
		tt := z.Next()
		// Raw is read first: TagName lowers the tag name in the buffer.
		raw := z.Raw()
		switch tt {
		case html.ErrorToken:
			if z.Err() != io.EOF {
				return src, false
			}
			return append(out, raw...), true
		case html.TextToken:
			if inStyle {
				text, ok := stripSections(raw)
				if !ok {
					return src, false
				}
				out = append(out, text...)
				continue
			}
			if bytes.Contains(raw, cdataOpen) || bytes.Contains(raw, cdataClose) {
				return src, false
			}
		case html.CommentToken:
			// A section outside raw text tokenizes as a bogus comment.
			if bytes.HasPrefix(raw, cdataOpen) {
				return src, false
			}
		case html.StartTagToken, html.SelfClosingTagToken, html.EndTagToken:
			// The tokenizer reads raw text after a style start tag, a
			// self-closed one included, as the HTML parser does.
			out = append(out, raw...)
			if name, _ := z.TagName(); string(name) == "style" {
				inStyle = tt != html.EndTagToken
			}
			continue
		case html.DoctypeToken:
			// A doctype holds no section to strip.
		}
		out = append(out, raw...)
	}
}

// stripSections removes the markers of the CDATA sections of text, the
// content of a style element, and keeps their content. It reports false
// for a section that does not close or a closing marker that opens none.
func stripSections(text []byte) ([]byte, bool) {
	var out []byte
	for {
		open, closing := bytes.Index(text, cdataOpen), bytes.Index(text, cdataClose)
		if open < 0 {
			return append(out, text...), closing < 0
		}
		if closing >= 0 && closing < open {
			return nil, false
		}
		out = append(out, text[:open]...)
		text = text[open+len(cdataOpen):]
		end := bytes.Index(text, cdataClose)
		if end < 0 {
			return nil, false
		}
		out = append(out, text[:end]...)
		text = text[end+len(cdataClose):]
	}
}

// xhtDoctype is the DOCTYPE an XHTML file is converted under. An XML
// parser leaves a document in no-quirks mode whatever its DOCTYPE (DOM
// Standard, 4.5), where an HTML parser takes the mode from the DOCTYPE
// (HTML Standard, 13.2.6.4.1): XHTML 1.0 Transitional sets limited-quirks
// mode, and a file without a DOCTYPE quirks mode. The parser takes the
// first DOCTYPE and drops a later one (13.2.6.4.2), so this one set before
// the file's keeps the HTML parse in no-quirks mode.
const xhtDoctype = "<!DOCTYPE html>"

// xhtNoQuirks returns the XHTML file src with [xhtDoctype] before it, after
// the UTF-8 byte order mark that opens it, if any.
func xhtNoQuirks(src []byte) []byte {
	bom := []byte("\xef\xbb\xbf")
	body, hadBOM := bytes.CutPrefix(src, bom)
	out := make([]byte, 0, len(src)+len(xhtDoctype))
	if hadBOM {
		out = append(out, bom...)
	}
	out = append(out, xhtDoctype...)
	return append(out, body...)
}

// The departures of an HTML parse from an XML parse that xhtDeparture
// finds, each the reason a file is kept out.
const (
	xhtHeadText = "XHTML with text in its head"
	xhtPreBreak = "XHTML whose pre element opens on a line feed"
)

// xhtDeparture returns how an HTML parser reads the XHTML file src
// otherwise than an XML parser, among two departures a static check finds,
// or "" for neither. Text in the head that is not white space ends the
// head in HTML and draws in the body (HTML Standard, 13.2.6.4.4), where an
// XML document keeps it in the head, which draws nothing. A line feed that
// opens a pre or listing element is dropped in HTML (13.2.6.4.7) and kept
// in XML. src is the file once stripStyleCDATA has stripped it.
func xhtDeparture(src []byte) string {
	z := html.NewTokenizer(bytes.NewReader(src))
	inHead, raw, afterPre := false, false, false
	for {
		tt := z.Next()
		opensPre := afterPre
		afterPre = false
		switch tt {
		case html.ErrorToken:
			return ""
		case html.TextToken:
			text := z.Text()
			if opensPre && (bytes.HasPrefix(text, []byte("\n")) || bytes.HasPrefix(text, []byte("\r\n"))) {
				return xhtPreBreak
			}
			if inHead && !raw && len(bytes.TrimSpace(text)) > 0 {
				return xhtHeadText
			}
		case html.StartTagToken, html.SelfClosingTagToken, html.EndTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "head":
				inHead = tt == html.StartTagToken
			case "body":
				inHead = false
			case "style", "title", "script":
				raw = tt == html.StartTagToken
			case "pre", "listing":
				afterPre = tt == html.StartTagToken
			}
		case html.CommentToken, html.DoctypeToken:
			// Neither draws or moves.
		}
	}
}
