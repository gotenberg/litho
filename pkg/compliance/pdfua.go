package compliance

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// ValidatePDFUA parses a PDF from the given reader and audits it against a
// specific PDF/UA profile, returning all compliance violations found. An empty
// slice means the document passes.
func ValidatePDFUA(ctx context.Context, r io.ReaderAt, size int64, profile PDFUAProfile) ([]error, error) {
	doc, err := document.NewDocument(ctx, r, size)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PDF: %w", err)
	}
	return validatePDFUA(ctx, doc, profile), nil
}

// validatePDFUA is the internal implementation that operates on a parsed document.
func validatePDFUA(ctx context.Context, doc *document.Document, profile PDFUAProfile) []error {
	if profile != PDFUA1 && profile != PDFUA2 {
		return []error{fmt.Errorf("%w: %s", ErrInvalidProfile, profile)}
	}

	catalog, err := getCatalog(ctx, doc)
	if err != nil {
		return []error{err}
	}

	errs := make([]error, 0, 10) //nolint:mnd // multiple check categories below.

	// --- Base rules (both UA-1 and UA-2) ---
	errs = append(errs, checkLang(ctx, doc, catalog)...)
	errs = append(errs, checkStructTreeRoot(ctx, doc, catalog)...)
	errs = append(errs, checkMarkInfo(ctx, doc, catalog)...)
	errs = append(errs, checkXMPTitle(ctx, doc, catalog)...)
	errs = append(errs, checkFontsToUnicodeUA(ctx, doc, catalog)...)
	errs = append(errs, checkFormFieldTooltips(ctx, doc, catalog)...)
	errs = append(errs, checkImagesHaveAltText(ctx, doc, catalog)...)
	errs = append(errs, checkArtifactTagging(ctx, doc, catalog)...)

	// --- Profile-specific rules ---
	switch profile {
	case PDFUA1:
		errs = append(errs, checkStandardTags(ctx, doc, catalog)...)
	case PDFUA2:
		errs = append(errs, checkPDFVersionForUA2(ctx, doc, catalog)...)
		errs = append(errs, checkNamespaces(ctx, doc, catalog)...)
		errs = append(errs, checkLinkAnnotationNesting(ctx, doc, catalog)...)
		errs = append(errs, checkFormulaMathML(ctx, doc, catalog)...)
	}

	return errs
}

// checkLang verifies that the /Lang key is present in the document catalog.
func checkLang(_ context.Context, _ *document.Document, catalog ast.PdfDict) []error {
	if _, ok := catalog["Lang"]; !ok {
		return []error{ErrMissingLang}
	}
	return nil
}

// checkXMPTitle verifies that the document has XMP metadata containing a dc:title.
func checkXMPTitle(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	mdObj, ok := catalog["Metadata"]
	if !ok {
		return []error{ErrMissingXMPTitle}
	}
	mdRes, err := resolve(ctx, doc, mdObj)
	if err != nil {
		return []error{ErrMissingXMPTitle}
	}
	stream, ok := mdRes.(ast.PdfStream)
	if !ok {
		return []error{ErrMissingXMPTitle}
	}
	// Check raw XMP bytes for dc:title presence.
	if !bytes.Contains(stream.Data, []byte("dc:title")) {
		return []error{ErrMissingXMPTitle}
	}
	return nil
}

// checkFontsToUnicodeUA verifies that all fonts have /ToUnicode mappings
// for reliable text extraction, as required by PDF/UA.
func checkFontsToUnicodeUA(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	var errs []error
	fonts := collectFontDicts(ctx, doc, catalog)
	for name, fontDict := range fonts {
		if isStandard14Font(ctx, doc, fontDict) {
			continue
		}
		if !hasFontToUnicode(ctx, doc, fontDict) {
			errs = append(errs, fmt.Errorf("%w: %s", ErrFontMissingToUnicode, name))
		}
	}
	return errs
}

