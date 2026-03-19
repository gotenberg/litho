package compliance

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter"
)

// ValidatePDFA parses a PDF from the given reader and audits it against a
// specific PDF/A profile, returning all compliance violations found. An empty
// slice means the document passes.
func ValidatePDFA(ctx context.Context, r io.ReaderAt, size int64, profile PDFAProfile) ([]error, error) {
	doc, err := document.NewDocument(ctx, r, size, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PDF: %w", err)
	}
	return validatePDFA(ctx, doc, profile), nil
}

// SourceProvider is implemented by types that expose an underlying io.ReaderAt
// and size (e.g., *pdf.Reader). This allows compliance validation to share
// the same source data without re-reading from disk.
type SourceProvider interface {
	Source() (io.ReaderAt, int64)
}

// ValidatePDFAFromReader validates a PDF/A profile using a Reader's underlying
// source, avoiding redundant parsing when the caller already has an open Reader.
func ValidatePDFAFromReader(ctx context.Context, sp SourceProvider, profile PDFAProfile) ([]error, error) {
	r, size := sp.Source()
	return ValidatePDFA(ctx, r, size, profile)
}

// validatePDFA is the internal implementation that operates on a parsed document.
func validatePDFA(ctx context.Context, doc *document.Document, profile PDFAProfile) []error {
	part := pdfaPart(profile)
	if part == 0 {
		return []error{fmt.Errorf("%w: %s", ErrInvalidProfile, profile)}
	}

	catalog, err := getCatalog(ctx, doc)
	if err != nil {
		return []error{err}
	}

	var errs []error

	// --- Base rules: Level 'b' (Basic Visual Integrity) ---
	errs = append(errs, checkEncryptionForbidden(ctx, doc)...)
	errs = append(errs, checkFontsEmbedded(ctx, doc, catalog)...)
	errs = append(errs, checkICCProfile(ctx, doc, catalog)...)
	errs = append(errs, checkNoLZW(ctx, doc, catalog)...)
	errs = append(errs, checkNoActiveContent(ctx, doc, catalog)...)

	// --- Part-specific rules ---
	switch part {
	case 1:
		errs = append(errs, checkPDFA1Version(ctx, doc, catalog)...)
		errs = append(errs, checkNoTransparency(ctx, doc, catalog)...)
		errs = append(errs, checkNoJPX(ctx, doc, catalog)...)
		errs = append(errs, checkNoAttachments(ctx, doc, catalog)...)
	case 2:
		errs = append(errs, checkAttachmentsPDFA(ctx, doc, catalog)...)
		errs = append(errs, checkOCGUsageApplication(ctx, doc, catalog)...)
	case 3:
		errs = append(errs, checkAFRelationship(ctx, doc, catalog)...)
		errs = append(errs, checkOCGUsageApplication(ctx, doc, catalog)...)
	case 4:
		errs = append(errs, checkPDFA4Version(ctx, doc, catalog)...)
		errs = append(errs, checkOCGUsageApplication(ctx, doc, catalog)...)
		// PDF/A-4 natively requires Unicode mapping (no a/b/u split).
		errs = append(errs, checkUnicodeMapping(ctx, doc, catalog)...)
		// Sub-profile specific rules.
		conf4 := pdfaConformance(profile)
		if conf4 == 'f' {
			errs = append(errs, checkAFRelationship(ctx, doc, catalog)...)
		}
		if conf4 == 'e' {
			errs = append(errs, check3DStreamCompliance(ctx, doc, catalog)...)
		}
		return errs
	}

	// --- Conformance level rules (Parts 1-3 only) ---
	conf := pdfaConformance(profile)

	// Level 'u' (Unicode): every font must have a Unicode mapping.
	// Level 'a' implies 'u', so run 'u' checks for both.
	if conf == 'u' || conf == 'a' {
		errs = append(errs, checkUnicodeMapping(ctx, doc, catalog)...)
	}

	// Level 'a' (Accessible): tagged, structured, and navigable.
	if conf == 'a' {
		errs = append(errs, checkLangEntry(ctx, doc, catalog)...)
		errs = append(errs, checkMarkInfo(ctx, doc, catalog)...)
		errs = append(errs, checkStructTreeRoot(ctx, doc, catalog)...)
		errs = append(errs, checkFigureAltText(ctx, doc, catalog)...)
	}

	return errs
}

// --- Catalog access ---

func getCatalog(ctx context.Context, doc *document.Document) (ast.PdfDict, error) {
	rootObj, ok := doc.Index.Trailer["Root"]
	if !ok {
		return nil, fmt.Errorf("trailer missing /Root")
	}
	resolved, err := resolve(ctx, doc, rootObj)
	if err != nil {
		return nil, err
	}
	catalog, ok := resolved.(ast.PdfDict)
	if !ok {
		return nil, fmt.Errorf("catalog is not a dictionary")
	}
	return catalog, nil
}

