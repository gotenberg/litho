package semantics

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"log/slog"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// maxPDFXPacketBytes bounds the XMP packet read while looking for the PDF/X
// identifier. Anti-DoS guard, not a spec limit: the packet length is taken from
// the file, so a crafted document could otherwise make this read without end.
// A real identification packet is a few kilobytes; a megabyte is far past any
// legitimate one.
//
// # Corpus evidence
//
//   - All corpora: a megabyte still reads the whole packet of every fixture.
const maxPDFXPacketBytes = 1 << 20

// pdfxIDNamespace is the namespace URI the PDF/X identification schema binds
// to the pdfxid prefix, under which PDF/X-4 and later declare conformance.
const pdfxIDNamespace = "http://www.npes.org/pdfx/ns/id/"

// DeclaredPDFX reports the PDF/X conformance level a document DECLARES, or the
// empty profile when it declares none.
//
// This is introspection, not a verdict: it reads an identifier and says what it
// found. It does not check the document against ISO 15930, and a returned
// profile says nothing about whether the file conforms to it.
//
// The identifier migrated location between generations (ISO 15930): X-1a and
// X-3 declare it in the Info dictionary as /GTS_PDFXVersion plus
// /GTS_PDFXConformance, while X-4 and later declare it only in XMP as
// pdfxid:GTS_PDFXVersion, where an Info key left behind is itself a
// non-conformance. XMP is therefore consulted first and wins: a file carrying
// both is an X-4+ document with a stale Info key, not an X-1a/X-3 document.
//
// Sub-levels are never guessed. A declaration of "PDF/X-4" is reported as
// [models.PDFX4] even though the file may really be X-4p: telling those apart
// needs the output intent's DestOutputProfile vs DestOutputProfileRef, which is
// a structural question this function does not ask. Reporting the declared
// level and stopping is the honest answer.
func DeclaredPDFX(ctx context.Context, doc *document.Document) models.PDFXProfile {
	if p := pdfxFromXMP(ctx, doc); p != "" {
		return p
	}
	return pdfxFromInfo(ctx, doc)
}

// pdfxFromXMP reads the pdfxid: identifier from the document's XMP packet.
//
// The CATALOG's /Metadata stream is the document's metadata (ISO 32000-1,
// 14.3.2), and it is the only packet consulted. Real files carry many others,
// some of them declaring PDF/X on a resource while the catalog packet declares
// nothing or is absent. Such a file makes no document-level claim, so it is not
// reported as one. A byte scan of the whole file says otherwise and is wrong.
//
// # Corpus evidence
//
//   - pdfjs issue14562.pdf: keeps a pdfxid identifier in a packet hanging off
//     a pattern object while its catalog packet declares nothing.
//   - pdfjs issue14297.pdf: has no catalog /Metadata at all while two
//     resource-level packets declare PDF/X-4.
func pdfxFromXMP(ctx context.Context, doc *document.Document) models.PDFXProfile {
	open, err := XMPOpen(ctx, doc)
	if err != nil || open == nil {
		if err != nil {
			doc.Logger.Debug("opening XMP for the PDF/X identifier", slog.Any("err", err))
		}
		return ""
	}
	rc, err := open(ctx)
	if err != nil || rc == nil {
		doc.Logger.Debug("reading XMP for the PDF/X identifier", slog.Any("err", err))
		return ""
	}
	defer rc.Close() //nolint:errcheck // read-only stream; a close failure cannot affect the value already parsed

	data, err := io.ReadAll(io.LimitReader(rc, maxPDFXPacketBytes))
	if err != nil {
		doc.Logger.Debug("reading XMP packet for the PDF/X identifier", slog.Any("err", err))
		return ""
	}
	for _, value := range pdfxXMPIdentifiers(ctx, data) {
		if level := pdfxLevel(value); level != "" {
			return level
		}
	}
	return ""
}

