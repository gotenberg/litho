package annotation

import (
	"context"
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter"
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

// assembleXFAXML resolves the /XFA value into a single XML byte slice.
//
// The /XFA value can be either:
//   - A single stream object containing the entire XFA XML.
//   - An array of alternating name/stream pairs:
//     [ "preamble" <stream1> "datasets" <stream2> "template" <stream3> ... ]
func assembleXFAXML(ctx context.Context, doc *document.Document, xfaObj ast.PdfObject) ([]byte, error) {
	switch v := xfaObj.(type) {
	case ast.PdfStream:
		data, err := filter.DecodeStream(v)
		if err != nil {
			return nil, fmt.Errorf("failed to decode XFA stream: %w", err)
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

		data, err := filter.DecodeStream(stream)
		if err != nil {
			continue
		}

		buf = append(buf, data...)
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
			return textBuf.String(), nil
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
