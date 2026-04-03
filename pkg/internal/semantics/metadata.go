package semantics

import (
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"time"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
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
func getXMPMetadata(ctx context.Context, doc *document.Document) map[string]string {
	rootObj, ok := doc.Index.Trailer["Root"]
	if !ok {
		return nil
	}
	catalog, err := document.ResolveDict(ctx, doc, rootObj)
	if err != nil {
		return nil
	}
	if catalog == nil {
		return nil
	}
	metaRef, ok := catalog["Metadata"]
	if !ok {
		return nil
	}
	stream, err := document.ResolveStream(ctx, doc, metaRef)
	if err != nil {
		return nil
	}
	if stream.Dict == nil {
		return nil
	}

	decoded, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		return nil
	}

	return parseXMP(decoded)
}

// getInfoMetadata extracts the legacy /Info dictionary metadata.
func getInfoMetadata(ctx context.Context, doc *document.Document) map[string]string {
	infoObj, ok := doc.Index.Trailer["Info"]
	if !ok {
		return nil
	}
	infoDict, err := document.ResolveDict(ctx, doc, infoObj)
	if err != nil {
		return nil
	}
	if infoDict == nil {
		return nil
	}

	metadata := make(map[string]string)
	keys := []string{"Title", "Author", "Subject", "Keywords", "Creator", "Producer", "CreationDate", "ModDate"}
	for _, key := range keys {
		if valObj, ok := infoDict[ast.PdfName(key)]; ok {
			strVal, err := document.ResolveString(ctx, doc, valObj)
			if err != nil {
				doc.Logger.Debug("resolving info dict value", slog.String("key", key), slog.Any("err", err))
			}
			if strVal != "" {
				metadata[key] = document.DecodePdfString(strVal)
			}
		}
	}
	return metadata
}

// GetMetadata extracts document-level metadata. It first reads the legacy /Info
// dictionary, then overlays any XMP metadata from the catalog's /Metadata stream.
// XMP values take precedence over /Info values, following the PDF 2.0 standard.
func GetMetadata(ctx context.Context, doc *document.Document) (map[string]string, error) {
	info := getInfoMetadata(ctx, doc)
	xmp := getXMPMetadata(ctx, doc)

	metadata := make(map[string]string)

	// Start with legacy /Info values.
	maps.Copy(metadata, info)

	// Overlay XMP values — XMP takes precedence per PDF 2.0.
	// Exception: for date fields, keep the most recent date since many
	// PDF tools update the Info dict without updating XMP.
	for k, v := range xmp {
		if (k == "CreationDate" || k == "ModDate") && metadata[k] != "" {
			metadata[k] = newerDate(metadata[k], v)
		} else {
			metadata[k] = v
		}
	}

	// Catalog /Lang: the document-level language tag (per ISO 32000-2, 14.9.2).
	if _, hasLang := metadata["Language"]; !hasLang {
		if lang := getCatalogLang(ctx, doc); lang != "" {
			metadata["Language"] = lang
		}
	}

	if len(metadata) == 0 {
		return nil, fmt.Errorf("no metadata found (neither /Info dictionary nor XMP stream)")
	}

	return metadata, nil
}

// newerDate returns whichever of two date strings represents the more recent
// time. Falls back to b if parsing fails.
func newerDate(a, b string) string {
	ta, okA := parseAnyDate(a)
	tb, okB := parseAnyDate(b)
	if okA && okB {
		if ta.After(tb) {
			return a
		}
		return b
	}
	if okA {
		return a
	}
	return b
}

// parseAnyDate attempts to parse a PDF date string ("D:YYYYMMDD..." or ISO 8601).
func parseAnyDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	raw := strings.TrimPrefix(s, "D:")
	raw = strings.ReplaceAll(raw, "'", "")

	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05-07:00",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
		"20060102150405-0700",
		"20060102150405Z0700",
		"20060102150405Z",
		"20060102150405",
		"200601021504",
		"2006010215",
		"20060102",
	} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, true
		}
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// getCatalogLang reads the /Lang entry from the document catalog.
func getCatalogLang(ctx context.Context, doc *document.Document) string {
	rootObj, ok := doc.Index.Trailer["Root"]
	if !ok {
		return ""
	}
	catalog, err := document.ResolveDict(ctx, doc, rootObj)
	if err != nil || catalog == nil {
		return ""
	}
	langObj, ok := catalog["Lang"]
	if !ok {
		return ""
	}
	lang, err := document.ResolveString(ctx, doc, langObj)
	if err != nil {
		return ""
	}
	return document.DecodePdfString(lang)
}
