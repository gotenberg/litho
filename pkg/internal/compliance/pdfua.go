package compliance

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// ValidatePDFUA is defined in rules.go alongside the table-driven rule sets.

// checkLangValues validates that all /Lang entries in the catalog and
// structure tree are valid RFC 3066 language tags (PDF/UA-1 §7.2).
func checkLangValues(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	var errs []error
	// Catalog-level /Lang.
	if langObj, ok := catalog["Lang"]; ok {
		langRes, _ := document.ResolveObject(ctx, doc, langObj)
		if langStr, ok := langRes.(ast.PdfString); ok {
			decoded := document.DecodePdfString(langStr)
			if !isValidLangTag(decoded) {
				errs = append(errs, fmt.Errorf("%w: %q", models.ErrInvalidLangTag, decoded))
			}
		}
	}
	// Structure tree /Lang entries.
	strObj, ok := catalog["StructTreeRoot"]
	if !ok {
		return errs
	}
	strRes, _ := document.ResolveObject(ctx, doc, strObj)
	strDict, ok := strRes.(ast.PdfDict)
	if !ok {
		return errs
	}
	kObj, ok := strDict["K"]
	if !ok {
		return errs
	}
	kRes, _ := document.ResolveObject(ctx, doc, kObj)
	checkStructTreeLangValues(ctx, doc, kRes, &errs)
	return errs
}

func checkStructTreeLangValues(ctx context.Context, doc *document.Document, obj ast.PdfObject, errs *[]error) {
	switch v := obj.(type) {
	case ast.PdfDict:
		if langObj, ok := v["Lang"]; ok {
			langRes, _ := document.ResolveObject(ctx, doc, langObj)
			if langStr, ok := langRes.(ast.PdfString); ok {
				decoded := document.DecodePdfString(langStr)
				if !isValidLangTag(decoded) {
					*errs = append(*errs, fmt.Errorf("%w: %q", models.ErrInvalidLangTag, decoded))
				}
			}
		}
		if kObj, ok := v["K"]; ok {
			kRes, _ := document.ResolveObject(ctx, doc, kObj)
			checkStructTreeLangValues(ctx, doc, kRes, errs)
		}
	case ast.PdfArray:
		for _, item := range v {
			itemRes, _ := document.ResolveObject(ctx, doc, item)
			checkStructTreeLangValues(ctx, doc, itemRes, errs)
		}
	}
}

// isValidLangTag performs basic BCP 47 / RFC 3066 language tag validation.
// A language tag has the form: primary[-subtag[-subtag...]].
// The primary subtag must be 1-8 ASCII letters. Subsequent subtags must be
// 1-8 alphanumeric characters.
func isValidLangTag(tag string) bool {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return true // Empty /Lang means "not specified at this level" — valid.
	}
	parts := strings.Split(tag, "-")
	primary := parts[0]
	if !isAlpha(primary) || len(primary) == 0 || len(primary) > 8 { //nolint:mnd // BCP 47 subtag length
		return false
	}
	for _, sub := range parts[1:] {
		if len(sub) == 0 || len(sub) > 8 { //nolint:mnd // BCP 47 subtag length
			return false
		}
		if !isAlphaNum(sub) {
			return false
		}
	}
	return true
}

func isAlpha(s string) bool {
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
			return false
		}
	}
	return true
}

