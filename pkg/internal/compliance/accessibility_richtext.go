package compliance

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// maxRichTextBytes caps the /RC payload parsed for the ISO 14289-2, 8.9.2.3 t2
// comparison. It is an anti-DoS guard, not a spec-mandated limit: /RC holds a
// short rich text equivalent of /Contents, so a megabyte is already orders of
// magnitude above any real annotation, and the cap keeps a crafted stream from
// driving the XML parser through unbounded input.
const maxRichTextBytes = 1 << 20

// errRichTextNotXML reports a /RC payload that is not a parseable XML document.
// ISO 14289-2, 8.9.2.3 t2 compares the text of /RC with /Contents, so a payload
// that yields no text cannot match and the rule fires.
var errRichTextNotXML = errors.New("rich text is not a parseable XML document")

// checkMarkupAnnotRichText validates ISO 14289-2, 8.9.2.3 t2: when a markup
// annotation carries both /RC and /Contents, the text of the rich text string
// shall equal /Contents.
//
// The comparison mirrors veraPDF, which parses /RC as an XML document and
// reduces it to the concatenation of every text node, then compares that with
// /Contents for exact equality. No trimming and no whitespace collapsing take
// place on either side, attributes do not contribute, comments are dropped and
// processing instruction data counts.
func checkMarkupAnnotRichText(ctx context.Context, doc *document.Document, annot ast.PdfDict) []Violation {
	if _, hasRC := annot["RC"]; !hasRC {
		return nil
	}

	contents, err := document.ResolveString(ctx, doc, annot["Contents"])
	if err != nil {
		// Per ISO 14289-2, 8.9.2.3 t2 an absent /Contents satisfies the rule.
		doc.Logger.Debug("compliance.accessibility: /Contents absent or not a string, 8.9.2.3 t2 does not apply", "error", err)
		return nil
	}

	rich, err := richTextPayload(ctx, doc, annot["RC"])
	if err != nil {
		doc.Logger.Debug("compliance.accessibility: failed to read /RC", "error", err)
		return failf(Rule_14289_2_8_9_2_3_t02,
			"markup annotation /RC is unreadable and so cannot match /Contents")
	}

	text, err := richTextContent(rich)
	if err != nil {
		return failf(Rule_14289_2_8_9_2_3_t02,
			"markup annotation /RC yields no text and so cannot match /Contents %q",
			document.DecodePdfString(contents))
	}

	if plain := document.DecodePdfString(contents); text != plain {
		return failf(Rule_14289_2_8_9_2_3_t02,
			"markup annotation /RC text %q differs from /Contents %q", text, plain)
	}
	return nil
}

// richTextPayload returns the bytes of an /RC entry, which ISO 32000-2, 12.5.6.2
// allows to be either a text string or a stream.
//
// veraPDF reads a stream /RC with its filters left in place and so always
// reports a compressed one as non-matching. litho decodes the stream instead:
// mirroring that would trade a false negative here for a wrong answer on every
// real document that compresses its rich text.
func richTextPayload(ctx context.Context, doc *document.Document, obj ast.PdfObject) ([]byte, error) {
	if s, err := document.ResolveString(ctx, doc, obj); err == nil {
		return []byte(document.DecodePdfString(s)), nil
	}

	stream, err := document.ResolveStream(ctx, doc, obj)
	if err != nil {
		return nil, err
	}

	// The declared length is untrusted, so bound the payload against both the
	// cap and the file itself before materialising it.
	limit := int64(maxRichTextBytes)
	if size := doc.FileSize(); size > 0 && size < limit {
		limit = size
	}

	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		doc.Logger.Debug("compliance.accessibility: /RC stream decode failed, trying raw", "error", err)
		data, err = stream.Bytes(ctx)
		if err != nil {
			return nil, err
		}
	}
	if int64(len(data)) > limit {
		data = data[:limit]
	}
	return data, nil
}

// richTextContent returns the concatenated text of an XML document, which is
// what ISO 14289-2, 8.9.2.3 t2 compares with /Contents.
//
// Character data and processing instruction data contribute; comments and the
// XML declaration do not, matching the DOM veraPDF builds. A payload holding no
// element at all, or more than one root, is not a document and yields
// [errRichTextNotXML].
func richTextContent(data []byte) (string, error) {
	var (
		text    strings.Builder
		depth   int
		roots   int
		decoder = xml.NewDecoder(bytes.NewReader(data))
	)

	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", errRichTextNotXML
		}

		switch t := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				roots++
			}
			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth > 0 {
				text.Write(t)
			}
		case xml.ProcInst:
			// The XML declaration is not a node in the document veraPDF walks.
			if t.Target != "xml" {
				text.Write(t.Inst)
			}
		}
	}

	// A well-formed document holds exactly one root element. Anything else is
	// what veraPDF's parser rejects, leaving it with no text to compare.
	if roots != 1 || depth != 0 {
		return "", errRichTextNotXML
	}
	return text.String(), nil
}