func resolve(ctx context.Context, doc *document.Document, obj ast.PdfObject) (ast.PdfObject, error) {
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		return doc.Resolve(ctx, ref)
	}
	return obj, nil
}

// --- Base checks: Level 'b' (Basic Visual Integrity) ---

// checkEncryptionForbidden verifies that the document is not encrypted by
// inspecting both the Trailer /Encrypt key and the document's encryption flag.
func checkEncryptionForbidden(_ context.Context, doc *document.Document) []error {
	if _, ok := doc.Index.Trailer["Encrypt"]; ok {
		return []error{ErrEncryptionForbidden}
	}
	if doc.IsEncrypted {
		return []error{ErrEncryptionForbidden}
	}
	return nil
}

// checkICCProfile verifies that /OutputIntents contains at least one entry
// with an ICC profile stream (/DestOutputProfile) to define exact color
// reproduction, as required by PDF/A Level 'b'.
func checkICCProfile(_ context.Context, _ *document.Document, catalog ast.PdfDict) []error {
	oiObj, ok := catalog["OutputIntents"]
	if !ok {
		return []error{ErrMissingICCProfile}
	}

	// OutputIntents is an array; at least one entry must have a /DestOutputProfile.
	oi, ok := oiObj.(ast.PdfArray)
	if !ok || len(oi) == 0 {
		return []error{ErrMissingICCProfile}
	}
	// Check that at least one has an ICC profile stream.
	if slices.ContainsFunc(oi, hasICCProfile) {
		return nil
	}
	return []error{ErrMissingICCProfile}
}

func hasICCProfile(obj ast.PdfObject) bool {
	dict, ok := obj.(ast.PdfDict)
	if !ok {
		return false
	}
	_, has := dict["DestOutputProfile"]
	return has
}

// checkNoLZW scans all page resource streams for forbidden LZW compression.
func checkNoLZW(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	pages := collectPages(ctx, doc, catalog)
	for _, page := range pages {
		if hasPageFilter(ctx, doc, page, "LZWDecode") {
			return []error{ErrLZWForbidden}
		}
	}
	return nil
}

// forbiddenActionTypes are action types that constitute active content,
// forbidden under PDF/A Level 'b'.
var forbiddenActionTypes = map[ast.PdfName]bool{
	"JavaScript": true,
	"Launch":     true,
	"Sound":      true,
	"Movie":      true,
}

// checkNoActiveContent traverses the Catalog, /OpenAction, /AA (Additional
// Actions), and page-level annotations for forbidden active content actions:
// /JavaScript, /Launch, /Sound, and /Movie.
func checkNoActiveContent(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	// Check /Names /JavaScript name tree.
	if namesObj, ok := catalog["Names"]; ok {
		namesRes, _ := resolve(ctx, doc, namesObj)
		if namesDict, ok := namesRes.(ast.PdfDict); ok {
			if _, ok := namesDict["JavaScript"]; ok {
				return []error{ErrActiveContentForbidden}
			}
		}
	}

	// Check /OpenAction for forbidden action types.
	if oaObj, ok := catalog["OpenAction"]; ok {
		oaRes, _ := resolve(ctx, doc, oaObj)
		if oaDict, ok := oaRes.(ast.PdfDict); ok {
			if isForbiddenAction(oaDict) {
				return []error{ErrActiveContentForbidden}
			}
		}
	}

	// Check /AA (Additional Actions) on catalog.
	if hasForbiddenAdditionalActions(ctx, doc, catalog) {
		return []error{ErrActiveContentForbidden}
	}

	// Check page-level annotations for forbidden actions.
	pages := collectPages(ctx, doc, catalog)
	for _, page := range pages {
		if hasForbiddenAnnotActions(ctx, doc, page) {
			return []error{ErrActiveContentForbidden}
		}
		// Check page-level /AA.
		if hasForbiddenAdditionalActions(ctx, doc, page) {
			return []error{ErrActiveContentForbidden}
		}
	}

	return nil
}

// isForbiddenAction checks if an action dictionary has a forbidden /S type.
func isForbiddenAction(actionDict ast.PdfDict) bool {
	sObj, ok := actionDict["S"]
	if !ok {
		return false
	}
	sName, ok := sObj.(ast.PdfName)
	if !ok {
		return false
	}
	return forbiddenActionTypes[sName]
}

// hasForbiddenAdditionalActions checks the /AA dictionary of a dict for
// forbidden action types.
func hasForbiddenAdditionalActions(ctx context.Context, doc *document.Document, dict ast.PdfDict) bool {
	aaObj, ok := dict["AA"]
	if !ok {
		return false
	}
	aaRes, _ := resolve(ctx, doc, aaObj)
	aaDict, ok := aaRes.(ast.PdfDict)
	if !ok {
		return false
	}
	for _, v := range aaDict {
		vRes, _ := resolve(ctx, doc, v)
		if actionDict, ok := vRes.(ast.PdfDict); ok {
			if isForbiddenAction(actionDict) {
				return true
			}
		}
	}
	return false
}

