package semantics

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
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

// resolveXMPMetadata attempts to extract XMP metadata from the document catalog.
// If the current catalog lacks a /Metadata entry (common after incremental
// updates), it searches for a Metadata stream in the xref object pool.
func resolveXMPMetadata(ctx context.Context, doc *document.Document) map[string]string {
	rootObj, ok := doc.Index.Trailer["Root"]
	if !ok {
		return nil
	}
	catalog, err := document.ResolveDict(ctx, doc, rootObj)
	if err != nil || catalog == nil {
		return nil
	}

	if metaRef, ok := catalog["Metadata"]; ok {
		if result := resolveXMPStream(ctx, doc, metaRef); result != nil {
			return result
		}
	}

	// Fallback: the current catalog may lack /Metadata after an incremental
	// update. Scan the raw file for XMP packets and parse the last one.
	return scanRawXMP(doc)
}

// scanRawXMP reads the underlying file directly to find XMP packets when the
// catalog's /Metadata reference is missing. Scans backwards from the end to
// find the last <?xpacket or <x:xmpmeta marker.
func scanRawXMP(doc *document.Document) map[string]string {
	// Read the file in chunks from the end looking for XMP markers.
	const chunkSize = 64 * 1024
	buf := make([]byte, chunkSize)
	marker := []byte("<x:xmpmeta")
	endMarker := []byte("</x:xmpmeta>")

	fileSize := doc.FileSize()
	r := doc.RawReader()
	for offset := fileSize - int64(chunkSize); ; offset -= int64(chunkSize) - 128 {
		if offset < 0 {
			offset = 0
		}
		readLen := min(int64(chunkSize), fileSize-offset)
		n, err := r.ReadAt(buf[:readLen], offset)
		if n == 0 {
			break
		}
		if err != nil && err != io.EOF {
			break
		}
		chunk := buf[:n]

		// Find last <x:xmpmeta in this chunk.
		idx := bytes.LastIndex(chunk, marker)
		if idx == -1 {
			if offset == 0 {
				break
			}
			continue
		}

		// Find the closing </x:xmpmeta> after it.
		absStart := offset + int64(idx)
		// Read enough for the XMP packet (typically < 16KB).
		xmpBuf := make([]byte, min(32*1024, fileSize-absStart))
		n2, _ := r.ReadAt(xmpBuf, absStart)
		xmpData := xmpBuf[:n2]

		endIdx := bytes.Index(xmpData, endMarker)
		if endIdx == -1 {
			break
		}
		xmpData = xmpData[:endIdx+len(endMarker)]
		return parseXMP(xmpData)
	}
	return nil
}

func resolveXMPStream(ctx context.Context, doc *document.Document, ref ast.PdfObject) map[string]string {
	stream, err := document.ResolveStream(ctx, doc, ref)
	if err != nil || stream.Dict == nil {
		return nil
	}
	decoded, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		return nil
	}
	return parseXMP(decoded)
}

// XMPOpen returns a lazy Open function for the catalog /Metadata XMP stream.
// Returns nil if no XMP metadata stream is present. The stream data is not
// read until the caller invokes the returned function.
func XMPOpen(ctx context.Context, doc *document.Document) (func(context.Context) (io.ReadCloser, error), error) {
	rootObj, ok := doc.Index.Trailer["Root"]
	if !ok {
		return nil, nil
	}
	catalog, err := document.ResolveDict(ctx, doc, rootObj)
	if err != nil || catalog == nil {
		return nil, nil
	}

	metaRef, ok := catalog["Metadata"]
	if !ok {
		return nil, nil
	}

	stream, err := document.ResolveStream(ctx, doc, metaRef)
	if err != nil || stream.Dict == nil {
		return nil, nil
	}
	return doc.OpenFunc(stream), nil
}

