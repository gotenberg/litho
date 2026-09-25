package signature

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// withoutMetadata returns objects without the document metadata that an
// incremental update rewrote in place beside another change, and a note for
// MDPInfo when it could not be told apart. objects are all the objects
// appended after signing. It applies at every P.
//
// The document metadata is the trailer's information dictionary (ISO 32000-1,
// 14.3.3) and the XMP metadata stream the catalog's /Metadata names (14.3.2).
// Each is exempt when:
//
//   - it is rewritten in place: the current trailer and catalog name the
//     object the signed ones named, which the signed revision defines;
//   - the incremental update that rewrites it (7.5.6) also writes an object
//     other than a cross-reference section and the metadata, whose change is
//     judged by the policy as any other.
//
// Deliberate leniency: ISO 32000-1, 12.8.2.2, Table 254 invalidates every
// change beyond filling in forms, instantiating page templates and signing
// from P=2 up, and every change at P=1, and ISO 32000-2, 12.8.2.2, Table 257
// exempts only the updates that add validation data or document timestamps.
// Neither names the metadata, but the producers that make those permitted
// changes rewrite both the information dictionary and the XMP stream on each
// incremental save, the modification date and often the producer in each.
// iText does so on every save in append mode: its PdfDocument sets /ModDate
// when it opens a document for writing, and rewrites the XMP packet in its
// existing object when the document has one. The rule is the same at every P: an
// approval signature, a form fill or added validation data carries its
// metadata update with it. An update that changes only the metadata changes
// nothing a policy permits, so it stays a change, as does an update that
// points the trailer or the catalog at other metadata objects.
//
// # Corpus evidence
//
//   - pdfjs issue18801.pdf: each of the 14 form fills Acrobat saved after the
//     signature (Adobe XMP Core 5.6 to 9.1) rewrites /ModDate in the
//     information dictionary and the XMP packet, both in place.
//   - pdfjs issue17169.pdf: the AntennaHouse PDF Signature Module rewrites
//     /ModDate and the XMP packet with each of its three signatures.
//   - pdfjs issue14269.pdf: the Lacuna PKI SDK rewrites /Producer, /ModDate
//     and the XMP packet with its second signature, and /ModDate and the XMP
//     packet again with the update that adds the DSS.
//   - pdfjs issue8047.pdf, issue17332.pdf, xfa_issue13855.pdf and
//     xfa_issue14071.pdf: the updates that add a DSS, a seal or XFA form data
//     leave both untouched, and no update after a signature in the corpora
//     rewrites one of them without the other.
func withoutMetadata(ctx context.Context, doc *document.Document, signed *signedRevision, objects []appendedObject) (_ []appendedObject, note string) {
	infoNum := refNum(doc.Index.Trailer["Info"])
	var metadataNum int
	if catalog, err := document.ResolveCatalog(ctx, doc); err == nil {
		metadataNum = refNum(catalog.Dict["Metadata"])
	}
	rewritten := false
	for _, obj := range objects {
		rewritten = rewritten || (!obj.IsXRef && obj.ObjNum > 0 && (obj.ObjNum == infoNum || obj.ObjNum == metadataNum))
	}
	if !rewritten {
		return objects, ""
	}

	rev, err := signed.revision(ctx)
	if err != nil {
		return objects, fmt.Sprintf("; metadata not recognized: %v", err)
	}
	if refNum(rev.Index.Trailer["Info"]) != infoNum || !rev.DefinesObject(infoNum) {
		infoNum = 0
	}
	signedCatalog, err := document.ResolveCatalog(ctx, rev)
	if err != nil || refNum(signedCatalog.Dict["Metadata"]) != metadataNum || !rev.DefinesObject(metadataNum) {
		metadataNum = 0
	}
	isMetadata := func(obj appendedObject) bool {
		switch {
		case obj.ObjNum <= 0:
			return false
		case obj.ObjNum == infoNum:
			return !obj.IsStream && obj.Type == "" && obj.FT == "" && !obj.IsAnnot
		case obj.ObjNum == metadataNum:
			return obj.IsStream && (obj.Type == "" || obj.Type == "Metadata")
		}
		return false
	}

	// An update is known by where it ends, which every object it writes
	// records.
	changes := make(map[int64]bool)
	for _, obj := range objects {
		if !obj.IsXRef && !isMetadata(obj) {
			changes[obj.UpdateEnd] = true
		}
	}
	kept := make([]appendedObject, 0, len(objects))
	for _, obj := range objects {
		if isMetadata(obj) && changes[obj.UpdateEnd] {
			continue
		}
		kept = append(kept, obj)
	}
	return kept, ""
}

// refNum returns the object number obj references, or 0 when it is no
// reference.
func refNum(obj ast.PdfObject) int {
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		return ref.ObjectNum
	}
	return 0
}