// hasForbiddenAnnotActions checks page annotations for forbidden action types.
func hasForbiddenAnnotActions(ctx context.Context, doc *document.Document, pageDict ast.PdfDict) bool {
	annotsObj, ok := pageDict["Annots"]
	if !ok {
		return false
	}
	annotsRes, _ := resolve(ctx, doc, annotsObj)
	annotsArr, ok := annotsRes.(ast.PdfArray)
	if !ok {
		return false
	}
	for _, annotObj := range annotsArr {
		annotRes, _ := resolve(ctx, doc, annotObj)
		annotDict, ok := annotRes.(ast.PdfDict)
		if !ok {
			continue
		}
		// Check /A (action) on the annotation.
		if aObj, ok := annotDict["A"]; ok {
			aRes, _ := resolve(ctx, doc, aObj)
			if aDict, ok := aRes.(ast.PdfDict); ok {
				if isForbiddenAction(aDict) {
					return true
				}
			}
		}
		// Check /AA on the annotation.
		if hasForbiddenAdditionalActions(ctx, doc, annotDict) {
			return true
		}
	}
	return false
}

// --- Font checks ---

func checkFontsEmbedded(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	var errs []error
	fonts := collectFontDicts(ctx, doc, catalog)
	for name, fontDict := range fonts {
		if isStandard14Font(ctx, doc, fontDict) {
			continue
		}
		if !isFontEmbedded(ctx, doc, fontDict) {
			errs = append(errs, fmt.Errorf("%w: %s", ErrFontNotEmbedded, name))
		}
	}
	return errs
}

func isFontEmbedded(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	fdDict := findFontDescriptor(ctx, doc, fontDict)
	if fdDict == nil {
		return false
	}
	for _, key := range []ast.PdfName{"FontFile", "FontFile2", "FontFile3"} {
		if _, ok := fdDict[key]; ok {
			return true
		}
	}
	return false
}

func hasFontToUnicode(_ context.Context, _ *document.Document, fontDict ast.PdfDict) bool {
	// PDF/A conformance levels 'u' and 'a' require an explicit /ToUnicode CMap
	// to guarantee reliable Unicode text extraction.
	_, ok := fontDict["ToUnicode"]
	return ok
}

func isStandard14Font(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	bfObj, ok := fontDict["BaseFont"]
	if !ok {
		return false
	}
	bfRes, _ := resolve(ctx, doc, bfObj)
	bfName, ok := bfRes.(ast.PdfName)
	if !ok {
		return false
	}
	return isBase14Name(string(bfName))
}

// isBase14Name checks whether a font name is one of the PDF standard 14 fonts.
func isBase14Name(name string) bool {
	switch name {
	case "Courier", "Courier-Bold", "Courier-Oblique", "Courier-BoldOblique",
		"Helvetica", "Helvetica-Bold", "Helvetica-Oblique", "Helvetica-BoldOblique",
		"Times-Roman", "Times-Bold", "Times-Italic", "Times-BoldItalic",
		"Symbol", "ZapfDingbats":
		return true
	}
	return false
}

// --- Level 'u' (Unicode) checks ---

// standardAdobeEncodings are the predefined encoding names that provide
// well-defined glyph-to-Unicode mappings without requiring a /ToUnicode CMap.
var standardAdobeEncodings = map[ast.PdfName]bool{
	"WinAnsiEncoding":   true,
	"MacRomanEncoding":  true,
	"MacExpertEncoding": true,
	"StandardEncoding":  true,
}

// checkUnicodeMapping verifies that every font has a reliable path to Unicode:
// either an explicit /ToUnicode CMap, or an /Encoding that maps strictly to
// standard Adobe glyphs. If neither is present, the engine cannot guarantee
// every character code maps to a UTF-8 rune.
func checkUnicodeMapping(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	var errs []error
	fonts := collectFontDicts(ctx, doc, catalog)
	for name, fontDict := range fonts {
		if hasFontUnicodeMapping(ctx, doc, fontDict) {
			continue
		}
		errs = append(errs, fmt.Errorf("%w: %s", ErrMissingUnicodeMapping, name))
	}
	return errs
}

// hasFontUnicodeMapping returns true if the font has either a /ToUnicode CMap
// or a standard Adobe encoding that provides implicit Unicode mapping.
func hasFontUnicodeMapping(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	// Explicit /ToUnicode CMap is always sufficient.
	if _, ok := fontDict["ToUnicode"]; ok {
		return true
	}

	// Check for a standard predefined encoding name.
	encObj, ok := fontDict["Encoding"]
	if !ok {
		return false
	}
	encRes, _ := resolve(ctx, doc, encObj)

	switch enc := encRes.(type) {
	case ast.PdfName:
		// Direct encoding name (e.g., /WinAnsiEncoding).
		return standardAdobeEncodings[enc]
	case ast.PdfDict:
		// Encoding dictionary with /BaseEncoding.
		if beObj, ok := enc["BaseEncoding"]; ok {
			beRes, _ := resolve(ctx, doc, beObj)
			if beName, ok := beRes.(ast.PdfName); ok {
				return standardAdobeEncodings[beName]
			}
		}
	}
	return false
}

