package semantics

import (
	"context"
	"io"
	"log/slog"
	"regexp"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// maxPDFXPacketBytes bounds the XMP packet read while looking for the PDF/X
// identifier. Anti-DoS guard, not a spec limit: the packet length is taken from
// the file, so a crafted document could otherwise make this read without end.
// A real identification packet is a few kilobytes; a megabyte is far past any
// legitimate one while still reading the whole packet of every corpus fixture.
const maxPDFXPacketBytes = 1 << 20

// pdfxXMPIdentifier matches the pdfxid:GTS_PDFXVersion property in both forms
// XMP allows for a simple value: an attribute on rdf:Description, and a child
// element. Only the element form appears in the corpus, but both are legal
// (ISO 16684-1), and this mirrors the shape the PDF/A identification scan uses.
//
// The prefix is part of the pattern on purpose. A PDF/A extension schema
// DECLARES properties by name, so a document can carry
// <pdfaProperty:name>GTS_PDFXVersion</pdfaProperty:name> while claiming no
// PDF/X conformance at all. Matching the bare key reports a claim that is not
// there; anchoring on pdfxid: does not.
var pdfxXMPIdentifier = regexp.MustCompile(`pdfxid:GTS_PDFXVersion\s*(?:=\s*"([^"]*)"|>\s*([^<]*))`)

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
// 14.3.2), and it is the only packet consulted. Real files carry many others:
// issue14562.pdf keeps a pdfxid identifier in a packet hanging off a pattern
// object while its catalog packet declares nothing, and issue14297.pdf has no
// catalog /Metadata at all while two resource-level packets declare PDF/X-4.
// Neither file makes a document-level claim, so neither is reported as one. A
// byte scan of the whole file says otherwise and is wrong.
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
	m := pdfxXMPIdentifier.FindSubmatch(data)
	if m == nil {
		return ""
	}
	for _, group := range m[1:] {
		if level := pdfxLevel(string(group)); level != "" {
			return level
		}
	}
	return ""
}

// pdfxFromInfo reads the Info-dictionary identifier used by X-1a and X-3.
//
// /GTS_PDFXConformance is consulted first because it carries the LEVEL while
// /GTS_PDFXVersion carries the base: the corpus fixtures declaring X-1a pair
// version "PDF/X-1:2001" with conformance "PDF/X-1a:2001", and reading the
// version alone would lose the "a" that distinguishes the only X-1 level litho
// recognises.
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
