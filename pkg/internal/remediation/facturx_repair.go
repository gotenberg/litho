package remediation

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/models"
)

// hybridInvoiceName describes one hybrid-invoice convention litho can
// repair: the mandated embedded file name and its /AFRelationship.
// Factur-X 1.0.07 §5.2 mandates factur-x.xml with Data; ZUGFeRD 2.0
// used zugferd-invoice.xml with Alternative; ZUGFeRD 1.0 used
// ZUGFeRD-invoice.xml with Alternative. The list mirrors the variants
// the compliance side validates (internal/compliance/facturx.go).
type hybridInvoiceName struct {
	fileName     string
	relationship ast.PdfName
}

//nolint:gochecknoglobals // static spec table
var hybridInvoiceNames = []hybridInvoiceName{
	{"factur-x.xml", "Data"},
	{"zugferd-invoice.xml", "Alternative"},
	{"ZUGFeRD-invoice.xml", "Alternative"},
}

// RepairHybridInvoice fixes the PDF-container half of a received hybrid
// invoice in place: catalog /AF membership, the filespec
// /AFRelationship, and the embedded stream /Subtype. The invoice XML
// payload is business data and is never touched; XSD or Schematron
// violations inside it stay residual. Returns the fix descriptions, or
// nil when no known invoice attachment exists.
func RepairHybridInvoice(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator) ([]string, error) {
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return nil, fmt.Errorf("repair hybrid invoice: trailer missing /Root")
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, fmt.Errorf("repair hybrid invoice: resolving catalog")
	}

	name, specRef, specDict := locateInvoiceAttachment(ctx, alloc, doc, catalogDict)
	if specDict == nil {
		return nil, nil
	}

	// Non-nil from here on: a nil slice means "no invoice attachment",
	// an empty one "invoice found, container already clean".
	fixes := make([]string, 0, 4)
	newSpec := make(ast.PdfDict, len(specDict)+1)
	maps.Copy(newSpec, specDict)
	dirty := false

	// /AFRelationship per the variant's convention (ISO 32000-2 §14.13;
	// Factur-X 1.0.07 §5.2 mandates Data, ZUGFeRD 1.0/2.0 Alternative).
	if got, _ := document.ResolveName(ctx, doc, specDict["AFRelationship"]); got != name.relationship {
		newSpec["AFRelationship"] = name.relationship
		dirty = true
		fixes = append(fixes, fmt.Sprintf("set invoice /AFRelationship to %s", string(name.relationship)))
	}

	// Filespecs must carry both /F and /UF (ISO 19005-3 §6.9).
	fVal, hasF := newSpec["F"]
	ufVal, hasUF := newSpec["UF"]
	if hasF != hasUF {
		if !hasUF {
			newSpec["UF"] = fVal
		} else {
			newSpec["F"] = ufVal
		}
		dirty = true
		fixes = append(fixes, "mirrored invoice filespec /F and /UF")
	}

	if dirty {
		alloc.AllocAt(specRef.ObjectNum, newSpec)
	}

	// Embedded stream /Subtype must be text/xml (Factur-X 1.0.07 §5.2).
	fixes = append(fixes, repairInvoiceStreamSubtype(ctx, alloc, doc, newSpec)...)

	// Catalog /AF must reference the invoice filespec (ISO 19005-3 §6.8,
	// Factur-X 1.0.07 §5.1).
	if added := ensureCatalogAF(ctx, doc, alloc, catalogRef, catalogDict, specRef); added {
		fixes = append(fixes, "added invoice filespec to catalog /AF")
	}

	// fx: XMP identification block (Factur-X 1.0.07 §6). Only the fx:
	// namespace conventions are synthesized; ZUGFeRD 1.0 used the zf:
	// namespace and stays residual.
	if name.fileName != "ZUGFeRD-invoice.xml" {
		fixes = append(fixes, repairInvoiceXMP(ctx, alloc, doc, catalogRef, catalogDict, name, newSpec)...)
	}

	return fixes, nil
}

// repairInvoiceXMP synthesizes or corrects the four fx: properties for a
// received invoice. DocumentFileName comes from the attachment itself,
// DocumentType and Version are spec-fixed, and ConformanceLevel is
// inferred from the CII guideline URN inside the invoice XML; when the
// URN is unknown the property is left absent rather than guessed.
func repairInvoiceXMP(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalogRef ast.PdfIndirectRef, catalogDict ast.PdfDict, name hybridInvoiceName, specDict ast.PdfDict) []string {
	metaObj, ok := catalogDict["Metadata"]
	if !ok {
		// Without a metadata stream the PDF/A baseline is broken anyway;
		// the XMP violation stays residual until that is repaired.
		return nil
	}
	xmp, err := readXMPStream(ctx, alloc, doc, metaObj)
	if err != nil || len(xmp) == 0 {
		doc.Logger.Debug("repair hybrid invoice: cannot read XMP, leaving fx properties", "err", err)
		return nil
	}

	level := inferFacturXLevel(ctx, alloc, doc, specDict)
	buf, fixes, err := patchFacturXXMP(xmp, level, name.fileName, "INVOICE")
	if err != nil {
		doc.Logger.Debug("repair hybrid invoice: fx XMP patch failed", "err", err)
		return nil
	}
	if bytes.Equal(buf, xmp) {
		return nil
	}

	xmpStream := ast.NewStream(ast.PdfDict{
		"Type":    ast.PdfName("Metadata"),
		"Subtype": ast.PdfName("XML"),
	}, buf)
	if ref, isRef := metaObj.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 {
		alloc.AllocAt(ref.ObjectNum, xmpStream)
	} else {
		newRef := alloc.Alloc(xmpStream)
		newCatalog := make(ast.PdfDict, len(catalogDict)+1)
		maps.Copy(newCatalog, catalogDict)
		newCatalog["Metadata"] = newRef
		alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
	}
	return fixes
}