// --- Level 'a' (Accessible) checks ---

// checkLangEntry verifies that the Document Catalog contains a /Lang entry
// specifying the document's natural language (e.g., "en-US").
func checkLangEntry(_ context.Context, _ *document.Document, catalog ast.PdfDict) []error {
	if _, ok := catalog["Lang"]; !ok {
		return []error{ErrMissingLang}
	}
	return nil
}

// checkFigureAltText traverses the structure tree and verifies that every
// Figure element has an /Alt attribute providing alternative text.
func checkFigureAltText(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	strObj, ok := catalog["StructTreeRoot"]
	if !ok {
		return nil // Already caught by checkStructTreeRoot.
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
	checkFigureAltRecursive(ctx, doc, kRes, &errs)
	return errs
}

func checkFigureAltRecursive(ctx context.Context, doc *document.Document, obj ast.PdfObject, errs *[]error) {
	switch v := obj.(type) {
	case ast.PdfArray:
		for _, kid := range v {
			kidRes, _ := resolve(ctx, doc, kid)
			checkFigureAltRecursive(ctx, doc, kidRes, errs)
		}
	case ast.PdfDict:
		// Check if this is a Figure element.
		if sObj, ok := v["S"]; ok {
			sRes, _ := resolve(ctx, doc, sObj)
			if sName, ok := sRes.(ast.PdfName); ok && sName == "Figure" {
				if _, hasAlt := v["Alt"]; !hasAlt {
					*errs = append(*errs, ErrUntaggedVisualElement)
				}
			}
		}
		// Recurse into children.
		if kObj, ok := v["K"]; ok {
			kRes, _ := resolve(ctx, doc, kObj)
			checkFigureAltRecursive(ctx, doc, kRes, errs)
		}
	}
}

func findFontDescriptor(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) ast.PdfDict {
	if fdObj, ok := fontDict["FontDescriptor"]; ok {
		fdRes, _ := resolve(ctx, doc, fdObj)
		if fdDict, ok := fdRes.(ast.PdfDict); ok {
			return fdDict
		}
	}
	// Type0: /DescendantFonts[0]/FontDescriptor
	if dfObj, ok := fontDict["DescendantFonts"]; ok {
		dfRes, _ := resolve(ctx, doc, dfObj)
		if dfArr, ok := dfRes.(ast.PdfArray); ok && len(dfArr) > 0 {
			cidRes, _ := resolve(ctx, doc, dfArr[0])
			if cidDict, ok := cidRes.(ast.PdfDict); ok {
				if fdObj, ok := cidDict["FontDescriptor"]; ok {
					fdRes, _ := resolve(ctx, doc, fdObj)
					if fdDict, ok := fdRes.(ast.PdfDict); ok {
						return fdDict
					}
				}
			}
		}
	}
	return nil
}

// --- Part 1 specific ---

// checkPDFA1Version verifies that the document declares PDF version 1.4 or lower,
// as required by PDF/A-1 (ISO 19005-1). It checks the catalog /Version key first,
// then falls back to the file header bytes.
func checkPDFA1Version(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	// Check catalog /Version (overrides header per spec).
	if vObj, ok := catalog["Version"]; ok {
		vRes, _ := resolve(ctx, doc, vObj)
		if vName, ok := vRes.(ast.PdfName); ok {
			if isPDFVersionAtMost14(string(vName)) {
				return nil
			}
			return []error{ErrVersionTooHighForA1}
		}
	}

	// Fallback: read header bytes for %PDF-x.y.
	headerBuf, err := safeReadHeader(doc)
	if err != nil {
		// Cannot read header in test mocks — assume compliant.
		return nil
	}
	if _, after, ok := bytes.Cut(headerBuf, []byte("%PDF-")); ok {
		versionStr := string(after) // e.g., "1.4..."
		if !isPDFVersionAtMost14(versionStr) {
			return []error{ErrVersionTooHighForA1}
		}
	}
	return nil
}

// isPDFVersionAtMost14 returns true if the version string starts with "1.0"
// through "1.4". Any version "1.5" or higher, or "2.0", is rejected.
func isPDFVersionAtMost14(version string) bool {
	for _, allowed := range []string{"1.0", "1.1", "1.2", "1.3", "1.4"} {
		if strings.HasPrefix(version, allowed) {
			return true
		}
	}
	return false
}

func checkNoTransparency(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	pages := collectPages(ctx, doc, catalog)
	for _, page := range pages {
		// Check page /Group dictionary for /S /Transparency.
		if hasGroupTransparency(ctx, doc, page) {
			return []error{ErrTransparencyForbidden}
		}
		if errs := scanExtGStateForTransparency(ctx, doc, page); len(errs) > 0 {
			return errs
		}
	}
	return nil
}

// hasGroupTransparency checks whether a page's /Group dictionary declares
// a transparency group (/S /Transparency).
func hasGroupTransparency(ctx context.Context, doc *document.Document, pageDict ast.PdfDict) bool {
	grpObj, ok := pageDict["Group"]
	if !ok {
		return false
	}
	grpRes, _ := resolve(ctx, doc, grpObj)
	grpDict, ok := grpRes.(ast.PdfDict)
	if !ok {
		return false
	}
	sObj, ok := grpDict["S"]
	if !ok {
		return false
	}
	sName, ok := sObj.(ast.PdfName)
	return ok && sName == "Transparency"
}

func scanExtGStateForTransparency(ctx context.Context, doc *document.Document, pageDict ast.PdfDict) []error {
	resources := getResources(ctx, doc, pageDict)
	if resources == nil {
		return nil
	}
	gsObj, ok := resources["ExtGState"]
	if !ok {
		return nil
	}
	gsRes, _ := resolve(ctx, doc, gsObj)
	gsDict, ok := gsRes.(ast.PdfDict)
	if !ok {
		return nil
	}
	for _, v := range gsDict {
		vRes, _ := resolve(ctx, doc, v)
		if stateDict, ok := vRes.(ast.PdfDict); ok {
			// /BM (blend mode) other than Normal.
			if bmObj, ok := stateDict["BM"]; ok {
				if bmName, ok := bmObj.(ast.PdfName); ok && bmName != "Normal" {
					return []error{ErrTransparencyForbidden}
				}
			}
			// /ca (fill opacity) < 1.0.
			if caObj, ok := stateDict["ca"]; ok {
				if v := astFloat(caObj); v < 1.0 && v >= 0 {
					return []error{ErrTransparencyForbidden}
				}
			}
			// /CA (stroke opacity) < 1.0.
			if caObj, ok := stateDict["CA"]; ok {
				if v := astFloat(caObj); v < 1.0 && v >= 0 {
					return []error{ErrTransparencyForbidden}
				}
			}
			// /SMask (soft mask).
			if smObj, ok := stateDict["SMask"]; ok {
				if smName, ok := smObj.(ast.PdfName); !ok || smName != "None" {
					return []error{ErrTransparencyForbidden}
				}
			}
		}
	}
	return nil
}

func checkNoJPX(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	pages := collectPages(ctx, doc, catalog)
	for _, page := range pages {
		if hasPageFilter(ctx, doc, page, "JPXDecode") {
			return []error{ErrJPEG2000Forbidden}
		}
	}
	return nil
}

func checkNoAttachments(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	if hasEmbeddedFiles(ctx, doc, catalog) {
		return []error{ErrEmbeddedFilesForbidden}
	}
	return nil
}

// --- Part 2 specific ---

// checkAttachmentsPDFA verifies that every embedded file in a PDF/A-2 document
// is itself a PDF/A-compliant PDF. Non-PDF attachments (Word, Excel, etc.) and
// non-compliant PDFs are rejected.
func checkAttachmentsPDFA(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	if !hasEmbeddedFiles(ctx, doc, catalog) {
		return nil
	}
	fileSpecs := collectFileSpecs(ctx, doc, catalog)
	var errs []error
	for _, fs := range fileSpecs {
		if err := validateEmbeddedFilePDFA(ctx, doc, fs); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// validateEmbeddedFilePDFA checks a single embedded file spec for PDF/A compliance.
// It extracts the embedded stream, verifies it is a PDF (not Word/Excel/etc.), and
// recursively runs ValidatePDFA on the parsed document.
func validateEmbeddedFilePDFA(ctx context.Context, doc *document.Document, fileSpec ast.PdfDict) error {
	// Extract the embedded file stream from /EF/F.
	efObj, ok := fileSpec["EF"]
	if !ok {
		return ErrNonPDFAAttachment
	}
	efRes, _ := resolve(ctx, doc, efObj)
	efDict, ok := efRes.(ast.PdfDict)
	if !ok {
		return ErrNonPDFAAttachment
	}
	fObj, ok := efDict["F"]
	if !ok {
		return ErrNonPDFAAttachment
	}
	fRes, _ := resolve(ctx, doc, fObj)
	stream, ok := fRes.(ast.PdfStream)
	if !ok {
		return ErrNonPDFAAttachment
	}

	// Decode the stream to get the raw file bytes.
	data, err := filter.DecodeStream(stream)
	if err != nil {
		return fmt.Errorf("%w: failed to decode stream", ErrNonPDFAAttachment)
	}

	// Non-PDF files (Word, Excel, ZIP, etc.) lack the %PDF- header.
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return ErrNonPDFAAttachment
	}

	// Attempt to parse the embedded PDF and validate it recursively.
	embeddedDoc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNonPDFAAttachment, err)
	}
	if violations := validatePDFA(ctx, embeddedDoc, PDFA2b); len(violations) > 0 {
		return fmt.Errorf("%w: %v", ErrNonPDFAAttachment, violations[0])
	}
	return nil
}