// resolveInfoMetadata extracts the legacy /Info dictionary metadata.
func resolveInfoMetadata(ctx context.Context, doc *document.Document) map[string]string {
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

// Metadata extracts document-level metadata. It first reads the legacy /Info
// dictionary, then overlays any XMP metadata from the catalog's /Metadata stream.
// XMP values take precedence over /Info values, following the PDF 2.0 standard.
func Metadata(ctx context.Context, doc *document.Document) (map[string]string, error) {
	info := resolveInfoMetadata(ctx, doc)
	xmp := resolveXMPMetadata(ctx, doc)

	metadata := make(map[string]string)

	// Start with legacy /Info values.
	maps.Copy(metadata, info)

	// Overlay XMP values. XMP takes precedence per PDF 2.0 for fields that
	// are absent from /Info, but Info values are authoritative when
	// present. Producers that apply incremental saves update the /Info
	// dict consistently while leaving the XMP stream stale, so trusting
	// XMP on fields that /Info already defines causes stale dates,
	// producer strings, and titles to surface in the output.
	infoAuthoritative := map[string]bool{
		"Producer": true, "Creator": true, "Author": true,
		"Title": true, "Subject": true, "Keywords": true,
		"CreationDate": true, "ModDate": true,
	}
	for k, v := range xmp {
		if infoAuthoritative[k] && metadata[k] != "" {
			continue
		}
		metadata[k] = v
	}

	// Scan the raw file for Info fields that may exist in older Info
	// dict versions. Incremental updates occasionally point /Info at a
	// stale object (e.g. a number left by a rewriter) or drop fields
	// entirely; the raw scan recovers Title, Creator, Producer,
	// Keywords, CreationDate, and ModDate from the bytes of older Info
	// dicts when the in-memory metadata map is missing them.
	scanRawInfoFields(ctx, doc, metadata)

	// Catalog /Lang: the document-level language tag (per ISO 32000-2, 14.9.2).
	if _, hasLang := metadata["Language"]; !hasLang {
		if lang := resolveCatalogLang(ctx, doc); lang != "" {
			metadata["Language"] = lang
		}
	}

	if len(metadata) == 0 {
		return nil, fmt.Errorf("no metadata found (neither /Info dictionary nor XMP stream)")
	}

	return metadata, nil
}

// scanRawInfoFields scans the raw PDF bytes for Info-dict field
// values and populates [metadata] with recoveries for fields the
// in-memory Info resolution could not supply. /ModDate keeps its
// previous "newest wins" semantics; the remaining fields are filled
// only when missing so authoritative values stay intact. The scan
// runs against the entire file so incremental updates whose newest
// trailer points at a stale or non-dict /Info can still expose
// earlier revisions' values.
func scanRawInfoFields(ctx context.Context, doc *document.Document, metadata map[string]string) {
	fileSize := doc.FileSize()
	if fileSize > 10*1024*1024 {
		return
	}
	buf := make([]byte, fileSize)
	n, err := doc.RawReader().ReadAt(buf, 0)
	if n == 0 {
		return
	}
	if err != nil && err != io.EOF {
		return
	}
	raw := buf[:n]

	scanRawInfoField(ctx, raw, "ModDate", func(v string) {
		metadata["ModDate"] = newerDate(metadata["ModDate"], v)
	})
	fillIfMissing := func(key string) {
		if ctx.Err() != nil || metadata[key] != "" {
			return
		}
		scanRawInfoField(ctx, raw, key, func(v string) {
			if v != "" && metadata[key] == "" {
				metadata[key] = v
			}
		})
	}
	fillIfMissing("Title")
	fillIfMissing("Author")
	fillIfMissing("Subject")
	fillIfMissing("Keywords")
	fillIfMissing("Creator")
	fillIfMissing("Producer")
	fillIfMissing("CreationDate")
}

// scanRawInfoField walks every occurrence of "/<key>" in raw that sits
// inside an Info-dict-like context (neighbouring /Producer, /Author, or
// /Creator keys) and calls visit with the decoded string value. Both
// literal "(...)" and hex "<...>" syntaxes are supported. The visit
// callback is responsible for deciding how to combine multiple
// recoveries (newest wins for dates, first non-empty for strings).
func scanRawInfoField(ctx context.Context, raw []byte, key string, visit func(string)) {
	pattern := []byte("/" + key)
	pos := 0
	for {
		if ctx.Err() != nil {
			return
		}
		idx := bytes.Index(raw[pos:], pattern)
		if idx == -1 {
			return
		}
		absIdx := pos + idx
		pos = absIdx + len(pattern)
		// Reject hits where the next byte continues a longer name
		// (e.g. "/ModDate" inside "/ModDateOther" would be spurious).
		if pos < len(raw) && isNameContinuation(raw[pos]) {
			continue
		}
		// The sibling-key check is broad enough to admit Info dicts
		// whose other entries have been stripped; tightening it would
		// miss legitimate values on minimalist producers.
		contextStart := max(0, absIdx-500)
		context := raw[contextStart:min(len(raw), absIdx+500)]
		hasSibling := bytes.Contains(context, []byte("/Producer")) ||
			bytes.Contains(context, []byte("/Author")) ||
			bytes.Contains(context, []byte("/Creator")) ||
			bytes.Contains(context, []byte("/Title")) ||
			bytes.Contains(context, []byte("/CreationDate"))
		if !hasSibling {
			continue
		}
		// Extract the string value.
		after := raw[pos:]
		i := 0
		for i < len(after) && (after[i] == ' ' || after[i] == '\n' || after[i] == '\r' || after[i] == '\t') {
			i++
		}
		if i >= len(after) {
			continue
		}
		switch after[i] {
		case '(':
			end := findBalancedParen(after[i+1:])
			if end == -1 {
				continue
			}
			visit(document.DecodePdfString(ast.PdfString(unescapePdfLiteralString(after[i+1 : i+1+end]))))
		case '<':
			end := bytes.IndexByte(after[i+1:], '>')
			if end == -1 {
				continue
			}
			decoded, ok := decodeHexString(after[i+1 : i+1+end])
			if !ok {
				continue
			}
			visit(document.DecodePdfString(ast.PdfString(decoded)))
		}
	}
}

// isNameContinuation reports whether a byte is a valid continuation of
// a PDF name token. Names terminate at whitespace, delimiters, or the
// end of the stream; anything else means the match is part of a longer
// name and should be rejected.
func isNameContinuation(b byte) bool {
	switch b {
	case ' ', '\t', '\r', '\n', '\f', 0x00,
		'(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return false
	}
	return true
}

// findBalancedParen returns the offset of the closing ')' that balances
// the implicit opening paren before the first character of s, or -1 when
// the string is unterminated. Backslash escapes are skipped so "\)" does
// not count as a closer.
func findBalancedParen(s []byte) int {
	depth := 1
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// unescapePdfLiteralString decodes the standard literal-string escape
// sequences (\n, \r, \t, \b, \f, \(, \), \\, octal) into their raw byte
// values. [document.DecodePdfString] handles the higher-level UTF-16BE
// conversion afterward.
func unescapePdfLiteralString(s []byte) []byte {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			out = append(out, s[i])
			continue
		}
		c := s[i+1]
		switch c {
		case 'n':
			out = append(out, '\n')
			i++
		case 'r':
			out = append(out, '\r')
			i++
		case 't':
			out = append(out, '\t')
			i++
		case 'b':
			out = append(out, '\b')
			i++
		case 'f':
			out = append(out, '\f')
			i++
		case '(', ')', '\\':
			out = append(out, c)
			i++
		case '\n':
			i++
		case '\r':
			if i+2 < len(s) && s[i+2] == '\n' {
				i++
			}
			i++
		default:
			if c >= '0' && c <= '7' {
				num := int(c - '0')
				consumed := 1
				for consumed < 3 && i+1+consumed < len(s) {
					d := s[i+1+consumed]
					if d < '0' || d > '7' {
						break
					}
					num = num*8 + int(d-'0')
					consumed++
				}
				out = append(out, byte(num&0xff))
				i += consumed
			} else {
				out = append(out, c)
				i++
			}
		}
	}
	return out
}

