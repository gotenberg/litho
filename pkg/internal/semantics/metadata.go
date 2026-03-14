package semantics

import (
	"encoding/xml"
	"fmt"
	"maps"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter"
)

// xmpMeta is the top-level XMP wrapper element.
type xmpMeta struct {
	RDF rdfRoot `xml:"RDF"`
}

// rdfRoot holds the rdf:RDF element containing one or more descriptions.
type rdfRoot struct {
	Descriptions []rdfDescription `xml:"Description"`
}

// rdfDescription captures the most common XMP properties across the Dublin Core,
// PDF, and XMP Basic namespaces. Fields use the local element name so the
// standard encoding/xml decoder matches regardless of the namespace prefix
// actually used in the document.
type rdfDescription struct {
	// Dublin Core (dc:)
	Title   rdfAlt `xml:"title"`
	Creator rdfSeq `xml:"creator"`
	Subject rdfBag `xml:"subject"`
	Rights  rdfAlt `xml:"rights"`

	// PDF namespace (pdf:)
	Producer string `xml:"Producer"`
	Keywords string `xml:"Keywords"`

	// XMP Basic (xmp: / xap:)
	CreatorTool string `xml:"CreatorTool"`
	CreateDate  string `xml:"CreateDate"`
	ModifyDate  string `xml:"ModifyDate"`
}

// rdfAlt represents an rdf:Alt container (ordered alternatives).
type rdfAlt struct {
	Alt struct {
		Items []rdfLi `xml:"li"`
	} `xml:"Alt"`
}

// rdfSeq represents an rdf:Seq container (ordered list).
type rdfSeq struct {
	Seq struct {
		Items []rdfLi `xml:"li"`
	} `xml:"Seq"`
}

// rdfBag represents an rdf:Bag container (unordered set).
type rdfBag struct {
	Bag struct {
		Items []rdfLi `xml:"li"`
	} `xml:"Bag"`
}

// rdfLi represents a single rdf:li element.
type rdfLi struct {
	Value string `xml:",chardata"`
}

// firstAlt returns the first alternative from an rdf:Alt, or "".
func firstAlt(a rdfAlt) string {
	if len(a.Alt.Items) > 0 {
		return strings.TrimSpace(a.Alt.Items[0].Value)
	}
	return ""
}

// joinSeq joins an rdf:Seq into a single string separated by sep.
func joinSeq(s rdfSeq, sep string) string {
	parts := make([]string, 0, len(s.Seq.Items))
	for _, li := range s.Seq.Items {
		v := strings.TrimSpace(li.Value)
		if v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, sep)
}

// joinBag joins an rdf:Bag into a single string separated by sep.
func joinBag(b rdfBag, sep string) string {
	parts := make([]string, 0, len(b.Bag.Items))
	for _, li := range b.Bag.Items {
		v := strings.TrimSpace(li.Value)
		if v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, sep)
}

// parseXMP extracts metadata key/value pairs from raw XMP XML bytes.
func parseXMP(data []byte) map[string]string {
	var meta xmpMeta
	if err := xml.Unmarshal(data, &meta); err != nil {
		return nil
	}

	result := make(map[string]string)

	for _, desc := range meta.RDF.Descriptions {
		if v := firstAlt(desc.Title); v != "" {
			result["Title"] = v
		}
		if v := joinSeq(desc.Creator, ", "); v != "" {
			result["Author"] = v
		}
		if v := joinBag(desc.Subject, ", "); v != "" {
			result["Subject"] = v
		}
		if v := firstAlt(desc.Rights); v != "" {
			result["Rights"] = v
		}
		if desc.Producer != "" {
			result["Producer"] = desc.Producer
		}
		if desc.Keywords != "" {
			result["Keywords"] = desc.Keywords
		}
		if desc.CreatorTool != "" {
			result["Creator"] = desc.CreatorTool
		}
		if desc.CreateDate != "" {
			result["CreationDate"] = desc.CreateDate
		}
		if desc.ModifyDate != "" {
			result["ModDate"] = desc.ModifyDate
		}
	}

	return result
}

// getXMPMetadata attempts to extract XMP metadata from the document catalog.
func getXMPMetadata(doc *document.Document) map[string]string {
	rootObj, ok := doc.Index.Trailer["Root"]
	if !ok {
		return nil
	}
	catalogObj, err := document.ResolveObject(doc, rootObj)
	if err != nil {
		return nil
	}
	catalog, ok := catalogObj.(ast.PdfDict)
	if !ok {
		return nil
	}
	metaRef, ok := catalog["Metadata"]
	if !ok {
		return nil
	}
	metaObj, err := document.ResolveObject(doc, metaRef)
	if err != nil {
		return nil
	}
	stream, ok := metaObj.(ast.PdfStream)
	if !ok {
		return nil
	}

	decoded, err := filter.DecodeStream(stream)
	if err != nil {
		return nil
	}

	return parseXMP(decoded)
}

// getInfoMetadata extracts the legacy /Info dictionary metadata.
func getInfoMetadata(doc *document.Document) map[string]string {
	infoObj, ok := doc.Index.Trailer["Info"]
	if !ok {
		return nil
	}
	infoResolved, err := document.ResolveObject(doc, infoObj)
	if err != nil {
		return nil
	}
	infoDict, ok := infoResolved.(ast.PdfDict)
	if !ok {
		return nil
	}

	metadata := make(map[string]string)
	keys := []string{"Title", "Author", "Subject", "Keywords", "Creator", "Producer", "CreationDate"}
	for _, key := range keys {
		if valObj, ok := infoDict[ast.PdfName(key)]; ok {
			valResolved, _ := document.ResolveObject(doc, valObj)
			if strVal, ok := valResolved.(ast.PdfString); ok {
				metadata[key] = document.DecodePdfString(strVal)
			}
		}
	}
	return metadata
}

// GetMetadata extracts document-level metadata. It first reads the legacy /Info
// dictionary, then overlays any XMP metadata from the catalog's /Metadata stream.
// XMP values take precedence over /Info values, following the PDF 2.0 standard.
func GetMetadata(doc *document.Document) (map[string]string, error) {
	info := getInfoMetadata(doc)
	xmp := getXMPMetadata(doc)

	if info == nil && xmp == nil {
		return nil, fmt.Errorf("no metadata found (neither /Info dictionary nor XMP stream)")
	}

	metadata := make(map[string]string)

	// Start with legacy /Info values.
	maps.Copy(metadata, info)

	// Overlay XMP values — XMP takes precedence per PDF 2.0.
	maps.Copy(metadata, xmp)

	return metadata, nil
}