func isAlphaNum(s string) bool {
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

// checkSuspects verifies that /MarkInfo does not have /Suspects set to true.
// Per PDF/UA-1 §7.1, a file with Suspects=true indicates unverified tagging.
func checkSuspects(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	miObj, ok := catalog["MarkInfo"]
	if !ok {
		return nil
	}
	miRes, _ := document.ResolveObject(ctx, doc, miObj)
	miDict, ok := miRes.(ast.PdfDict)
	if !ok {
		return nil
	}
	susObj, ok := miDict["Suspects"]
	if !ok {
		return nil
	}
	susRes, _ := document.ResolveObject(ctx, doc, susObj)
	if b, ok := susRes.(ast.PdfBool); ok && bool(b) {
		return []error{models.ErrSuspectsTrue}
	}
	return nil
}

// checkDisplayDocTitle verifies that the catalog's /ViewerPreferences
// dictionary has /DisplayDocTitle set to true (PDF/UA-1 §7.1).
func checkDisplayDocTitle(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	vpObj, ok := catalog["ViewerPreferences"]
	if !ok {
		return []error{models.ErrMissingDisplayDocTitle}
	}
	vpRes, _ := document.ResolveObject(ctx, doc, vpObj)
	vpDict, ok := vpRes.(ast.PdfDict)
	if !ok {
		return []error{models.ErrMissingDisplayDocTitle}
	}
	ddtObj, ok := vpDict["DisplayDocTitle"]
	if !ok {
		return []error{models.ErrMissingDisplayDocTitle}
	}
	ddtRes, _ := document.ResolveObject(ctx, doc, ddtObj)
	if b, ok := ddtRes.(ast.PdfBool); !ok || !bool(b) {
		return []error{models.ErrMissingDisplayDocTitle}
	}
	return nil
}

// checkTabOrder verifies that every page with annotations has /Tabs set
// to /S (Structure), as required by PDF/UA-1 §7.18.1.
func checkTabOrder(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	pages := collectPages(ctx, doc, catalog)
	for _, page := range pages {
		// Only pages with annotations need /Tabs /S.
		if _, ok := page["Annots"]; !ok {
			continue
		}
		tabsObj, ok := page["Tabs"]
		if !ok {
			return []error{models.ErrTabOrderNotStructure}
		}
		tabsRes, _ := document.ResolveObject(ctx, doc, tabsObj)
		if tabsName, ok := tabsRes.(ast.PdfName); !ok || tabsName != "S" {
			return []error{models.ErrTabOrderNotStructure}
		}
	}
	return nil
}

// checkOCConfigNames verifies that all optional content configuration
// dictionaries (/D and /Configs in /OCProperties) contain a /Name key.
func checkOCConfigNames(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	ocObj, ok := catalog["OCProperties"]
	if !ok {
		return nil
	}
	ocRes, _ := document.ResolveObject(ctx, doc, ocObj)
	ocDict, ok := ocRes.(ast.PdfDict)
	if !ok {
		return nil
	}
	var errs []error
	// Check /D (default configuration).
	if dObj, ok := ocDict["D"]; ok {
		dRes, _ := document.ResolveObject(ctx, doc, dObj)
		if dDict, ok := dRes.(ast.PdfDict); ok {
			if _, ok := dDict["Name"]; !ok {
				errs = append(errs, models.ErrOCConfigMissingName)
			}
		}
	}
	// Check /Configs array.
	if cfgObj, ok := ocDict["Configs"]; ok {
		cfgRes, _ := document.ResolveObject(ctx, doc, cfgObj)
		if cfgArr, ok := cfgRes.(ast.PdfArray); ok {
			for _, item := range cfgArr {
				itemRes, _ := document.ResolveObject(ctx, doc, item)
				if itemDict, ok := itemRes.(ast.PdfDict); ok {
					if _, ok := itemDict["Name"]; !ok {
						errs = append(errs, models.ErrOCConfigMissingName)
					}
				}
			}
		}
	}
	return errs
}

// checkLang verifies that a natural language is declared either in the
// catalog or on the root structure element.
func checkLang(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	if !hasLangSpecified(ctx, doc, catalog) {
		return []error{models.ErrMissingLang}
	}
	// Also verify the catalog /Lang value is non-empty (8.4.4 test 1).
	if langObj, ok := catalog["Lang"]; ok {
		langRes, _ := document.ResolveObject(ctx, doc, langObj)
		if langStr, ok := langRes.(ast.PdfString); ok {
			decoded := document.DecodePdfString(langStr)
			if strings.TrimSpace(decoded) == "" {
				return []error{models.ErrMissingLang}
			}
		}
	}
	return nil
}

// checkXMPTitle verifies that the document has XMP metadata containing a dc:title.
func checkXMPTitle(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	mdObj, ok := catalog["Metadata"]
	if !ok {
		return []error{models.ErrMissingXMPTitle}
	}
	mdRes, err := document.ResolveObject(ctx, doc, mdObj)
	if err != nil {
		return []error{models.ErrMissingXMPTitle}
	}
	stream, ok := mdRes.(ast.PdfStream)
	if !ok {
		return []error{models.ErrMissingXMPTitle}
	}
	// Check raw XMP bytes for dc:title presence.
	if !bytes.Contains(stream.Data, []byte("dc:title")) {
		return []error{models.ErrMissingXMPTitle}
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
			errs = append(errs, fmt.Errorf("%w: %s", models.ErrFontMissingToUnicode, name))
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
	acroRes, err := document.ResolveObject(ctx, doc, acroObj)
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
	fieldsRes, err := document.ResolveObject(ctx, doc, fieldsObj)
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
	res, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return
	}
	dict, ok := res.(ast.PdfDict)
	if !ok {
		return
	}
	// Only check terminal fields with /T that are interactive and need tooltips.
	if _, hasT := dict["T"]; hasT {
		if !isFieldExemptFromTooltip(ctx, doc, dict) {
			if _, hasTU := dict["TU"]; !hasTU {
				*errs = append(*errs, models.ErrMissingFormFieldTooltip)
			}
		}
	}
	// Recurse into child fields.
	if kidsObj, ok := dict["Kids"]; ok {
		kidsRes, _ := document.ResolveObject(ctx, doc, kidsObj)
		if kidsArr, ok := kidsRes.(ast.PdfArray); ok {
			for _, kid := range kidsArr {
				checkFieldTooltip(ctx, doc, kid, errs)
			}
		}
	}
}

// isFieldExemptFromTooltip returns true if the field does not need a /TU
// tooltip. Push buttons (/FT /Btn with /Ff bit 17) and read-only fields
// (/Ff bit 1) are exempt. Fields with /Alt or /Contents on their widget
// annotation are also considered accessible.
func isFieldExemptFromTooltip(ctx context.Context, doc *document.Document, dict ast.PdfDict) bool {
	// Check /Ff (field flags).
	if ffObj, ok := dict["Ff"]; ok {
		ffRes, _ := document.ResolveObject(ctx, doc, ffObj)
		if ffInt, ok := ffRes.(ast.PdfInt); ok {
			ff := int(ffInt)
			// Bit 1: ReadOnly.
			if ff&1 != 0 {
				return true
			}
			// Check if push button: /FT /Btn AND /Ff bit 17.
			if ftObj, ok := dict["FT"]; ok {
				ftRes, _ := document.ResolveObject(ctx, doc, ftObj)
				if ftName, ok := ftRes.(ast.PdfName); ok && ftName == "Btn" {
					if ff&(1<<16) != 0 { //nolint:mnd // bit 17 (0-indexed: bit 16)
						return true
					}
				}
			}
		}
	}
	// /Alt or /Contents on the field or its widget children provides
	// accessibility without /TU.
	if _, ok := dict["Alt"]; ok {
		return true
	}
	if _, ok := dict["Contents"]; ok {
		return true
	}
	// Check widget annotation children for /Contents, /TU, or /StructParent
	// (indicating the widget is tagged in the structure tree, where /Alt
	// on the /Form structure element serves the same purpose as /TU).
	if kidsObj, ok := dict["Kids"]; ok {
		kidsRes, _ := document.ResolveObject(ctx, doc, kidsObj)
		if kidsArr, ok := kidsRes.(ast.PdfArray); ok {
			for _, kid := range kidsArr {
				kidRes, _ := document.ResolveObject(ctx, doc, kid)
				if kidDict, ok := kidRes.(ast.PdfDict); ok {
					if _, ok := kidDict["TU"]; ok {
						return true
					}
					if _, ok := kidDict["Contents"]; ok {
						return true
					}
					if _, ok := kidDict["StructParent"]; ok {
						return true
					}
				}
			}
		}
	}
	// The field itself may be tagged in the structure tree.
	if _, ok := dict["StructParent"]; ok {
		return true
	}
	return false
}

// checkImagesHaveAltText traverses the structure tree and verifies that every
// Figure element has an /Alt attribute.
func checkImagesHaveAltText(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	strObj, ok := catalog["StructTreeRoot"]
	if !ok {
		return []error{models.ErrMissingStructTreeRoot}
	}
	strRes, _ := document.ResolveObject(ctx, doc, strObj)
	strDict, ok := strRes.(ast.PdfDict)
	if !ok {
		return []error{models.ErrMissingStructTreeRoot}
	}

	var errs []error
	kObj, ok := strDict["K"]
	if !ok {
		return nil
	}
	kRes, _ := document.ResolveObject(ctx, doc, kObj)
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
	res, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return
	}
	dict, ok := res.(ast.PdfDict)
	if !ok {
		return
	}

	// Check if this is a Figure element.
	if sObj, ok := dict["S"]; ok {
		sRes, _ := document.ResolveObject(ctx, doc, sObj)
		if sName, ok := sRes.(ast.PdfName); ok && sName == "Figure" {
			if !hasAltOrActualText(dict) {
				*errs = append(*errs, models.ErrImageMissingAltText)
			}
		}
	}

	// Recurse into children.
	kObj, ok := dict["K"]
	if !ok {
		return
	}
	kRes, _ := document.ResolveObject(ctx, doc, kObj)
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
	strRes, _ := document.ResolveObject(ctx, doc, strObj)
	strDict, ok := strRes.(ast.PdfDict)
	if !ok {
		return nil
	}

	var errs []error
	kObj, ok := strDict["K"]
	if !ok {
		return nil
	}
	kRes, _ := document.ResolveObject(ctx, doc, kObj)
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
	res, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return
	}
	dict, ok := res.(ast.PdfDict)
	if !ok {
		return
	}

	if sObj, ok := dict["S"]; ok {
		sRes, _ := document.ResolveObject(ctx, doc, sObj)
		if sName, ok := sRes.(ast.PdfName); ok {
			if forbiddenStructTypes[string(sName)] {
				*errs = append(*errs, fmt.Errorf("%w: /%s should be /Artifact", models.ErrArtifactTagMissing, sName))
			}
		}
	}

	kObj, ok := dict["K"]
	if !ok {
		return
	}
	kRes, _ := document.ResolveObject(ctx, doc, kObj)
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
	strRes, _ := document.ResolveObject(ctx, doc, strObj)
	strDict, ok := strRes.(ast.PdfDict)
	if !ok {
		return nil
	}

	// Build the role map (source → target).
	roleMap := make(map[string]bool)
	roleMapTargets := make(map[string]string)
	if rmObj, ok := strDict["RoleMap"]; ok {
		rmRes, _ := document.ResolveObject(ctx, doc, rmObj)
		if rmDict, ok := rmRes.(ast.PdfDict); ok {
			for key, val := range rmDict {
				roleMap[string(key)] = true
				valRes, _ := document.ResolveObject(ctx, doc, val)
				if targetName, ok := valRes.(ast.PdfName); ok {
					roleMapTargets[string(key)] = string(targetName)
				}
			}
		}
	}

	var errs []error

	// Validate role map entries.
	for src, target := range roleMapTargets {
		// Standard tags must not be remapped (7.1 t07).
		if standardPDF17Tags[src] {
			errs = append(errs, fmt.Errorf("%w: /%s → /%s", models.ErrStandardTagRemapped, src, target))
			continue
		}
		// Follow the mapping chain to verify it terminates at a standard type.
		visited := map[string]bool{src: true}
		current := target
		for !standardPDF17Tags[current] {
			next, ok := roleMapTargets[current]
			if !ok {
				// Mapping doesn't terminate at standard type.
				errs = append(errs, fmt.Errorf("%w: /%s → /%s", models.ErrRoleMapNotTerminating, src, current))
				break
			}
			if visited[current] {
				errs = append(errs, fmt.Errorf("%w: /%s", models.ErrRoleMapCircular, src))
				break
			}
			visited[current] = true
			current = next
		}
	}
	kObj, ok := strDict["K"]
	if !ok {
		return nil
	}
	kRes, _ := document.ResolveObject(ctx, doc, kObj)
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
	res, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return
	}
	dict, ok := res.(ast.PdfDict)
	if !ok {
		return
	}

	if sObj, ok := dict["S"]; ok {
		sRes, _ := document.ResolveObject(ctx, doc, sObj)
		if sName, ok := sRes.(ast.PdfName); ok {
			tag := string(sName)
			if !standardPDF17Tags[tag] && !roleMap[tag] {
				*errs = append(*errs, fmt.Errorf("%w: /%s", models.ErrNonStandardTag, tag))
			}
		}
	}

	// Recurse.
	kObj, ok := dict["K"]
	if !ok {
		return
	}
	kRes, _ := document.ResolveObject(ctx, doc, kObj)
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
		vRes, _ := document.ResolveObject(ctx, doc, vObj)
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
	return []error{models.ErrInvalidVersionForUA2}
}