// checkFormFieldTooltips verifies that all interactive form fields have a /TU
// (tooltip) entry for screen reader accessibility.
func checkFormFieldTooltips(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	acroObj, ok := catalog["AcroForm"]
	if !ok {
		return nil
	}
	acroRes, err := resolve(ctx, doc, acroObj)
	if err != nil {
		return nil
	}
	acroDict, ok := acroRes.(ast.PdfDict)
	if !ok {
		return nil
	}
	fieldsObj, ok := acroDict["Fields"]
	if !ok {
		return nil
	}
	fieldsRes, err := resolve(ctx, doc, fieldsObj)
	if err != nil {
		return nil
	}
	fieldsArr, ok := fieldsRes.(ast.PdfArray)
	if !ok {
		return nil
	}
	var errs []error
	for _, fieldObj := range fieldsArr {
		checkFieldTooltip(ctx, doc, fieldObj, &errs)
	}
	return errs
}

func checkFieldTooltip(ctx context.Context, doc *document.Document, obj ast.PdfObject, errs *[]error) {
	res, err := resolve(ctx, doc, obj)
	if err != nil {
		return
	}
	dict, ok := res.(ast.PdfDict)
	if !ok {
		return
	}
	// Only check terminal fields (those with a /T key and no /Kids, or leaf widgets).
	if _, hasT := dict["T"]; hasT {
		if _, hasTU := dict["TU"]; !hasTU {
			*errs = append(*errs, ErrMissingFormFieldTooltip)
		}
	}
	// Recurse into child fields.
	if kidsObj, ok := dict["Kids"]; ok {
		kidsRes, _ := resolve(ctx, doc, kidsObj)
		if kidsArr, ok := kidsRes.(ast.PdfArray); ok {
			for _, kid := range kidsArr {
				checkFieldTooltip(ctx, doc, kid, errs)
			}
		}
	}
}

// checkImagesHaveAltText traverses the structure tree and verifies that every
// Figure element has an /Alt attribute.
func checkImagesHaveAltText(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	strObj, ok := catalog["StructTreeRoot"]
	if !ok {
		return []error{ErrMissingStructTreeRoot}
	}
	strRes, _ := resolve(ctx, doc, strObj)
	strDict, ok := strRes.(ast.PdfDict)
	if !ok {
		return []error{ErrMissingStructTreeRoot}
	}

	var errs []error
	kObj, ok := strDict["K"]
	if !ok {
		return nil
	}
	kRes, _ := resolve(ctx, doc, kObj)
	switch k := kRes.(type) {
	case ast.PdfArray:
		for _, kid := range k {
			checkStructElemAlt(ctx, doc, kid, &errs)
		}
	case ast.PdfDict:
		checkStructElemAlt(ctx, doc, k, &errs)
	}
	return errs
}

func checkStructElemAlt(ctx context.Context, doc *document.Document, obj ast.PdfObject, errs *[]error) {
	res, err := resolve(ctx, doc, obj)
	if err != nil {
		return
	}
	dict, ok := res.(ast.PdfDict)
	if !ok {
		return
	}

	// Check if this is a Figure element.
	if sObj, ok := dict["S"]; ok {
		sRes, _ := resolve(ctx, doc, sObj)
		if sName, ok := sRes.(ast.PdfName); ok && sName == "Figure" {
			if _, hasAlt := dict["Alt"]; !hasAlt {
				*errs = append(*errs, ErrImageMissingAltText)
			}
		}
	}

	// Recurse into children.
	kObj, ok := dict["K"]
	if !ok {
		return
	}
	kRes, _ := resolve(ctx, doc, kObj)
	switch k := kRes.(type) {
	case ast.PdfArray:
		for _, kid := range k {
			checkStructElemAlt(ctx, doc, kid, errs)
		}
	case ast.PdfDict:
		checkStructElemAlt(ctx, doc, k, errs)
	}
}

// checkArtifactTagging verifies that all content tagged with specific roles
// (headers, footers, page numbers) is marked as /Artifact in the structure tree.
// Per PDF/UA, non-meaningful repeating content must use the /Artifact tag.
func checkArtifactTagging(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	strObj, ok := catalog["StructTreeRoot"]
	if !ok {
		return nil // Already caught by checkStructTreeRoot.
	}
	strRes, _ := resolve(ctx, doc, strObj)
	strDict, ok := strRes.(ast.PdfDict)
	if !ok {
		return nil
	}

	var errs []error
	kObj, ok := strDict["K"]
	if !ok {
		return nil
	}
	kRes, _ := resolve(ctx, doc, kObj)
	switch k := kRes.(type) {
	case ast.PdfArray:
		for _, kid := range k {
			checkArtifactElem(ctx, doc, kid, &errs)
		}
	case ast.PdfDict:
		checkArtifactElem(ctx, doc, k, &errs)
	}
	return errs
}