// pdfxXMPIdentifiers returns the values of the GTS_PDFXVersion properties of
// the PDF/X identification namespace in an XMP packet, in document order. XMP
// allows a simple value in both forms (ISO 16684-1): an attribute on
// rdf:Description and a child element.
//
// The property is found by namespace URI and local name, with the namespace
// declarations in scope where it is written: on rdf:Description, or on the
// property element itself, where pikepdf declares every namespace it writes.
// A literal "pdfxid:GTS_PDFXVersion>" scan met only the closing tag of the
// second shape and read no value. Keying on the namespace also keeps what a
// PDF/A extension schema DECLARES out: it can carry
// <pdfaProperty:name>GTS_PDFXVersion</pdfaProperty:name> while claiming no
// PDF/X conformance at all, and matching the bare key would report a claim
// that is not there.
//
// A packet that is not well-formed yields the values that precede the defect.
//
// # Corpus evidence
//
//   - All corpora: only the element form appears, every pdfxid prefix is
//     bound to this namespace, and none is declared on the property element.
func pdfxXMPIdentifiers(ctx context.Context, data []byte) []string {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	// The packet is read as the bytes it is: an encoding its header names
	// changes no ASCII identifier.
	dec.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }
	var out []string
	for n := 0; ; n++ {
		if n%100 == 0 && ctx.Err() != nil {
			return out
		}
		tok, err := dec.Token()
		if err != nil {
			return out
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		for _, a := range start.Attr {
			if a.Name.Space == pdfxIDNamespace && a.Name.Local == "GTS_PDFXVersion" {
				out = append(out, strings.TrimSpace(a.Value))
			}
		}
		if start.Name.Space != pdfxIDNamespace || start.Name.Local != "GTS_PDFXVersion" {
			continue
		}
		var text strings.Builder
		for depth := 1; depth > 0; {
			tok, err := dec.Token()
			if err != nil {
				return append(out, strings.TrimSpace(text.String()))
			}
			switch t := tok.(type) {
			case xml.StartElement:
				depth++
			case xml.EndElement:
				depth--
			case xml.CharData:
				if depth == 1 {
					text.Write(t)
				}
			}
		}
		out = append(out, strings.TrimSpace(text.String()))
	}
}

// pdfxFromInfo reads the Info-dictionary identifier used by X-1a and X-3.
//
// /GTS_PDFXConformance is consulted first because it carries the LEVEL while
// /GTS_PDFXVersion carries the base: an X-1a file pairs version
// "PDF/X-1:2001" with conformance "PDF/X-1a:2001", and reading the version
// alone would lose the "a" that distinguishes the only X-1 level litho
// recognises.
//
// # Corpus evidence
//
//   - All corpora: the fixtures declaring X-1a pair version "PDF/X-1:2001"
//     with conformance "PDF/X-1a:2001".
func pdfxFromInfo(ctx context.Context, doc *document.Document) models.PDFXProfile {
	infoObj, ok := doc.Index.Trailer["Info"]
	if !ok {
		return ""
	}
	infoDict, err := document.ResolveDict(ctx, doc, infoObj)
	if err != nil || infoDict == nil {
		doc.Logger.Debug("resolving /Info for the PDF/X identifier", slog.Any("err", err))
		return ""
	}
	for _, key := range []string{"GTS_PDFXConformance", "GTS_PDFXVersion"} {
		valObj, ok := infoDict[ast.PdfName(key)]
		if !ok {
			continue
		}
		raw, err := document.ResolveString(ctx, doc, valObj)
		if err != nil {
			doc.Logger.Debug("resolving a PDF/X identifier value",
				slog.String("key", key), slog.Any("err", err))
			continue
		}
		if level := pdfxLevel(document.DecodePdfString(raw)); level != "" {
			return level
		}
	}
	return ""
}

// pdfxLevel maps a declared identifier string to a known profile, or returns
// the empty profile when it names no level litho recognises.
//
// Identifiers carry the year of the part that defined them ("PDF/X-1a:2001",
// "PDF/X-3:2002", "PDF/X-1a:2003"), which distinguishes editions of one level
// rather than levels, so everything from the colon on is dropped. A bare
// "PDF/X-1" is deliberately NOT mapped: ISO 15930-1's X-1 is not among the ten
// live levels, and promoting it to X-1a would invent a claim the file does not
// make.
func pdfxLevel(declared string) models.PDFXProfile {
	value := strings.TrimSpace(declared)
	if idx := strings.IndexByte(value, ':'); idx >= 0 {
		value = strings.TrimSpace(value[:idx])
	}
	if value == "" {
		return ""
	}
	candidate := models.PDFXProfile(value)
	if candidate.IsValid() {
		return candidate
	}
	return ""
}