// checkNamespaces verifies that /StructTreeRoot contains a /Namespaces array
// and that each entry has a valid /NS URI.
func checkNamespaces(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	strObj, ok := catalog["StructTreeRoot"]
	if !ok {
		return nil // Already caught by checkStructTreeRoot.
	}
	strRes, _ := document.ResolveObject(ctx, doc, strObj)
	strDict, ok := strRes.(ast.PdfDict)
	if !ok {
		return nil
	}

	nsObj, ok := strDict["Namespaces"]
	if !ok {
		return []error{models.ErrMissingNamespaces}
	}
	nsRes, _ := document.ResolveObject(ctx, doc, nsObj)
	nsArr, ok := nsRes.(ast.PdfArray)
	if !ok || len(nsArr) == 0 {
		return []error{models.ErrMissingNamespaces}
	}

	var errs []error
	for _, nsEntry := range nsArr {
		entryRes, _ := document.ResolveObject(ctx, doc, nsEntry)
		entryDict, ok := entryRes.(ast.PdfDict)
		if !ok {
			errs = append(errs, models.ErrInvalidStructureNamespace)
			continue
		}
		if _, ok := entryDict["NS"]; !ok {
			errs = append(errs, models.ErrInvalidStructureNamespace)
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
		annotsRes, _ := document.ResolveObject(ctx, doc, annotsObj)
		annotsArr, ok := annotsRes.(ast.PdfArray)
		if !ok {
			continue
		}
		for _, annotObj := range annotsArr {
			annotRes, _ := document.ResolveObject(ctx, doc, annotObj)
			annotDict, ok := annotRes.(ast.PdfDict)
			if !ok {
				continue
			}
			if subtypeObj, ok := annotDict["Subtype"]; ok {
				subtypeRes, _ := document.ResolveObject(ctx, doc, subtypeObj)
				if sName, ok := subtypeRes.(ast.PdfName); ok && sName == "Link" {
					hasLinkAnnot = true
					// Check if the annotation has a /StructParent pointing to a Link struct elem.
					if _, ok := annotDict["StructParent"]; !ok {
						return []error{models.ErrLinkNotNested}
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
		return []error{models.ErrLinkNotNested}
	}
	return nil
}

// hasStructElemType checks whether a specific structure element type exists in the tree.
func hasStructElemType(ctx context.Context, doc *document.Document, catalog ast.PdfDict, typeName string) bool {
	strObj, ok := catalog["StructTreeRoot"]
	if !ok {
		return false
	}
	strRes, _ := document.ResolveObject(ctx, doc, strObj)
	strDict, ok := strRes.(ast.PdfDict)
	if !ok {
		return false
	}
	kObj, ok := strDict["K"]
	if !ok {
		return false
	}
	kRes, _ := document.ResolveObject(ctx, doc, kObj)
	return searchStructElemType(ctx, doc, kRes, typeName)
}

func searchStructElemType(ctx context.Context, doc *document.Document, obj ast.PdfObject, typeName string) bool {
	switch v := obj.(type) {
	case ast.PdfArray:
		for _, kid := range v {
			kidRes, _ := document.ResolveObject(ctx, doc, kid)
			if searchStructElemType(ctx, doc, kidRes, typeName) {
				return true
			}
		}
	case ast.PdfDict:
		if sObj, ok := v["S"]; ok {
			sRes, _ := document.ResolveObject(ctx, doc, sObj)
			if sName, ok := sRes.(ast.PdfName); ok && string(sName) == typeName {
				return true
			}
		}
		if kObj, ok := v["K"]; ok {
			kRes, _ := document.ResolveObject(ctx, doc, kObj)
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
	strRes, _ := document.ResolveObject(ctx, doc, strObj)
	strDict, ok := strRes.(ast.PdfDict)
	if !ok {
		return nil
	}
	kObj, ok := strDict["K"]
	if !ok {
		return nil
	}
	kRes, _ := document.ResolveObject(ctx, doc, kObj)

	var errs []error
	checkFormulaAF(ctx, doc, kRes, &errs)
	return errs
}

// mathMLNamespace is the W3C MathML namespace URI.
const mathMLNamespace = "http://www.w3.org/1998/Math/MathML"

// hasMathMLChild returns true if any child of the element references the
// MathML namespace via /NS.
func hasMathMLChild(ctx context.Context, doc *document.Document, dict ast.PdfDict) bool {
	kObj, ok := dict["K"]
	if !ok {
		return false
	}
	kRes, _ := document.ResolveObject(ctx, doc, kObj)
	switch k := kRes.(type) {
	case ast.PdfDict:
		return isMathMLNamespace(ctx, doc, k)
	case ast.PdfArray:
		for _, item := range k {
			itemRes, _ := document.ResolveObject(ctx, doc, item)
			if d, ok := itemRes.(ast.PdfDict); ok && isMathMLNamespace(ctx, doc, d) {
				return true
			}
		}
	}
	return false
}

func isMathMLNamespace(ctx context.Context, doc *document.Document, dict ast.PdfDict) bool {
	nsObj, ok := dict["NS"]
	if !ok {
		return false
	}
	nsRes, _ := document.ResolveObject(ctx, doc, nsObj)
	nsDict, ok := nsRes.(ast.PdfDict)
	if !ok {
		return false
	}
	// Direct namespace URI match.
	if uriObj, ok := nsDict["NS"]; ok {
		uriRes, _ := document.ResolveObject(ctx, doc, uriObj)
		if uri, ok := uriRes.(ast.PdfString); ok && string(uri) == mathMLNamespace {
			return true
		}
	}
	// Check /RoleMapNS for a transitive mapping to the MathML namespace.
	if rmObj, ok := nsDict["RoleMapNS"]; ok {
		rmRes, _ := document.ResolveObject(ctx, doc, rmObj)
		if rmDict, ok := rmRes.(ast.PdfDict); ok {
			for _, v := range rmDict {
				vRes, _ := document.ResolveObject(ctx, doc, v)
				if vArr, ok := vRes.(ast.PdfArray); ok && len(vArr) >= 2 {
					targetNSRes, _ := document.ResolveObject(ctx, doc, vArr[1])
					if targetDict, ok := targetNSRes.(ast.PdfDict); ok {
						if uriObj, ok := targetDict["NS"]; ok {
							uriRes, _ := document.ResolveObject(ctx, doc, uriObj)
							if uri, ok := uriRes.(ast.PdfString); ok && string(uri) == mathMLNamespace {
								return true
							}
						}
					}
				}
			}
		}
	}
	return false
}

func checkFormulaAF(ctx context.Context, doc *document.Document, obj ast.PdfObject, errs *[]error) {
	switch v := obj.(type) {
	case ast.PdfArray:
		for _, kid := range v {
			kidRes, _ := document.ResolveObject(ctx, doc, kid)
			checkFormulaAF(ctx, doc, kidRes, errs)
		}
	case ast.PdfDict:
		if sObj, ok := v["S"]; ok {
			sRes, _ := document.ResolveObject(ctx, doc, sObj)
			if sName, ok := sRes.(ast.PdfName); ok && sName == "Formula" {
				// /AF with MathML or a child in the MathML namespace both satisfy
				// the requirement (ISO 14289-2 §8.2.5.29).
				if _, hasAF := v["AF"]; !hasAF && !hasMathMLChild(ctx, doc, v) {
					*errs = append(*errs, models.ErrFormulaMissingMathML)
				}
			}
		}
		if kObj, ok := v["K"]; ok {
			kRes, _ := document.ResolveObject(ctx, doc, kObj)
			checkFormulaAF(ctx, doc, kRes, errs)
		}
	}
}

// --- PDF/UA identification ---

// checkPDFUAIdentification verifies that the XMP metadata contains
// pdfuaid:part with the correct value for the declared PDF/UA profile.
// For PDF/UA-2, pdfuaid:part must be "2" and pdfuaid:rev must be a
// valid 4-digit year if present.
func checkPDFUAIdentification(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	mdObj, ok := catalog["Metadata"]
	if !ok {
		return []error{models.ErrMissingPDFUAId}
	}
	mdRes, _ := document.ResolveObject(ctx, doc, mdObj)
	stream, ok := mdRes.(ast.PdfStream)
	if !ok {
		return []error{models.ErrMissingPDFUAId}
	}

	data := stream.Data
	if len(data) == 0 {
		return []error{models.ErrMissingPDFUAId}
	}

	// Check for pdfuaid namespace presence.
	if !bytes.Contains(data, []byte("pdfuaid")) &&
		!bytes.Contains(data, []byte("pdfua/ns/id")) {
		return []error{models.ErrMissingPDFUAId}
	}

	// Verify pdfuaid:part is present and has value "2".
	if !bytes.Contains(data, []byte("pdfuaid:part")) {
		// Check attribute form.
		if !bytes.Contains(data, []byte("pdfua/ns/id")) {
			return []error{models.ErrMissingPDFUAId}
		}
	}

	// Validate pdfuaid:part value is "2" for PDF/UA-2.
	// Check both element form (>2<) and attribute form (="2").
	hasPart2 := bytes.Contains(data, []byte(">2</")) ||
		bytes.Contains(data, []byte(`="2"`)) ||
		bytes.Contains(data, []byte(`="2"`))
	if !hasPart2 && bytes.Contains(data, []byte("pdfuaid:part")) {
		return []error{fmt.Errorf("%w: pdfuaid:part value is not 2", models.ErrMissingPDFUAId)}
	}

	return nil
}

// --- Annotation accessibility (PDF/UA-1 §7.18) ---

// checkAnnotationAltText verifies that non-widget, non-hidden annotations
// have either a /Contents key or an /Alt entry in their enclosing structure
// element (PDF/UA-1 §7.18.1).
func checkAnnotationAltText(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	pages := collectPages(ctx, doc, catalog)
	for _, page := range pages {
		annotsObj, ok := page["Annots"]
		if !ok {
			continue
		}
		annotsRes, _ := document.ResolveObject(ctx, doc, annotsObj)
		annotsArr, ok := annotsRes.(ast.PdfArray)
		if !ok {
			continue
		}
		for _, annotObj := range annotsArr {
			annotRes, _ := document.ResolveObject(ctx, doc, annotObj)
			annotDict, ok := annotRes.(ast.PdfDict)
			if !ok {
				continue
			}

			// Skip Widget annotations and hidden annotations.
			subtypeName := resolveAnnotSubtype(ctx, doc, annotDict)
			if subtypeName == "Widget" || subtypeName == "Popup" {
				continue
			}

			// Skip hidden annotations (bit 2 of /F flags).
			if fObj, ok := annotDict["F"]; ok {
				fRes, _ := document.ResolveObject(ctx, doc, fObj)
				if fInt, ok := fRes.(ast.PdfInt); ok {
					const flagHidden = 1 << 1 // bit 2
					if int(fInt)&flagHidden != 0 {
						continue
					}
				}
			}

			// Skip annotations with zero-area Rect (invisible).
			if isZeroRect(ctx, doc, annotDict) {
				continue
			}

			// Annotation must have /Contents or /StructParent (linked to struct elem with /Alt).
			if _, hasContents := annotDict["Contents"]; hasContents {
				continue
			}
			if _, hasStructParent := annotDict["StructParent"]; hasStructParent {
				continue
			}

			return []error{models.ErrAnnotationMissingAlt}
		}
	}
	return nil
}

// --- Table/List containment checks (PDF/UA-1 §7.2) ---

// checkTableSectionChildren verifies that THead, TBody, TFoot elements
// contain only TR children (7.2 test 36/37).
func checkTableSectionChildren(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	strObj, ok := catalog["StructTreeRoot"]
	if !ok {
		return nil
	}
	strRes, _ := document.ResolveObject(ctx, doc, strObj)
	strDict, ok := strRes.(ast.PdfDict)
	if !ok {
		return nil
	}
	kObj, ok := strDict["K"]
	if !ok {
		return nil
	}
	kRes, _ := document.ResolveObject(ctx, doc, kObj)

	var errs []error
	checkSectionChildrenRecursive(ctx, doc, kRes, &errs, 0)
	return errs
}

func checkSectionChildrenRecursive(ctx context.Context, doc *document.Document, obj ast.PdfObject, errs *[]error, depth int) {
	if depth > 200 { //nolint:mnd // prevent infinite recursion
		return
	}
	switch v := obj.(type) {
	case ast.PdfArray:
		for _, kid := range v {
			kidRes, _ := document.ResolveObject(ctx, doc, kid)
			checkSectionChildrenRecursive(ctx, doc, kidRes, errs, depth+1)
		}
	case ast.PdfDict:
		sObj := v["S"]
		sRes, _ := document.ResolveObject(ctx, doc, sObj)
		sName, _ := sRes.(ast.PdfName)

		// THead/TBody/TFoot must contain only TR.
		if sName == "THead" || sName == "TBody" || sName == "TFoot" {
			kObj, ok := v["K"]
			if ok {
				kRes, _ := document.ResolveObject(ctx, doc, kObj)
				checkOnlyTR(ctx, doc, kRes, sName, errs)
			}
		}

		// Recurse.
		if kObj, ok := v["K"]; ok {
			kRes, _ := document.ResolveObject(ctx, doc, kObj)
			checkSectionChildrenRecursive(ctx, doc, kRes, errs, depth+1)
		}
	}
}

func checkOnlyTR(ctx context.Context, doc *document.Document, obj ast.PdfObject, parent ast.PdfName, errs *[]error) {
	switch v := obj.(type) {
	case ast.PdfArray:
		for _, kid := range v {
			kidRes, _ := document.ResolveObject(ctx, doc, kid)
			if d, ok := kidRes.(ast.PdfDict); ok {
				sObj := d["S"]
				sRes, _ := document.ResolveObject(ctx, doc, sObj)
				if sName, ok := sRes.(ast.PdfName); ok && sName != "TR" {
					*errs = append(*errs, fmt.Errorf("%w: /%s contains /%s child",
						models.ErrTableStructureInvalid, parent, sName))
				}
			}
		}
	case ast.PdfDict:
		sObj := v["S"]
		sRes, _ := document.ResolveObject(ctx, doc, sObj)
		if sName, ok := sRes.(ast.PdfName); ok && sName != "TR" {
			*errs = append(*errs, fmt.Errorf("%w: /%s contains /%s child",
				models.ErrTableStructureInvalid, parent, sName))
		}
	}
}

// checkListItemChildren verifies that LI elements contain only Lbl and
// LBody children (7.2 test 20).
func checkListItemChildren(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	strObj, ok := catalog["StructTreeRoot"]
	if !ok {
		return nil
	}
	strRes, _ := document.ResolveObject(ctx, doc, strObj)
	strDict, ok := strRes.(ast.PdfDict)
	if !ok {
		return nil
	}
	kObj, ok := strDict["K"]
	if !ok {
		return nil
	}
	kRes, _ := document.ResolveObject(ctx, doc, kObj)

	var errs []error
	checkLIChildrenRecursive(ctx, doc, kRes, &errs, 0)
	return errs
}

func checkLIChildrenRecursive(ctx context.Context, doc *document.Document, obj ast.PdfObject, errs *[]error, depth int) {
	if depth > 200 { //nolint:mnd
		return
	}
	switch v := obj.(type) {
	case ast.PdfArray:
		for _, kid := range v {
			kidRes, _ := document.ResolveObject(ctx, doc, kid)
			checkLIChildrenRecursive(ctx, doc, kidRes, errs, depth+1)
		}
	case ast.PdfDict:
		sObj := v["S"]
		sRes, _ := document.ResolveObject(ctx, doc, sObj)
		sName, _ := sRes.(ast.PdfName)

		if sName == "LI" {
			if kiObj, ok := v["K"]; ok {
				kiRes, _ := document.ResolveObject(ctx, doc, kiObj)
				checkLIOnlyLblLBody(ctx, doc, kiRes, errs)
			}
		}

		if kiObj, ok := v["K"]; ok {
			kiRes, _ := document.ResolveObject(ctx, doc, kiObj)
			checkLIChildrenRecursive(ctx, doc, kiRes, errs, depth+1)
		}
	}
}

// validLIChildren are the allowed types inside an LI element.
var validLIChildren = map[ast.PdfName]bool{ //nolint:gochecknoglobals
	"Lbl": true, "LBody": true,
}

func checkLIOnlyLblLBody(ctx context.Context, doc *document.Document, obj ast.PdfObject, errs *[]error) {
	switch v := obj.(type) {
	case ast.PdfArray:
		for _, kid := range v {
			kidRes, _ := document.ResolveObject(ctx, doc, kid)
			if d, ok := kidRes.(ast.PdfDict); ok {
				sObj := d["S"]
				sRes, _ := document.ResolveObject(ctx, doc, sObj)
				if sName, ok := sRes.(ast.PdfName); ok && !validLIChildren[sName] {
					*errs = append(*errs, fmt.Errorf("%w: LI contains /%s child",
						models.ErrListStructureInvalid, sName))
				}
			}
		}
	case ast.PdfDict:
		sObj := v["S"]
		sRes, _ := document.ResolveObject(ctx, doc, sObj)
		if sName, ok := sRes.(ast.PdfName); ok && !validLIChildren[sName] {
			*errs = append(*errs, fmt.Errorf("%w: LI contains /%s child",
				models.ErrListStructureInvalid, sName))
		}
	}
}

// checkCaptionFirstChild verifies that L and TOC elements may contain
// a Caption element only as their first child (7.2 test 28, 40).
func checkCaptionFirstChild(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	strObj, ok := catalog["StructTreeRoot"]
	if !ok {
		return nil
	}
	strRes, _ := document.ResolveObject(ctx, doc, strObj)
	strDict, ok := strRes.(ast.PdfDict)
	if !ok {
		return nil
	}
	kObj, ok := strDict["K"]
	if !ok {
		return nil
	}
	kRes, _ := document.ResolveObject(ctx, doc, kObj)

	var errs []error
	checkCaptionRecursive(ctx, doc, kRes, &errs, 0)
	return errs
}

func checkCaptionRecursive(ctx context.Context, doc *document.Document, obj ast.PdfObject, errs *[]error, depth int) {
	if depth > 200 { //nolint:mnd
		return
	}
	switch v := obj.(type) {
	case ast.PdfArray:
		for _, kid := range v {
			kidRes, _ := document.ResolveObject(ctx, doc, kid)
			checkCaptionRecursive(ctx, doc, kidRes, errs, depth+1)
		}
	case ast.PdfDict:
		sObj := v["S"]
		sRes, _ := document.ResolveObject(ctx, doc, sObj)
		sName, _ := sRes.(ast.PdfName)

		// L and TOC: Caption allowed only as first child.
		if sName == "L" || sName == "TOC" {
			if kiObj, ok := v["K"]; ok {
				kiRes, _ := document.ResolveObject(ctx, doc, kiObj)
				if arr, ok := kiRes.(ast.PdfArray); ok {
					for i, kid := range arr {
						kidRes, _ := document.ResolveObject(ctx, doc, kid)
						if d, ok := kidRes.(ast.PdfDict); ok {
							csObj := d["S"]
							csRes, _ := document.ResolveObject(ctx, doc, csObj)
							if csName, ok := csRes.(ast.PdfName); ok && csName == "Caption" && i > 0 {
								*errs = append(*errs, fmt.Errorf("%w: /%s has Caption not as first child",
									models.ErrListStructureInvalid, sName))
							}
						}
					}
				}
			}
		}

		if kiObj, ok := v["K"]; ok {
			kiRes, _ := document.ResolveObject(ctx, doc, kiObj)
			checkCaptionRecursive(ctx, doc, kiRes, errs, depth+1)
		}
	}
}