// forbiddenStructTypes are structure types that should be tagged as /Artifact
// rather than appearing as semantic structure elements.
var forbiddenStructTypes = map[string]bool{
	"Header":     true,
	"Footer":     true,
	"Watermark":  true,
	"PageNum":    true,
	"Pagination": true,
}

func checkArtifactElem(ctx context.Context, doc *document.Document, obj ast.PdfObject, errs *[]error) {
	res, err := resolve(ctx, doc, obj)
	if err != nil {
		return
	}
	dict, ok := res.(ast.PdfDict)
	if !ok {
		return
	}

	if sObj, ok := dict["S"]; ok {
		sRes, _ := resolve(ctx, doc, sObj)
		if sName, ok := sRes.(ast.PdfName); ok {
			if forbiddenStructTypes[string(sName)] {
				*errs = append(*errs, fmt.Errorf("%w: /%s should be /Artifact", ErrArtifactTagMissing, sName))
			}
		}
	}

	kObj, ok := dict["K"]
	if !ok {
		return
	}
	kRes, _ := resolve(ctx, doc, kObj)
	switch k := kRes.(type) {
	case ast.PdfArray:
		for _, kid := range k {
			checkArtifactElem(ctx, doc, kid, errs)
		}
	case ast.PdfDict:
		checkArtifactElem(ctx, doc, k, errs)
	}
}

// --- UA-1 specific checks ---

// standardPDF17Tags are the standard structure element types defined in PDF 1.7
// (ISO 32000-1), which PDF/UA-1 mandates. Tags not in this set must have a
// /RoleMap entry mapping them to a standard type.
var standardPDF17Tags = map[string]bool{
	// Grouping elements.
	"Document": true, "Part": true, "Art": true, "Sect": true,
	"Div": true, "BlockQuote": true, "Caption": true,
	"TOC": true, "TOCI": true, "Index": true,
	"NonStruct": true, "Private": true,
	// Block-level structure elements.
	"H": true, "H1": true, "H2": true, "H3": true, "H4": true, "H5": true, "H6": true,
	"P": true, "L": true, "LI": true, "Lbl": true, "LBody": true,
	// Table elements.
	"Table": true, "TR": true, "TH": true, "TD": true, "THead": true, "TBody": true, "TFoot": true,
	// Inline-level structure elements.
	"Span": true, "Quote": true, "Note": true, "Reference": true,
	"BibEntry": true, "Code": true, "Link": true, "Annot": true,
	// Illustration elements.
	"Figure": true, "Formula": true, "Form": true,
	// Ruby/Warichu.
	"Ruby": true, "RB": true, "RT": true, "RP": true,
	"Warichu": true, "WT": true, "WP": true,
}

// checkStandardTags verifies that all structure element types are either standard
// PDF 1.7 tags or have a mapping in the /RoleMap dictionary.
func checkStandardTags(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	strObj, ok := catalog["StructTreeRoot"]
	if !ok {
		return nil // Already caught by checkStructTreeRoot.
	}
	strRes, _ := resolve(ctx, doc, strObj)
	strDict, ok := strRes.(ast.PdfDict)
	if !ok {
		return nil
	}

	// Build the role map.
	roleMap := make(map[string]bool)
	if rmObj, ok := strDict["RoleMap"]; ok {
		rmRes, _ := resolve(ctx, doc, rmObj)
		if rmDict, ok := rmRes.(ast.PdfDict); ok {
			for key := range rmDict {
				roleMap[string(key)] = true
			}
		}
	}

	var errs []error
	kObj, ok := strDict["K"]
	if !ok {
		return nil
	}
	kRes, _ := resolve(ctx, doc, kObj)
	switch k := kRes.(type) {
	case ast.PdfArray:
		for _, kid := range k {
			checkStructElemStandard(ctx, doc, kid, roleMap, &errs)
		}
	case ast.PdfDict:
		checkStructElemStandard(ctx, doc, k, roleMap, &errs)
	}
	return errs
}

