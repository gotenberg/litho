package annotation

import (
	"context"
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// extractXFAData checks the AcroForm dictionary for an /XFA key, resolves and
// concatenates the XFA stream(s), parses the XML, and returns a flat
// map[string]string of field name → value extracted from the <xfa:datasets>
// section.
//
// Returns nil, nil if no /XFA key is present.
func extractXFAData(ctx context.Context, doc *document.Document, acroFormDict ast.PdfDict) (map[string]string, error) {
	xfaObj, ok := acroFormDict["XFA"]
	if !ok {
		return nil, nil
	}

	xfaResolved, err := document.ResolveObject(ctx, doc, xfaObj)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve /XFA: %w", err)
	}

	xmlData, err := assembleXFAXML(ctx, doc, xfaResolved)
	if err != nil {
		return nil, fmt.Errorf("assembling XFA XML: %w", err)
	}

	if len(xmlData) == 0 {
		return nil, nil
	}

	return parseXFADatasets(xmlData)
}

// maxXFADataSize bounds the decoded XFA payload one /AcroForm may carry. It is
// an anti-DoS guard, not spec-mandated.
//
// Per ISO 32000-1, Table 224 the /XFA value may be an array alternating packet
// names with their streams, and the per-stream decompression cap says nothing
// about their sum: every entry decodes under its own maxDecompressedSize, so a
// file grows the assembled buffer with the number of entries it names rather
// than with anything the form could legitimately hold. Measured on an array of
// 20 compressed streams, 328 KB of input assembled 160 MB.
//
// It bounds both shapes /XFA takes. The single-stream shape carries the same
// payload as the array, so bounding only the array would let a file evade the
// limit by writing one stream instead of several and fall back to the 256 MB
// decompression cap, sixteen times this one.
//
// 16 MB is chosen against the corpora rather than by round number. The 17
// XFA-bearing files across all of them assemble at most 2.34 MB (pdfjs
// xfa_filled_imm1344e.pdf, a 20-entry array in a 3.02 MB file) and none names
// more than 30 entries, so this leaves better than 6x headroom over the
// heaviest real form. That is wider headroom than the page-content bound takes,
// because 17 files is a thin sample to call a ceiling.
//
// It is deliberately not compliance's maxXFAScanBytes, which is 1 MB. That
// bound reads only far enough to find the dynamic-render setting, which every
// real file places in its first packet inside 163 bytes, so it may legitimately
// stop below the size of a valid form. This one has to hold a whole form.
const maxXFADataSize = 16 << 20

// assembleXFAXML resolves the /XFA value into a single XML byte slice.
//
// The /XFA value can be either:
//   - A single stream object containing the entire XFA XML.
//   - An array of alternating name/stream pairs:
//     [ "preamble" <stream1> "datasets" <stream2> "template" <stream3> ... ]
func assembleXFAXML(ctx context.Context, doc *document.Document, xfaObj ast.PdfObject) ([]byte, error) {
	switch v := xfaObj.(type) {
	case ast.PdfStream:
		data, err := doc.DecodeStream(ctx, v)
		if err != nil {
			return nil, fmt.Errorf("failed to decode XFA stream: %w", err)
		}
		if len(data) > maxXFADataSize {
			return nil, fmt.Errorf("XFA data exceeds %d MB limit", maxXFADataSize>>20)
		}
		return data, nil

	case ast.PdfArray:
		return assembleXFAArray(ctx, doc, v)

	default:
		return nil, fmt.Errorf("unexpected /XFA type: %T", xfaObj)
	}
}

// assembleXFAArray processes the alternating name/stream pairs in an /XFA array
// and concatenates all decoded stream data.
func assembleXFAArray(ctx context.Context, doc *document.Document, arr ast.PdfArray) ([]byte, error) {
	var buf []byte

	// The array alternates: name (string), stream reference, name, stream, ...
	for i := 0; i+1 < len(arr); i += 2 {
		// arr[i] is the packet name (e.g., "preamble", "datasets", "template").
		// arr[i+1] is the stream reference.
		stream, err := document.ResolveStream(ctx, doc, arr[i+1])
		if err != nil {
			continue
		}
		if stream.Dict == nil {
			continue
		}

		data, err := doc.DecodeStream(ctx, stream)
		if err != nil {
			continue
		}

		// No separator between packets: they concatenate into one XML
		// document, unlike the page content streams this loop otherwise
		// mirrors, which are joined with a newline.
		buf = append(buf, data...)
		if len(buf) > maxXFADataSize {
			return nil, fmt.Errorf("XFA data exceeds %d MB limit", maxXFADataSize>>20)
		}
	}

	return buf, nil
}

// parseXFADatasets parses concatenated XFA XML and extracts field values from
// the <xfa:datasets> → <xfa:data> section into a flat map.
func parseXFADatasets(xmlData []byte) (map[string]string, error) {
	decoder := xml.NewDecoder(strings.NewReader(string(xmlData)))

	// Walk the token stream until we find the <xfa:data> element inside
	// <xfa:datasets>.
	inDatasets := false
	for {
		tok, err := decoder.Token()
		if err != nil {
			return nil, nil
		}

		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "datasets" {
				inDatasets = true
			}
			if inDatasets && t.Name.Local == "data" {
				result := make(map[string]string)
				if err := walkXFADataTree(decoder, "", result); err != nil {
					return nil, fmt.Errorf("walking XFA data tree: %w", err)
				}
				return result, nil
			}
		case xml.EndElement:
			if t.Name.Local == "datasets" {
				inDatasets = false
			}
		}
	}
}

// walkXFADataTree recursively walks the XML tree inside <xfa:data> and
// populates the result map with dotted-path keys and leaf text values.
func walkXFADataTree(decoder *xml.Decoder, prefix string, result map[string]string) error {
	for {
		tok, err := decoder.Token()
		if err != nil {
			return nil
		}

		switch t := tok.(type) {
		case xml.StartElement:
			name := t.Name.Local
			fullPath := name
			if prefix != "" {
				fullPath = prefix + "." + name
			}

			childText, err := collectXFAElement(decoder, fullPath, result)
			if err != nil {
				return fmt.Errorf("collecting XFA element %q: %w", fullPath, err)
			}

			if childText != "" {
				result[fullPath] = childText
			}

		case xml.EndElement:
			return nil
		}
	}
}

// collectXFAElement reads the content of a single XML element. If it contains
// only text (a leaf), it returns the text. If it contains child elements, it
// recurses and returns "".
func collectXFAElement(decoder *xml.Decoder, path string, result map[string]string) (string, error) {
	var textBuf strings.Builder
	hasChildren := false

	for {
		tok, err := decoder.Token()
		if err != nil {
			// Trimmed exactly as the well-formed path below trims. A truncated
			// packet ends the token stream mid-element, and returning the
			// buffer raw stored an untrimmed value in a map whose every other
			// value is trimmed, so one field's spacing depended on where the
			// file happened to be cut.
			return strings.TrimSpace(textBuf.String()), nil
		}

		switch t := tok.(type) {
		case xml.CharData:
			textBuf.Write(t)

		case xml.StartElement:
			hasChildren = true
			childPath := path + "." + t.Name.Local

			childText, err := collectXFAElement(decoder, childPath, result)
			if err != nil {
				return "", fmt.Errorf("collecting XFA child element %q: %w", childPath, err)
			}
			if childText != "" {
				result[childPath] = childText
			}

		case xml.EndElement:
			if hasChildren {
				return "", nil
			}
			return strings.TrimSpace(textBuf.String()), nil
		}
	}
}