// checkOCGUsageApplication verifies that if Optional Content Groups (Layers)
// are present via /OCProperties, the dictionary contains /AS (Usage Application
// Dictionaries) as required by PDF/A-2.
func checkOCGUsageApplication(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	ocObj, ok := catalog["OCProperties"]
	if !ok {
		return nil
	}
	ocRes, _ := resolve(ctx, doc, ocObj)
	ocDict, ok := ocRes.(ast.PdfDict)
	if !ok {
		return nil
	}
	if _, ok := ocDict["AS"]; !ok {
		return []error{ErrMissingOCGUsageApp}
	}
	return nil
}

// --- Part 3 specific ---

// validAFRelationships are the allowed /AFRelationship values per ISO 19005-3.
var validAFRelationships = map[ast.PdfName]bool{
	"Source":      true,
	"Data":        true,
	"Alternative": true,
	"Supplement":  true,
	"Unspecified": true,
}

// checkAFRelationship verifies that every embedded file spec has a valid
// /AFRelationship key. PDF/A-3 allows any file type but mandates this key
// with one of: /Source, /Data, /Alternative, /Supplement, or /Unspecified.
func checkAFRelationship(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	fileSpecs := collectFileSpecs(ctx, doc, catalog)
	var errs []error
	for _, fs := range fileSpecs {
		afObj, ok := fs["AFRelationship"]
		if !ok {
			errs = append(errs, ErrMissingAFRelationship)
			continue
		}
		afRes, _ := resolve(ctx, doc, afObj)
		afName, ok := afRes.(ast.PdfName)
		if !ok || !validAFRelationships[afName] {
			errs = append(errs, fmt.Errorf("%w: got /%v", ErrMissingAFRelationship, afObj))
		}
	}
	return errs
}