func checkStructElemStandard(ctx context.Context, doc *document.Document, obj ast.PdfObject, roleMap map[string]bool, errs *[]error) {
	res, err := resolve(ctx, doc, obj)
	if err != nil {
		return
	}
	dict, ok := res.(ast.PdfDict)
	if !ok {
		return
	}

	if sObj, ok := dict["S"]; ok {
		sRes, _ := resolve(ctx, doc, sObj)
		if sName, ok := sRes.(ast.PdfName); ok {
			tag := string(sName)
			if !standardPDF17Tags[tag] && !roleMap[tag] {
				*errs = append(*errs, fmt.Errorf("%w: /%s", ErrNonStandardTag, tag))
			}
		}
	}

	// Recurse.
	kObj, ok := dict["K"]
	if !ok {
		return
	}
	kRes, _ := resolve(ctx, doc, kObj)
	switch k := kRes.(type) {
	case ast.PdfArray:
		for _, kid := range k {
			checkStructElemStandard(ctx, doc, kid, roleMap, errs)
		}
	case ast.PdfDict:
		checkStructElemStandard(ctx, doc, k, roleMap, errs)
	}
}

// --- UA-2 specific checks ---

// checkPDFVersionForUA2 verifies the document declares PDF version 2.0.
// The version is read from the catalog /Version key or from the PDF header.
func checkPDFVersionForUA2(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	// Check catalog /Version first (overrides header version per spec).
	if vObj, ok := catalog["Version"]; ok {
		vRes, _ := resolve(ctx, doc, vObj)
		if vName, ok := vRes.(ast.PdfName); ok {
			if strings.HasPrefix(string(vName), "2.") {
				return nil
			}
		}
	}
	// Fallback: read header bytes for %PDF-2.x.
	// ReadFileBytes requires a valid io.ReaderAt; guard against nil.
	headerBuf, err := safeReadHeader(doc)
	if err == nil && bytes.Contains(headerBuf, []byte("%PDF-2.")) {
		return nil
	}
	return []error{ErrInvalidVersionForUA2}
}

// safeReadHeader reads the first 16 bytes of the document, recovering from
// panics caused by a nil io.ReaderAt (e.g., in test mocks).
func safeReadHeader(doc *document.Document) (buf []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("cannot read header: %v", r)
		}
	}()
	return doc.ReadFileBytes(0, 16) //nolint:mnd // enough for "%PDF-2.0".
}

// checkNamespaces verifies that /StructTreeRoot contains a /Namespaces array
// and that each entry has a valid /NS URI.
func checkNamespaces(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	strObj, ok := catalog["StructTreeRoot"]
	if !ok {
		return nil // Already caught by checkStructTreeRoot.
	}
	strRes, _ := resolve(ctx, doc, strObj)
	strDict, ok := strRes.(ast.PdfDict)
	if !ok {
		return nil
	}

	nsObj, ok := strDict["Namespaces"]
	if !ok {
		return []error{ErrMissingNamespaces}
	}
	nsRes, _ := resolve(ctx, doc, nsObj)
	nsArr, ok := nsRes.(ast.PdfArray)
	if !ok || len(nsArr) == 0 {
		return []error{ErrMissingNamespaces}
	}

	var errs []error
	for _, nsEntry := range nsArr {
		entryRes, _ := resolve(ctx, doc, nsEntry)
		entryDict, ok := entryRes.(ast.PdfDict)
		if !ok {
			errs = append(errs, ErrInvalidStructureNamespace)
			continue
		}
		if _, ok := entryDict["NS"]; !ok {
			errs = append(errs, ErrInvalidStructureNamespace)
		}
	}
	return errs
}

