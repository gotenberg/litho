package compliance

import (
	"bytes"
	"encoding/xml"
	"fmt"

	"github.com/gotenberg/litho/pkg/models"
)

// xmpStrictnessLevel controls how strictly container types are validated.
type xmpStrictnessLevel int

const (
	// strictCore enforces full container type validation for XMP core,
	// Dublin Core, PDF, and pdfaid namespaces.
	strictCore xmpStrictnessLevel = iota

	// lenientApp checks property names but does not enforce container types
	// for Adobe application namespaces (photoshop, tiff, exif, crs).
	lenientApp
)

// namespaceStrictness maps namespace URIs to their validation strictness.
var namespaceStrictness = map[string]xmpStrictnessLevel{
	// Strict: XMP core schemas.
	"http://purl.org/dc/elements/1.1/":    strictCore,
	"http://ns.adobe.com/xap/1.0/":        strictCore,
	"http://ns.adobe.com/xap/1.0/mm/":     strictCore,
	"http://ns.adobe.com/xap/1.0/rights/": strictCore,
	"http://ns.adobe.com/pdf/1.3/":        strictCore,
	"http://www.aiim.org/pdfa/ns/id/":     strictCore,

	// Lenient: Adobe application namespaces.
	"http://ns.adobe.com/photoshop/1.0/":           lenientApp,
	"http://ns.adobe.com/exif/1.0/":                lenientApp,
	"http://ns.adobe.com/exif/1.0/aux/":            lenientApp,
	"http://ns.adobe.com/tiff/1.0/":                lenientApp,
	"http://ns.adobe.com/camera-raw-settings/1.0/": lenientApp,
	"http://ns.adobe.com/xmp/1.0/DynamicMedia/":    lenientApp,
}

// checkXMPContainerTypes validates that XMP properties use the correct
// container types (Bag/Seq/Alt/Simple) per their schema definitions.
// Only strict-tier namespaces enforce container type constraints.
func checkXMPContainerTypes(data []byte) []error {
	properties := extractXMPContainerInfo(data)
	var errs []error

	for ns, props := range properties {
		strictness, hasStrictness := namespaceStrictness[ns]
		if !hasStrictness {
			continue // Unknown namespace — skip container validation.
		}
		if strictness != strictCore {
			continue // Lenient — property names only, no container enforcement.
		}
		registry, hasRegistry := xmpPropertyRegistry[ns]
		if !hasRegistry {
			continue
		}
		for prop, observed := range props {
			expected, known := registry[prop]
			if !known || expected == xmpAny || observed == xmpAny {
				continue
			}
			// Only flag container vs container mismatches and simple vs container.
			if observed == expected {
				continue
			}
			if isContainerType(expected) && isContainerType(observed) {
				errs = append(errs, fmt.Errorf("%w: %s:%s expected %s, got %s",
					models.ErrXMPContainerTypeMismatch, nsPrefix(ns), prop,
					containerTypeName(expected), containerTypeName(observed)))
			} else if isSimpleType(expected) && isContainerType(observed) {
				errs = append(errs, fmt.Errorf("%w: %s:%s expected simple, got %s",
					models.ErrXMPContainerTypeMismatch, nsPrefix(ns), prop,
					containerTypeName(observed)))
			} else if isContainerType(expected) && isSimpleType(observed) {
				errs = append(errs, fmt.Errorf("%w: %s:%s expected %s, got simple",
					models.ErrXMPContainerTypeMismatch, nsPrefix(ns), prop,
					containerTypeName(expected)))
			}
		}
	}
	return errs
}

// containerTypeName returns a human-readable name for a container type.
func containerTypeName(t xmpType) string {
	switch t {
	case xmpAlt:
		return "Alt"
	case xmpSeq:
		return "Seq"
	case xmpBag:
		return "Bag"
	case xmpText, xmpURI, xmpInteger, xmpReal, xmpBoolean, xmpDate, xmpStruct, xmpAny:
		return "Simple"
	default:
		return "Simple"
	}
}

// extractXMPContainerInfo parses XMP XML and returns the observed container
// types for each property, grouped by namespace.
func extractXMPContainerInfo(data []byte) map[string]map[string]xmpType {
	result := make(map[string]map[string]xmpType)

	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false

	const rdfNS = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"

	depth := 0
	inDescription := false
	var currentPropNS, currentPropLocal string

	recordType := func(ns, local string, t xmpType) {
		if result[ns] == nil {
			result[ns] = make(map[string]xmpType)
		}
		result[ns][local] = t
	}

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == rdfNS && t.Name.Local == "Description" {
				inDescription = true
				depth = 0
				// Attribute-form properties are simple type.
				for _, attr := range t.Attr {
					if attr.Name.Space != "" && attr.Name.Space != "xmlns" &&
						attr.Name.Space != rdfNS &&
						attr.Name.Space != "http://www.w3.org/XML/1998/namespace" {
						recordType(attr.Name.Space, attr.Name.Local, xmpText)
					}
				}
				continue
			}

			if inDescription {
				depth++
				if depth == 1 && t.Name.Space != "" && t.Name.Space != rdfNS {
					currentPropNS = t.Name.Space
					currentPropLocal = t.Name.Local
					// Default to simple text; container detection follows.
					recordType(t.Name.Space, t.Name.Local, xmpText)
				}
				if depth == 2 && currentPropNS != "" && t.Name.Space == rdfNS { //nolint:mnd // depth 2 = container wrapper
					switch t.Name.Local {
					case "Alt":
						recordType(currentPropNS, currentPropLocal, xmpAlt)
					case "Seq":
						recordType(currentPropNS, currentPropLocal, xmpSeq)
					case "Bag":
						recordType(currentPropNS, currentPropLocal, xmpBag)
					}
				}
			}

			// Extension schemas (pdfaExtension:schemas) are structural,
			// not properties — no container validation needed.

		case xml.EndElement:
			if t.Name.Space == rdfNS && t.Name.Local == "Description" {
				inDescription = false
				depth = 0
				currentPropNS = ""
				currentPropLocal = ""
			} else if inDescription {
				if depth == 1 {
					currentPropNS = ""
					currentPropLocal = ""
				}
				depth--
			}
		}
	}

	return result
}