// decodeHexString converts a PDF hex string body (the bytes between "<"
// and ">") to raw bytes. Whitespace is ignored and an unpaired trailing
// nibble is treated as if followed by '0', per ISO 32000-1 §7.3.4.3.
// Returns false when a non-hex, non-whitespace byte is encountered.
func decodeHexString(s []byte) ([]byte, bool) {
	out := make([]byte, 0, len(s)/2)
	var hi byte
	haveHi := false
	for _, c := range s {
		var nibble byte
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f':
			continue
		case c >= '0' && c <= '9':
			nibble = c - '0'
		case c >= 'A' && c <= 'F':
			nibble = c - 'A' + 10
		case c >= 'a' && c <= 'f':
			nibble = c - 'a' + 10
		default:
			return nil, false
		}
		if !haveHi {
			hi = nibble
			haveHi = true
			continue
		}
		out = append(out, hi<<4|nibble)
		haveHi = false
	}
	if haveHi {
		out = append(out, hi<<4)
	}
	return out, true
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
	// Strip redundant "Z0000" suffix (some producers emit Z00'00').
	if idx := strings.Index(raw, "Z"); idx != -1 && idx == len(raw)-5 {
		raw = raw[:idx+1]
	}

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

// resolveCatalogLang reads the /Lang entry from the document catalog.
func resolveCatalogLang(ctx context.Context, doc *document.Document) string {
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