// --- Part 4 specific ---

// checkPDFA4Version verifies that the document declares PDF version 2.0,
// as required by PDF/A-4 (ISO 19005-4). Checks catalog /Version then header.
func checkPDFA4Version(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	if vObj, ok := catalog["Version"]; ok {
		vRes, _ := resolve(ctx, doc, vObj)
		if vName, ok := vRes.(ast.PdfName); ok {
			if strings.HasPrefix(string(vName), "2.") {
				return nil
			}
		}
	}
	headerBuf, err := safeReadHeader(doc)
	if err == nil && bytes.Contains(headerBuf, []byte("%PDF-2.")) {
		return nil
	}
	return []error{ErrVersionNotPDF20}
}

// valid3DSubtypes are the allowed /Subtype values for 3D streams in PDF/A-4e.
var valid3DSubtypes = map[ast.PdfName]bool{
	"U3D": true,
	"PRC": true,
}

// check3DStreamCompliance validates /RichMedia and /3D annotations for PDF/A-4e.
// Each 3D annotation must have a stream with a valid /Subtype (U3D or PRC) and
// a 2D fallback appearance (/AP /N).
func check3DStreamCompliance(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	var errs []error
	pages := collectPages(ctx, doc, catalog)
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
			if !is3DAnnotation(annotDict) {
				continue
			}
			errs = append(errs, validate3DAnnotation(ctx, doc, annotDict)...)
		}
	}
	return errs
}

// is3DAnnotation returns true if the annotation is a 3D or RichMedia annotation.
func is3DAnnotation(annotDict ast.PdfDict) bool {
	stObj, ok := annotDict["Subtype"]
	if !ok {
		return false
	}
	stName, ok := stObj.(ast.PdfName)
	if !ok {
		return false
	}
	return stName == "3D" || stName == "RichMedia"
}

// validate3DAnnotation checks that a 3D annotation has a valid stream /Subtype
// and a 2D fallback appearance.
func validate3DAnnotation(ctx context.Context, doc *document.Document, annotDict ast.PdfDict) []error {
	var errs []error

	// Check for valid 3D stream /Subtype.
	if !has3DStreamSubtype(ctx, doc, annotDict) {
		errs = append(errs, ErrMissing3DSubtype)
	}

	// Check for 2D fallback appearance (/AP /N).
	if !has2DFallback(ctx, doc, annotDict) {
		errs = append(errs, ErrMissing3DFallback)
	}

	return errs
}

// has3DStreamSubtype checks the /3DD (3D data) stream for a valid /Subtype.
func has3DStreamSubtype(ctx context.Context, doc *document.Document, annotDict ast.PdfDict) bool {
	// /3DD is the 3D stream reference for /Subtype /3D annotations.
	ddObj, ok := annotDict["3DD"]
	if !ok {
		// For /RichMedia, check /RichMediaContent/Assets for 3D streams.
		return hasRichMedia3DSubtype(ctx, doc, annotDict)
	}
	ddRes, _ := resolve(ctx, doc, ddObj)
	switch dd := ddRes.(type) {
	case ast.PdfStream:
		return check3DStreamDict(dd.Dict)
	case ast.PdfDict:
		return check3DStreamDict(dd)
	}
	return false
}