// checkLinkAnnotationNesting verifies that /Link annotations on pages are
// nested within /Link structure elements in the structure tree.
func checkLinkAnnotationNesting(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	pages := collectPages(ctx, doc, catalog)
	hasLinkAnnot := false
	for _, page := range pages {
		annotsObj, ok := page["Annots"]
		if !ok {
			continue
		}
		annotsRes, _ := resolve(ctx, doc, annotsObj)
		annotsArr, ok := annotsRes.(ast.PdfArray)
		if !ok {
			continue
		}
		for _, annotObj := range annotsArr {
			annotRes, _ := resolve(ctx, doc, annotObj)
			annotDict, ok := annotRes.(ast.PdfDict)
			if !ok {
				continue
			}
			if subtypeObj, ok := annotDict["Subtype"]; ok {
				subtypeRes, _ := resolve(ctx, doc, subtypeObj)
				if sName, ok := subtypeRes.(ast.PdfName); ok && sName == "Link" {
					hasLinkAnnot = true
					// Check if the annotation has a /StructParent pointing to a Link struct elem.
					if _, ok := annotDict["StructParent"]; !ok {
						return []error{ErrLinkNotNested}
					}
				}
			}
		}
	}
	if !hasLinkAnnot {
		return nil
	}

	// Verify that at least one Link structure element exists in the struct tree.
	if !hasStructElemType(ctx, doc, catalog, "Link") {
		return []error{ErrLinkNotNested}
	}
	return nil
}

// hasStructElemType checks whether a specific structure element type exists in the tree.
func hasStructElemType(ctx context.Context, doc *document.Document, catalog ast.PdfDict, typeName string) bool {
	strObj, ok := catalog["StructTreeRoot"]
	if !ok {
		return false
	}
	strRes, _ := resolve(ctx, doc, strObj)
	strDict, ok := strRes.(ast.PdfDict)
	if !ok {
		return false
	}
	kObj, ok := strDict["K"]
	if !ok {
		return false
	}
	kRes, _ := resolve(ctx, doc, kObj)
	return searchStructElemType(ctx, doc, kRes, typeName)
}

func searchStructElemType(ctx context.Context, doc *document.Document, obj ast.PdfObject, typeName string) bool {
	switch v := obj.(type) {
	case ast.PdfArray:
		for _, kid := range v {
			kidRes, _ := resolve(ctx, doc, kid)
			if searchStructElemType(ctx, doc, kidRes, typeName) {
				return true
			}
		}
	case ast.PdfDict:
		if sObj, ok := v["S"]; ok {
			sRes, _ := resolve(ctx, doc, sObj)
			if sName, ok := sRes.(ast.PdfName); ok && string(sName) == typeName {
				return true
			}
		}
		if kObj, ok := v["K"]; ok {
			kRes, _ := resolve(ctx, doc, kObj)
			return searchStructElemType(ctx, doc, kRes, typeName)
		}
	}
	return false
}

// checkFormulaMathML verifies that /Formula structure elements have an /AF
// entry containing an associated MathML file.
func checkFormulaMathML(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	strObj, ok := catalog["StructTreeRoot"]
	if !ok {
		return nil
	}
	strRes, _ := resolve(ctx, doc, strObj)
	strDict, ok := strRes.(ast.PdfDict)
	if !ok {
		return nil
	}
	kObj, ok := strDict["K"]
	if !ok {
		return nil
	}
	kRes, _ := resolve(ctx, doc, kObj)

	var errs []error
	checkFormulaAF(ctx, doc, kRes, &errs)
	return errs
}

func checkFormulaAF(ctx context.Context, doc *document.Document, obj ast.PdfObject, errs *[]error) {
	switch v := obj.(type) {
	case ast.PdfArray:
		for _, kid := range v {
			kidRes, _ := resolve(ctx, doc, kid)
			checkFormulaAF(ctx, doc, kidRes, errs)
		}
	case ast.PdfDict:
		if sObj, ok := v["S"]; ok {
			sRes, _ := resolve(ctx, doc, sObj)
			if sName, ok := sRes.(ast.PdfName); ok && sName == "Formula" {
				if _, hasAF := v["AF"]; !hasAF {
					*errs = append(*errs, ErrFormulaMissingMathML)
				}
			}
		}
		if kObj, ok := v["K"]; ok {
			kRes, _ := resolve(ctx, doc, kObj)
			checkFormulaAF(ctx, doc, kRes, errs)
		}
	}
}