// inferFacturXLevel maps the CII GuidelineSpecifiedDocumentContextParameter
// URN inside the embedded invoice to a Factur-X conformance level
// (Factur-X 1.0.07 §4.3 guideline identifiers). Returns "" when the
// invoice cannot be read or the URN is unknown.
func inferFacturXLevel(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, specDict ast.PdfDict) models.FacturXLevel {
	efDict, err := document.ResolveDict(ctx, doc, specDict["EF"])
	if err != nil || efDict == nil {
		return ""
	}
	streamRef, ok := efDict["F"].(ast.PdfIndirectRef)
	if !ok {
		return ""
	}
	stream, ok2 := resolveStreamFromAllocOrDoc(ctx, alloc, doc, streamRef, efDict["F"])
	if !ok2 {
		return ""
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		if data, err = stream.Bytes(ctx); err != nil {
			return ""
		}
	}
	urn := strings.ToLower(string(data))
	switch {
	case strings.Contains(urn, "xrechnung"):
		return models.FacturXXRechnung
	case strings.Contains(urn, "factur-x.eu:1p0:minimum"):
		return models.FacturXMinimum
	case strings.Contains(urn, "factur-x.eu:1p0:basicwl"):
		return models.FacturXBasicWL
	case strings.Contains(urn, "factur-x.eu:1p0:basic"):
		return models.FacturXBasic
	case strings.Contains(urn, "factur-x.eu:1p0:extended"):
		return models.FacturXExtended
	case strings.Contains(urn, "urn:cen.eu:en16931:2017"):
		return models.FacturXEN16931
	}
	return ""
}

// locateInvoiceAttachment finds the first embedded file whose name
// matches a known hybrid-invoice convention. It searches the
// EmbeddedFiles name tree, not just /AF: a missing /AF entry is one of
// the defects this repair exists to fix.
func locateInvoiceAttachment(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalogDict ast.PdfDict) (hybridInvoiceName, ast.PdfIndirectRef, ast.PdfDict) {
	namesObj, ok := catalogDict["Names"]
	if !ok {
		return hybridInvoiceName{}, ast.PdfIndirectRef{}, nil
	}
	namesDict, err := document.ResolveDict(ctx, doc, namesObj)
	if err != nil || namesDict == nil {
		return hybridInvoiceName{}, ast.PdfIndirectRef{}, nil
	}
	efObj, ok := namesDict["EmbeddedFiles"]
	if !ok {
		return hybridInvoiceName{}, ast.PdfIndirectRef{}, nil
	}
	var entries []mutations.EFEntry
	mutations.CollectEmbeddedFileEntries(ctx, alloc, doc, efObj, 0, &entries)
	for _, want := range hybridInvoiceNames {
		for _, e := range entries {
			if e.Name != want.fileName {
				continue
			}
			ref, ok := e.Ref.(ast.PdfIndirectRef)
			if !ok {
				continue
			}
			dict := resolveFromAllocOrDoc(ctx, alloc, doc, ref)
			if dict == nil {
				continue
			}
			return want, ref, dict
		}
	}
	return hybridInvoiceName{}, ast.PdfIndirectRef{}, nil
}

// repairInvoiceStreamSubtype rewrites the embedded invoice stream's
// /Subtype to text/xml when it differs. The stream body is preserved.
func repairInvoiceStreamSubtype(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, specDict ast.PdfDict) []string {
	efDict, err := document.ResolveDict(ctx, doc, specDict["EF"])
	if err != nil || efDict == nil {
		return nil
	}
	streamRef, ok := efDict["F"].(ast.PdfIndirectRef)
	if !ok {
		return nil
	}
	stream, ok2 := resolveStreamFromAllocOrDoc(ctx, alloc, doc, streamRef, efDict["F"])
	if !ok2 {
		return nil
	}
	subtype, _ := stream.Dict["Subtype"].(ast.PdfName)
	if subtype == "text/xml" {
		return nil
	}
	data, err := stream.Bytes(ctx)
	if err != nil {
		doc.Logger.Debug("repair hybrid invoice: cannot read invoice stream, keeping /Subtype", "err", err)
		return nil
	}
	newDict := make(ast.PdfDict, len(stream.Dict))
	maps.Copy(newDict, stream.Dict)
	newDict["Subtype"] = ast.PdfName("text/xml")
	alloc.AllocAt(streamRef.ObjectNum, ast.NewStream(newDict, data))
	return []string{"set invoice stream /Subtype to text/xml"}
}

// ensureCatalogAF adds specRef to the catalog /AF array when absent.
func ensureCatalogAF(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, catalogRef ast.PdfIndirectRef, catalogDict ast.PdfDict, specRef ast.PdfIndirectRef) bool {
	var afArr ast.PdfArray
	if existing, ok := catalogDict["AF"]; ok {
		if arr, err := document.ResolveArray(ctx, doc, existing); err == nil {
			afArr = arr
		}
	}
	for _, item := range afArr {
		if ref, ok := item.(ast.PdfIndirectRef); ok && ref == specRef {
			return false
		}
	}
	newCatalog := make(ast.PdfDict, len(catalogDict)+1)
	maps.Copy(newCatalog, catalogDict)
	newCatalog["AF"] = append(afArr, specRef)
	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
	return true
}