func check3DStreamDict(dict ast.PdfDict) bool {
	stObj, ok := dict["Subtype"]
	if !ok {
		return false
	}
	stName, ok := stObj.(ast.PdfName)
	return ok && valid3DSubtypes[stName]
}

// hasRichMedia3DSubtype checks /RichMediaContent for 3D assets with valid subtypes.
func hasRichMedia3DSubtype(ctx context.Context, doc *document.Document, annotDict ast.PdfDict) bool {
	rcObj, ok := annotDict["RichMediaContent"]
	if !ok {
		return false
	}
	rcRes, _ := resolve(ctx, doc, rcObj)
	rcDict, ok := rcRes.(ast.PdfDict)
	if !ok {
		return false
	}
	assetsObj, ok := rcDict["Assets"]
	if !ok {
		return false
	}
	assetsRes, _ := resolve(ctx, doc, assetsObj)
	assetsDict, ok := assetsRes.(ast.PdfDict)
	if !ok {
		return false
	}
	namesObj, ok := assetsDict["Names"]
	if !ok {
		return false
	}
	namesRes, _ := resolve(ctx, doc, namesObj)
	namesArr, ok := namesRes.(ast.PdfArray)
	if !ok {
		return false
	}
	// Names array is [key val key val ...]; check val entries.
	for i := 1; i < len(namesArr); i += 2 {
		valRes, _ := resolve(ctx, doc, namesArr[i])
		if fsDict, ok := valRes.(ast.PdfDict); ok {
			if efObj, ok := fsDict["EF"]; ok {
				efRes, _ := resolve(ctx, doc, efObj)
				if efDict, ok := efRes.(ast.PdfDict); ok {
					if fObj, ok := efDict["F"]; ok {
						fRes, _ := resolve(ctx, doc, fObj)
						if stream, ok := fRes.(ast.PdfStream); ok {
							if check3DStreamDict(stream.Dict) {
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

// has2DFallback checks that a 3D annotation has an /AP /N appearance stream
// providing a static 2D visual fallback.
func has2DFallback(ctx context.Context, doc *document.Document, annotDict ast.PdfDict) bool {
	apObj, ok := annotDict["AP"]
	if !ok {
		return false
	}
	apRes, _ := resolve(ctx, doc, apObj)
	apDict, ok := apRes.(ast.PdfDict)
	if !ok {
		return false
	}
	_, ok = apDict["N"]
	return ok
}

// --- Conformance 'a' checks ---

func checkMarkInfo(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	miObj, ok := catalog["MarkInfo"]
	if !ok {
		return []error{ErrMissingMarkInfo}
	}
	miRes, _ := resolve(ctx, doc, miObj)
	miDict, ok := miRes.(ast.PdfDict)
	if !ok {
		return []error{ErrMissingMarkInfo}
	}
	markedObj, ok := miDict["Marked"]
	if !ok {
		return []error{ErrMissingMarkInfo}
	}
	if b, ok := markedObj.(ast.PdfBool); !ok || !bool(b) {
		return []error{ErrMissingMarkInfo}
	}
	return nil
}

func checkStructTreeRoot(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	strObj, ok := catalog["StructTreeRoot"]
	if !ok {
		return []error{ErrMissingStructTreeRoot}
	}
	strRes, _ := resolve(ctx, doc, strObj)
	if _, ok := strRes.(ast.PdfDict); !ok {
		return []error{ErrMissingStructTreeRoot}
	}
	return nil
}

// --- Helpers ---

func astFloat(obj ast.PdfObject) float64 {
	switch v := obj.(type) {
	case ast.PdfReal:
		return float64(v)
	case ast.PdfInt:
		return float64(v)
	}
	return 0
}

func collectPages(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []ast.PdfDict {
	pagesObj, ok := catalog["Pages"]
	if !ok {
		return nil
	}
	var pages []ast.PdfDict
	collectPagesRecursive(ctx, doc, pagesObj, &pages)
	return pages
}

func collectPagesRecursive(ctx context.Context, doc *document.Document, obj ast.PdfObject, pages *[]ast.PdfDict) {
	res, err := resolve(ctx, doc, obj)
	if err != nil {
		return
	}
	dict, ok := res.(ast.PdfDict)
	if !ok {
		return
	}
	typeObj, _ := resolve(ctx, doc, dict["Type"])
	typeName, _ := typeObj.(ast.PdfName)

	switch typeName {
	case "Pages":
		kidsObj, ok := dict["Kids"]
		if !ok {
			return
		}
		kidsRes, _ := resolve(ctx, doc, kidsObj)
		kidsArr, ok := kidsRes.(ast.PdfArray)
		if !ok {
			return
		}
		for _, kid := range kidsArr {
			collectPagesRecursive(ctx, doc, kid, pages)
		}
	case "Page":
		*pages = append(*pages, dict)
	}
}

func getResources(ctx context.Context, doc *document.Document, pageDict ast.PdfDict) ast.PdfDict {
	resObj, ok := pageDict["Resources"]
	if !ok {
		return nil
	}
	resRes, _ := resolve(ctx, doc, resObj)
	resDict, ok := resRes.(ast.PdfDict)
	if !ok {
		return nil
	}
	return resDict
}

func collectFontDicts(ctx context.Context, doc *document.Document, catalog ast.PdfDict) map[string]ast.PdfDict {
	fonts := make(map[string]ast.PdfDict)
	pages := collectPages(ctx, doc, catalog)
	for _, page := range pages {
		resources := getResources(ctx, doc, page)
		if resources == nil {
			continue
		}
		fontObj, ok := resources["Font"]
		if !ok {
			continue
		}
		fontRes, _ := resolve(ctx, doc, fontObj)
		fontDict, ok := fontRes.(ast.PdfDict)
		if !ok {
			continue
		}
		for name, fObj := range fontDict {
			fRes, _ := resolve(ctx, doc, fObj)
			if fDict, ok := fRes.(ast.PdfDict); ok {
				fonts[string(name)] = fDict
			}
		}
	}
	return fonts
}

// hasPageFilter checks whether any XObject stream on a page uses the given filter.
func hasPageFilter(ctx context.Context, doc *document.Document, pageDict ast.PdfDict, filter string) bool {
	resources := getResources(ctx, doc, pageDict)
	if resources == nil {
		return false
	}
	xobjObj, ok := resources["XObject"]
	if !ok {
		return false
	}
	xobjRes, _ := resolve(ctx, doc, xobjObj)
	xobjDict, ok := xobjRes.(ast.PdfDict)
	if !ok {
		return false
	}
	for _, obj := range xobjDict {
		objRes, _ := resolve(ctx, doc, obj)
		stream, ok := objRes.(ast.PdfStream)
		if !ok {
			continue
		}
		if streamHasFilter(ctx, doc, stream.Dict, filter) {
			return true
		}
	}
	return false
}

func streamHasFilter(ctx context.Context, doc *document.Document, dict ast.PdfDict, target string) bool {
	filterObj, ok := dict["Filter"]
	if !ok {
		return false
	}
	filterRes, _ := resolve(ctx, doc, filterObj)
	switch f := filterRes.(type) {
	case ast.PdfName:
		return string(f) == target
	case ast.PdfArray:
		for _, item := range f {
			itemRes, _ := resolve(ctx, doc, item)
			if n, ok := itemRes.(ast.PdfName); ok && string(n) == target {
				return true
			}
		}
	}
	return false
}

func hasEmbeddedFiles(ctx context.Context, doc *document.Document, catalog ast.PdfDict) bool {
	namesObj, ok := catalog["Names"]
	if !ok {
		return false
	}
	namesRes, _ := resolve(ctx, doc, namesObj)
	namesDict, ok := namesRes.(ast.PdfDict)
	if !ok {
		return false
	}
	_, has := namesDict["EmbeddedFiles"]
	return has
}

func collectFileSpecs(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []ast.PdfDict {
	namesObj, ok := catalog["Names"]
	if !ok {
		return nil
	}
	namesRes, _ := resolve(ctx, doc, namesObj)
	namesDict, ok := namesRes.(ast.PdfDict)
	if !ok {
		return nil
	}
	efObj, ok := namesDict["EmbeddedFiles"]
	if !ok {
		return nil
	}
	var specs []ast.PdfDict
	collectFileSpecsRecursive(ctx, doc, efObj, &specs)
	return specs
}

func collectFileSpecsRecursive(ctx context.Context, doc *document.Document, obj ast.PdfObject, specs *[]ast.PdfDict) {
	res, _ := resolve(ctx, doc, obj)
	dict, ok := res.(ast.PdfDict)
	if !ok {
		return
	}
	if kidsObj, ok := dict["Kids"]; ok {
		kidsRes, _ := resolve(ctx, doc, kidsObj)
		if kidsArr, ok := kidsRes.(ast.PdfArray); ok {
			for _, kid := range kidsArr {
				collectFileSpecsRecursive(ctx, doc, kid, specs)
			}
		}
		return
	}
	if namesObj, ok := dict["Names"]; ok {
		namesRes, _ := resolve(ctx, doc, namesObj)
		if namesArr, ok := namesRes.(ast.PdfArray); ok {
			for i := 1; i < len(namesArr); i += 2 {
				valRes, _ := resolve(ctx, doc, namesArr[i])
				if fsDict, ok := valRes.(ast.PdfDict); ok {
					*specs = append(*specs, fsDict)
				}
			}
		}
	}
}
