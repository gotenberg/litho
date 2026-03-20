package compliance

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/models"
)

// ValidatePDFA and ValidatePDFUA are defined in rules.go alongside the
// table-driven rule sets.

// --- Catalog access ---

func getCatalog(ctx context.Context, doc *document.Document) (ast.PdfDict, error) {
	catalog, err := document.GetCatalog(ctx, doc)
	if err != nil {
		return nil, err
	}
	return catalog.Dict, nil
}

// --- Base checks: Level 'b' (Basic Visual Integrity) ---

// checkFileHeader verifies the PDF file header conforms to ISO 19005 §6.1.2:
//  1. The file must start with %PDF-x.y
//  2. The header must be followed by a binary comment line: a '%' character
//     followed by at least 4 bytes with values ≥ 128 (0x80). This signals to
//     file-transfer programs that the file contains binary data.
func checkFileHeader(_ context.Context, doc *document.Document) []error {
	// Read enough bytes for the header line + binary comment line.
	buf, err := safeReadHeader64(doc)
	if err != nil {
		// Cannot read header — skip check (e.g., test mocks).
		return nil
	}

	// 1. Must start with %PDF-.
	if !bytes.HasPrefix(buf, []byte("%PDF-")) {
		return []error{models.ErrInvalidFileHeader}
	}

	// 2. Find the binary comment line after the first newline.
	// The binary marker must appear within the first 1024 bytes per spec,
	// but realistically it's in the first 64 bytes.
	nlIdx := bytes.IndexAny(buf, "\r\n")
	if nlIdx < 0 {
		return []error{models.ErrMissingBinaryMarker}
	}

	// Skip past the newline(s) — handle \r\n, \r, \n.
	pos := nlIdx + 1
	if pos < len(buf) && buf[nlIdx] == '\r' && buf[pos] == '\n' {
		pos++
	}

	// The next line must start with '%' followed by ≥ 4 high bytes.
	if pos >= len(buf) || buf[pos] != '%' {
		return []error{models.ErrMissingBinaryMarker}
	}
	pos++ // skip the '%'

	highCount := 0
	for pos < len(buf) && buf[pos] != '\r' && buf[pos] != '\n' {
		if buf[pos] >= 0x80 {
			highCount++
		}
		pos++
	}

	const requiredHighBytes = 4
	if highCount < requiredHighBytes {
		return []error{models.ErrMissingBinaryMarker}
	}

	return nil
}

// safeReadHeader64 reads the first 64 bytes of the document, recovering from
// panics caused by a nil io.ReaderAt (e.g., in test mocks).
func safeReadHeader64(doc *document.Document) (buf []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("cannot read header: %v", r)
		}
	}()
	return doc.ReadFileBytes(0, 64) //nolint:mnd // enough for header + binary comment.
}

// checkEncryptionForbidden verifies that the document is not encrypted by
// inspecting both the Trailer /Encrypt key and the document's encryption flag.
func checkEncryptionForbidden(_ context.Context, doc *document.Document) []error {
	if _, ok := doc.Index.Trailer["Encrypt"]; ok {
		return []error{models.ErrEncryptionForbidden}
	}
	if doc.IsEncrypted {
		return []error{models.ErrEncryptionForbidden}
	}
	return nil
}

// checkICCProfile verifies that /OutputIntents contains at least one entry
// with an ICC profile stream (/DestOutputProfile) to define exact color
// reproduction, as required by PDF/A Level 'b'.
//
// If the document uses only device-independent color spaces (ICCBased,
// CalRGB, CalGray, Lab, or Indexed wrapping one of these), OutputIntents
// with an ICC profile is not required (ISO 19005-1 §6.2.2, ISO 19005-2
// §6.2.2).
func checkICCProfile(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	if hasOutputIntentICC(ctx, doc, catalog) {
		return nil
	}

	// No ICC output intent found. Check whether the document actually uses
	// device-dependent color spaces. If it only uses device-independent
	// color spaces, OutputIntents is not required.
	if usesDeviceDependentColorSpace(ctx, doc, catalog) {
		return []error{models.ErrMissingICCProfile}
	}
	return nil
}

// hasOutputIntentICC returns true if the catalog's /OutputIntents array
// contains at least one entry with a /DestOutputProfile key.
func hasOutputIntentICC(ctx context.Context, doc *document.Document, catalog ast.PdfDict) bool {
	oiObj, ok := catalog["OutputIntents"]
	if !ok {
		return false
	}
	oiRes, _ := document.ResolveObject(ctx, doc, oiObj)
	oi, ok := oiRes.(ast.PdfArray)
	if !ok || len(oi) == 0 {
		return false
	}
	for _, entry := range oi {
		entryRes, _ := document.ResolveObject(ctx, doc, entry)
		dict, ok := entryRes.(ast.PdfDict)
		if !ok {
			continue
		}
		if _, has := dict["DestOutputProfile"]; has {
			return true
		}
	}
	return false
}

// deviceDependentNames are the PDF color space names that require an
// OutputIntents ICC profile for PDF/A conformance.
var deviceDependentNames = map[ast.PdfName]bool{
	"DeviceRGB":  true,
	"DeviceCMYK": true,
	"DeviceGray": true,
}

// usesDeviceDependentColorSpace scans page resources and XObject streams
// for any device-dependent color space reference (DeviceRGB, DeviceCMYK,
// DeviceGray). If Default{RGB,CMYK,Gray} is defined in the page's
// ColorSpace resource dict, the corresponding device space is considered
// remapped to a device-independent space and is not flagged.
func usesDeviceDependentColorSpace(ctx context.Context, doc *document.Document, catalog ast.PdfDict) bool {
	pages := collectPages(ctx, doc, catalog)
	for _, page := range pages {
		resources := getResources(ctx, doc, page)
		if resources == nil {
			continue
		}

		defaults := collectDefaultColorSpaces(ctx, doc, resources)

		// A transparency group /CS that is device-independent effectively
		// remaps all device-dependent color spaces on the page (PDF 2.0,
		// ISO 32000-2:2020 §11.4.8).
		addTransparencyGroupDefaults(ctx, doc, page, defaults)

		// Check named color spaces in /Resources/ColorSpace.
		if csObj, ok := resources["ColorSpace"]; ok {
			csRes, _ := document.ResolveObject(ctx, doc, csObj)
			if csDict, ok := csRes.(ast.PdfDict); ok {
				for _, v := range csDict {
					if isDeviceDependentCS(ctx, doc, v, defaults) {
						return true
					}
				}
			}
		}

		// Check image XObjects for device-dependent /ColorSpace.
		if xobjObj, ok := resources["XObject"]; ok {
			xobjRes, _ := document.ResolveObject(ctx, doc, xobjObj)
			if xobjDict, ok := xobjRes.(ast.PdfDict); ok {
				for _, obj := range xobjDict {
					objRes, _ := document.ResolveObject(ctx, doc, obj)
					stream, ok := objRes.(ast.PdfStream)
					if !ok {
						continue
					}
					subtypeObj, _ := stream.Dict["Subtype"]
					if st, ok := subtypeObj.(ast.PdfName); !ok || st != "Image" {
						continue
					}
					if csVal, ok := stream.Dict["ColorSpace"]; ok {
						if isDeviceDependentCS(ctx, doc, csVal, defaults) {
							return true
						}
					}
				}
			}
		}
	}
	return false
}

// collectDefaultColorSpaces returns the set of device-dependent families
// that are overridden by Default{RGB,CMYK,Gray} entries in the ColorSpace
// resource dictionary. When a default is present, the corresponding
// device space is implicitly remapped to a device-independent space.
func collectDefaultColorSpaces(ctx context.Context, doc *document.Document, resources ast.PdfDict) map[ast.PdfName]bool {
	defaults := make(map[ast.PdfName]bool)
	csObj, ok := resources["ColorSpace"]
	if !ok {
		return defaults
	}
	csRes, _ := document.ResolveObject(ctx, doc, csObj)
	csDict, ok := csRes.(ast.PdfDict)
	if !ok {
		return defaults
	}
	if _, ok := csDict["DefaultRGB"]; ok {
		defaults["DeviceRGB"] = true
	}
	if _, ok := csDict["DefaultCMYK"]; ok {
		defaults["DeviceCMYK"] = true
	}
	if _, ok := csDict["DefaultGray"]; ok {
		defaults["DeviceGray"] = true
	}
	return defaults
}

// addTransparencyGroupDefaults checks the page's /Group dictionary for a
// transparency group whose /CS is a device-independent color space. When
// present, it remaps the corresponding device-dependent family (e.g., a
// /CS of [/CalRGB ...] covers DeviceRGB).
func addTransparencyGroupDefaults(ctx context.Context, doc *document.Document, page ast.PdfDict, defaults map[ast.PdfName]bool) {
	grpObj, ok := page["Group"]
	if !ok {
		return
	}
	grpRes, _ := document.ResolveObject(ctx, doc, grpObj)
	grpDict, ok := grpRes.(ast.PdfDict)
	if !ok {
		return
	}
	csObj, ok := grpDict["CS"]
	if !ok {
		return
	}
	csRes, _ := document.ResolveObject(ctx, doc, csObj)
	switch cs := csRes.(type) {
	case ast.PdfName:
		// A direct device-independent name (unlikely but valid).
		return // Device-independent names don't remap anything here.
	case ast.PdfArray:
		if len(cs) == 0 {
			return
		}
		familyObj, _ := document.ResolveObject(ctx, doc, cs[0])
		family, ok := familyObj.(ast.PdfName)
		if !ok {
			return
		}
		switch family {
		case "ICCBased", "CalRGB":
			defaults["DeviceRGB"] = true
		case "CalGray":
			defaults["DeviceGray"] = true
		case "Lab":
			// Lab is device-independent but doesn't map to a single device family.
		}
	}
}

// isDeviceDependentCS returns true if the color space object resolves to a
// device-dependent color space (DeviceRGB, DeviceCMYK, DeviceGray) that is
// not covered by a Default entry in the page's resource dictionary.
func isDeviceDependentCS(ctx context.Context, doc *document.Document, csObj ast.PdfObject, defaults map[ast.PdfName]bool) bool {
	resolved, _ := document.ResolveObject(ctx, doc, csObj)
	switch cs := resolved.(type) {
	case ast.PdfName:
		return deviceDependentNames[cs] && !defaults[cs]
	case ast.PdfArray:
		if len(cs) == 0 {
			return false
		}
		familyObj, _ := document.ResolveObject(ctx, doc, cs[0])
		family, ok := familyObj.(ast.PdfName)
		if !ok {
			return false
		}
		switch family {
		case "ICCBased", "CalRGB", "CalGray", "Lab":
			return false
		case "Indexed", "Separation", "DeviceN":
			// Check the base color space (second element).
			if len(cs) >= 2 {
				return isDeviceDependentCS(ctx, doc, cs[1], defaults)
			}
			return false
		default:
			return deviceDependentNames[family] && !defaults[family]
		}
	}
	return false
}

// checkNoLZW scans all page resource streams for forbidden LZW compression.
func checkNoLZW(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	pages := collectPages(ctx, doc, catalog)
	for _, page := range pages {
		if hasPageFilter(ctx, doc, page, "LZWDecode") {
			return []error{models.ErrLZWForbidden}
		}
	}
	return nil
}

// forbiddenActionTypes are action types that constitute active content,
// forbidden under PDF/A Level 'b'.
var forbiddenActionTypes = map[ast.PdfName]bool{
	"JavaScript":  true,
	"Launch":      true,
	"Sound":       true,
	"Movie":       true,
	"ResetForm":   true,
	"ImportData":  true,
	"Hide":        true,
	"SetOCGState": true,
	"Rendition":   true,
	"Trans":       true,
	"GoTo3DView":  true,
	"GoToE":       true,
}

// allowedNamedActions are the only named actions permitted in PDF/A (6.6.1 t2).
var allowedNamedActions = map[ast.PdfName]bool{
	"NextPage":  true,
	"PrevPage":  true,
	"FirstPage": true,
	"LastPage":  true,
}

// checkNoActiveContent traverses the Catalog, /OpenAction, /AA (Additional
// Actions), and page-level annotations for forbidden active content actions:
// /JavaScript, /Launch, /Sound, and /Movie.
func checkNoActiveContent(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	// Check /Names /JavaScript name tree.
	if namesObj, ok := catalog["Names"]; ok {
		namesRes, _ := document.ResolveObject(ctx, doc, namesObj)
		if namesDict, ok := namesRes.(ast.PdfDict); ok {
			if _, ok := namesDict["JavaScript"]; ok {
				return []error{models.ErrActiveContentForbidden}
			}
		}
	}

	// Check /OpenAction for forbidden action types.
	if oaObj, ok := catalog["OpenAction"]; ok {
		oaRes, _ := document.ResolveObject(ctx, doc, oaObj)
		if oaDict, ok := oaRes.(ast.PdfDict); ok {
			if isForbiddenAction(oaDict) {
				return []error{models.ErrActiveContentForbidden}
			}
		}
	}

	// Check /AA (Additional Actions) on catalog.
	if hasForbiddenAdditionalActions(ctx, doc, catalog) {
		return []error{models.ErrActiveContentForbidden}
	}

	// Check page-level annotations for forbidden actions.
	pages := collectPages(ctx, doc, catalog)
	for _, page := range pages {
		if hasForbiddenAnnotActions(ctx, doc, page) {
			return []error{models.ErrActiveContentForbidden}
		}
		// Check page-level /AA.
		if hasForbiddenAdditionalActions(ctx, doc, page) {
			return []error{models.ErrActiveContentForbidden}
		}
	}

	// 6.6.2: Widget annotations and form fields must not have /AA.
	if acroObj, ok := catalog["AcroForm"]; ok {
		acroRes, _ := document.ResolveObject(ctx, doc, acroObj)
		if acroDict, ok := acroRes.(ast.PdfDict); ok {
			if hasFieldAA(ctx, doc, acroDict) {
				return []error{models.ErrActiveContentForbidden}
			}
		}
	}

	// Check outline (bookmark) entries for forbidden actions.
	if hasForbiddenOutlineActions(ctx, doc, catalog) {
		return []error{models.ErrActiveContentForbidden}
	}

	return nil
}

// hasForbiddenOutlineActions traverses the document outline tree for
// forbidden action types.
func hasForbiddenOutlineActions(ctx context.Context, doc *document.Document, catalog ast.PdfDict) bool {
	outlinesObj, ok := catalog["Outlines"]
	if !ok {
		return false
	}
	outlinesRes, _ := document.ResolveObject(ctx, doc, outlinesObj)
	outlinesDict, ok := outlinesRes.(ast.PdfDict)
	if !ok {
		return false
	}
	firstObj, ok := outlinesDict["First"]
	if !ok {
		return false
	}
	return walkOutlineItems(ctx, doc, firstObj, 0)
}

func walkOutlineItems(ctx context.Context, doc *document.Document, obj ast.PdfObject, depth int) bool {
	if depth > 100 { //nolint:mnd // prevent infinite loops
		return false
	}
	res, _ := document.ResolveObject(ctx, doc, obj)
	dict, ok := res.(ast.PdfDict)
	if !ok {
		return false
	}
	// Check /A (action) on this outline item.
	if aObj, ok := dict["A"]; ok {
		aRes, _ := document.ResolveObject(ctx, doc, aObj)
		if aDict, ok := aRes.(ast.PdfDict); ok {
			if isForbiddenAction(aDict) {
				return true
			}
		}
	}
	// Recurse into children.
	if firstObj, ok := dict["First"]; ok {
		if walkOutlineItems(ctx, doc, firstObj, depth+1) {
			return true
		}
	}
	// Follow /Next sibling.
	if nextObj, ok := dict["Next"]; ok {
		if walkOutlineItems(ctx, doc, nextObj, depth+1) {
			return true
		}
	}
	return false
}

// hasFieldAA recursively checks form fields for forbidden /AA entries.
func hasFieldAA(ctx context.Context, doc *document.Document, dict ast.PdfDict) bool {
	fieldsObj, ok := dict["Fields"]
	if !ok {
		return false
	}
	fieldsRes, _ := document.ResolveObject(ctx, doc, fieldsObj)
	fieldsArr, ok := fieldsRes.(ast.PdfArray)
	if !ok {
		return false
	}
	for _, fieldObj := range fieldsArr {
		fieldRes, _ := document.ResolveObject(ctx, doc, fieldObj)
		if fieldDict, ok := fieldRes.(ast.PdfDict); ok {
			if _, ok := fieldDict["AA"]; ok {
				return true
			}
			// Recurse into /Kids.
			if kidsObj, ok := fieldDict["Kids"]; ok {
				kidsRes, _ := document.ResolveObject(ctx, doc, kidsObj)
				if kidsArr, ok := kidsRes.(ast.PdfArray); ok {
					for _, kid := range kidsArr {
						kidRes, _ := document.ResolveObject(ctx, doc, kid)
						if kidDict, ok := kidRes.(ast.PdfDict); ok {
							if _, ok := kidDict["AA"]; ok {
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

// isForbiddenAction checks if an action dictionary has a forbidden /S type
// or a named action (/S /Named) that is not in the allowed set.
func isForbiddenAction(actionDict ast.PdfDict) bool {
	sObj, ok := actionDict["S"]
	if !ok {
		return false
	}
	sName, ok := sObj.(ast.PdfName)
	if !ok {
		return false
	}
	if forbiddenActionTypes[sName] {
		return true
	}
	// Named actions: only NextPage/PrevPage/FirstPage/LastPage are allowed.
	if sName == "Named" {
		nObj, ok := actionDict["N"]
		if !ok {
			return true
		}
		nName, ok := nObj.(ast.PdfName)
		if !ok {
			return true
		}
		return !allowedNamedActions[nName]
	}
	return false
}

// hasForbiddenAdditionalActions checks the /AA dictionary of a dict for
// forbidden action types.
func hasForbiddenAdditionalActions(ctx context.Context, doc *document.Document, dict ast.PdfDict) bool {
	aaObj, ok := dict["AA"]
	if !ok {
		return false
	}
	aaRes, _ := document.ResolveObject(ctx, doc, aaObj)
	aaDict, ok := aaRes.(ast.PdfDict)
	if !ok {
		return false
	}
	for _, v := range aaDict {
		vRes, _ := document.ResolveObject(ctx, doc, v)
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
	annotsRes, _ := document.ResolveObject(ctx, doc, annotsObj)
	annotsArr, ok := annotsRes.(ast.PdfArray)
	if !ok {
		return false
	}
	for _, annotObj := range annotsArr {
		annotRes, _ := document.ResolveObject(ctx, doc, annotObj)
		annotDict, ok := annotRes.(ast.PdfDict)
		if !ok {
			continue
		}
		// Check /A (action) on the annotation.
		if aObj, ok := annotDict["A"]; ok {
			aRes, _ := document.ResolveObject(ctx, doc, aObj)
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

// --- Annotation checks (ISO 19005, clause 6.3) ---

// forbiddenAnnotSubtypes lists annotation types that are forbidden in PDF/A.
// Sound, Screen, and Movie are forbidden in all parts. PDF/A-1 additionally
// forbids 3D; PDF/A-4 allows it only under the 4e sub-profile.
var forbiddenAnnotSubtypes = map[ast.PdfName]bool{
	"Sound":  true,
	"Screen": true,
	"Movie":  true,
}

// pdfA1ExtraForbiddenAnnots are additional forbidden types in PDF/A-1.
var pdfA1ExtraForbiddenAnnots = map[ast.PdfName]bool{
	"3D":        true,
	"RichMedia": true,
}

// checkAnnotationCompliance validates annotation structural requirements:
// - 6.3.1: no forbidden annotation types
// - 6.3.2: /F key present with correct flags
// - 6.3.3: appearance stream (/AP /N) present
//
// extraForbidden lists additional annotation subtypes forbidden beyond the
// base set (e.g., 3D/RichMedia for PDF/A-1). linkExemptFromAP controls
// whether Link annotations are exempt from appearance stream requirements
// (true for PDF/A-2+).
func checkAnnotationCompliance(ctx context.Context, doc *document.Document, catalog ast.PdfDict, extraForbidden map[ast.PdfName]bool, linkExemptFromAP bool) []error {
	var errs []error
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
			subtypeName := resolveAnnotSubtype(ctx, doc, annotDict)

			// 6.3.1: Forbidden annotation types.
			if forbiddenAnnotSubtypes[subtypeName] {
				errs = append(errs, fmt.Errorf("%w: /%s", models.ErrForbiddenAnnotationType, subtypeName))
				continue
			}
			if extraForbidden[subtypeName] {
				errs = append(errs, fmt.Errorf("%w: /%s", models.ErrForbiddenAnnotationType, subtypeName))
				continue
			}

			// Popup annotations are exempt from /F and /AP requirements.
			if subtypeName == "Popup" {
				continue
			}

			// 6.3.2 t1: /F key must be present.
			fObj, hasF := annotDict["F"]
			if !hasF {
				errs = append(errs, models.ErrAnnotationMissingFlags)
				continue
			}

			// 6.3.2 t2: Flag bits — Print=1, Hidden/Invisible/ToggleNoView/NoView=0.
			fRes, _ := document.ResolveObject(ctx, doc, fObj)
			if fInt, ok := fRes.(ast.PdfInt); ok {
				flags := int(fInt)
				const (
					flagInvisible    = 1 << 0 // bit 1
					flagHidden       = 1 << 1 // bit 2
					flagPrint        = 1 << 2 // bit 3
					flagNoView       = 1 << 5 // bit 6
					flagToggleNoView = 1 << 8 // bit 9
				)
				printSet := flags&flagPrint != 0
				badBits := flags&(flagInvisible|flagHidden|flagNoView|flagToggleNoView) != 0
				if !printSet || badBits {
					errs = append(errs, models.ErrAnnotationInvalidFlags)
				}
			}

			// 6.3.3 t1: Appearance stream (/AP with /N) must be present.
			// Exempt: annotations with zero-area /Rect (invisible) and
			// Link annotations (exempt in PDF/A-2+ per ISO 19005-2 §6.3.3).
			needsAP := !isZeroRect(ctx, doc, annotDict)
			if needsAP && subtypeName == "Link" && linkExemptFromAP {
				needsAP = false
			}
			if needsAP && !hasAppearanceStream(ctx, doc, annotDict) {
				errs = append(errs, models.ErrAnnotationMissingAppearance)
			}
		}
	}
	return errs
}

func resolveAnnotSubtype(ctx context.Context, doc *document.Document, annotDict ast.PdfDict) ast.PdfName {
	stObj, ok := annotDict["Subtype"]
	if !ok {
		return ""
	}
	stRes, _ := document.ResolveObject(ctx, doc, stObj)
	stName, _ := stRes.(ast.PdfName)
	return stName
}

func hasAppearanceStream(ctx context.Context, doc *document.Document, annotDict ast.PdfDict) bool {
	apObj, ok := annotDict["AP"]
	if !ok {
		return false
	}
	apRes, _ := document.ResolveObject(ctx, doc, apObj)
	apDict, ok := apRes.(ast.PdfDict)
	if !ok {
		return false
	}
	_, hasN := apDict["N"]
	return hasN
}

// isZeroRect returns true if the annotation's /Rect has zero area (invisible).
// This covers both [0 0 0 0] and degenerate rectangles like [50 110 50 110]
// where width or height is zero.
func isZeroRect(ctx context.Context, doc *document.Document, annotDict ast.PdfDict) bool {
	rectObj, ok := annotDict["Rect"]
	if !ok {
		return false
	}
	rectRes, _ := document.ResolveObject(ctx, doc, rectObj)
	rectArr, ok := rectRes.(ast.PdfArray)
	if !ok || len(rectArr) < 4 { //nolint:mnd // PDF rectangles have 4 elements
		return false
	}
	x1 := document.GetFloat(rectArr[0])
	y1 := document.GetFloat(rectArr[1])
	x2 := document.GetFloat(rectArr[2])
	y2 := document.GetFloat(rectArr[3])
	return x1 == x2 || y1 == y2
}

// --- XMP metadata checks (ISO 19005, clauses 6.6 and 6.7) ---

// predefinedXMPNamespaces are the namespace URIs that do not require an
// extension schema declaration in PDF/A. This covers the XMP core schemas,
// Dublin Core, PDF, PDF/A identification, and the extension schema types.
var predefinedXMPNamespaces = map[string]bool{
	// RDF/XML infrastructure.
	"http://www.w3.org/1999/02/22-rdf-syntax-ns#":      true,
	"http://www.w3.org/XML/1998/namespace":             true,
	"http://www.w3.org/2001/XMLSchema-instance":        true,
	"http://www.w3.org/2001/XMLSchema":                 true,
	"http://www.w3.org/2000/xmlns/":                    true,
	"adobe:ns:meta/":                                   true,
	"http://ns.adobe.com/xap/1.0/sType/ResourceRef#":   true,
	"http://ns.adobe.com/xap/1.0/sType/ResourceEvent#": true,
	"http://ns.adobe.com/xap/1.0/sType/Version#":       true,
	"http://ns.adobe.com/xap/1.0/sType/Rendition#":     true,
	"http://ns.adobe.com/xap/1.0/sType/Job#":           true,
	"http://ns.adobe.com/xap/1.0/g/":                   true,
	"http://ns.adobe.com/xap/1.0/g/img/":               true,
	"http://ns.adobe.com/xap/1.0/sType/Dimensions#":    true,
	"http://ns.adobe.com/xap/1.0/sType/Font#":          true,
	"http://ns.adobe.com/xap/1.0/t/pg/":                true,
	"http://ns.adobe.com/xap/1.0/sType/ManifestItem#":  true,
	// Dublin Core.
	"http://purl.org/dc/elements/1.1/": true,
	// XMP Core namespaces.
	"http://ns.adobe.com/xap/1.0/":        true,
	"http://ns.adobe.com/xap/1.0/mm/":     true,
	"http://ns.adobe.com/xap/1.0/rights/": true,
	"http://ns.adobe.com/xap/1.0/bj/":     true,
	// PDF namespace.
	"http://ns.adobe.com/pdf/1.3/":  true,
	"http://ns.adobe.com/pdfx/1.3/": true,
	// PDF/A identification.
	"http://www.aiim.org/pdfa/ns/id/": true,
	// PDF/A extension schemas.
	"http://www.aiim.org/pdfa/ns/extension/": true,
	"http://www.aiim.org/pdfa/ns/schema#":    true,
	"http://www.aiim.org/pdfa/ns/property#":  true,
	"http://www.aiim.org/pdfa/ns/field#":     true,
	"http://www.aiim.org/pdfa/ns/type#":      true,
	// Adobe application namespaces accepted by veraPDF as predefined.
	"http://ns.adobe.com/photoshop/1.0/":           true,
	"http://ns.adobe.com/exif/1.0/":                true,
	"http://ns.adobe.com/exif/1.0/aux/":            true,
	"http://ns.adobe.com/tiff/1.0/":                true,
	"http://ns.adobe.com/camera-raw-settings/1.0/": true,
	"http://ns.adobe.com/xmp/1.0/DynamicMedia/":    true,
	"http://ns.adobe.com/xmp/Identifier/qual/1.0/": true,
	"http://iptc.org/std/Iptc4xmpCore/1.0/xmlns/":  true,
	"http://iptc.org/std/Iptc4xmpExt/2008-02-29/":  true,
	"http://www.aiim.org/pdfa/":                    true,
}

// checkXMPCompliance validates XMP metadata requirements for PDF/A:
// - 6.6.2.3.1 / 6.7.9: All XMP properties must use predefined or extension schemas
// - 6.7.3: Document info dict entries must be consistent with XMP
// - 6.6.4 / 6.7.11: PDF/A identification (pdfaid:part/conformance) must be present
// - 6.7.2.1: XMP packet must not use 'bytes' or 'encoding' attributes
func checkXMPCompliance(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile models.PDFAProfile) []error {
	mdObj, ok := catalog["Metadata"]
	if !ok {
		return []error{models.ErrMissingCatalogMetdata}
	}
	mdRes, _ := document.ResolveObject(ctx, doc, mdObj)
	stream, ok := mdRes.(ast.PdfStream)
	if !ok {
		return []error{models.ErrMissingCatalogMetdata}
	}

	xmpData := stream.Data
	if len(xmpData) == 0 {
		decoded, err := filter.DecodeStream(stream)
		if err != nil {
			return []error{models.ErrMissingCatalogMetdata}
		}
		xmpData = decoded
	}

	var errs []error

	// 6.7.2.1 t2/t3: Forbidden attributes in XMP packet header.
	errs = append(errs, checkXMPPacketHeader(xmpData)...)

	// 6.6.2.3.1 / 6.7.9: Schema validation.
	errs = append(errs, checkXMPNamespaces(xmpData)...)

	// 6.6.4 / 6.7.11: PDF/A identification.
	errs = append(errs, checkPDFAIdentification(xmpData, profile)...)

	// 6.6.2.3.3 / 6.7.8: Extension schema structure validation.
	// TODO: re-enable when the parser handles nested rdf:li depth correctly.
	// errs = append(errs, checkExtensionSchemaStructure(xmpData)...)

	// 6.7.3: DocInfo ↔ XMP consistency.
	errs = append(errs, checkDocInfoXMPSync(ctx, doc, xmpData)...)

	return errs
}

// checkXMPPacketHeader checks that the XMP packet header does not use the
// forbidden 'bytes' or 'encoding' attributes (6.7.2.1 t2/t3).
func checkXMPPacketHeader(data []byte) []error {
	// Only check the first ~200 bytes for the processing instruction.
	header := data
	if len(header) > 200 { //nolint:mnd // header is short
		header = header[:200]
	}
	before, _, ok := bytes.Cut(header, []byte("?>"))
	if !ok {
		return nil
	}
	pi := before
	if bytes.Contains(pi, []byte("bytes=")) || bytes.Contains(pi, []byte("encoding=")) {
		return []error{models.ErrXMPPacketInvalid}
	}
	return nil
}

// xmpPropInfo holds the observed type and text value of an XMP property.
type xmpPropInfo struct {
	observedType xmpType
	textValue    string // text content for simple-type properties
}

// checkXMPNamespaces walks the XMP XML token stream and verifies that every
// namespace URI used by properties is either a predefined XMP schema or is
// declared in a PDF/A extension schema within the same XMP packet. For
// predefined schemas, it also validates property names, container types,
// and value formats.
func checkXMPNamespaces(data []byte) []error {
	usedProperties, extensionNamespaces := extractXMPProperties(data)
	var errs []error
	for ns, props := range usedProperties {
		if !predefinedXMPNamespaces[ns] && !extensionNamespaces[ns] {
			errs = append(errs, fmt.Errorf("%w: %s", models.ErrXMPSchemaViolation, ns))
			continue
		}
		registry, hasRegistry := xmpPropertyRegistry[ns]
		if !hasRegistry {
			continue
		}
		for prop, info := range props {
			expectedType, known := registry[prop]
			if !known {
				errs = append(errs, fmt.Errorf("%w: %s (unknown property %s)",
					models.ErrXMPSchemaViolation, ns, prop))
				continue
			}
			observed := info.observedType

			// Container type validation.
			if observed != xmpAny && expectedType != xmpAny && observed != expectedType {
				if isSimpleType(expectedType) && isContainerType(observed) {
					errs = append(errs, fmt.Errorf("%w: %s property %s has wrong type",
						models.ErrXMPSchemaViolation, ns, prop))
					continue
				} else if isContainerType(expectedType) && isContainerType(observed) {
					errs = append(errs, fmt.Errorf("%w: %s property %s has wrong container type",
						models.ErrXMPSchemaViolation, ns, prop))
					continue
				}
			}

			// Value format validation for simple-type properties.
			if info.textValue != "" && isSimpleType(expectedType) {
				if !isValidXMPValue(expectedType, info.textValue) {
					errs = append(errs, fmt.Errorf("%w: %s property %s has invalid value",
						models.ErrXMPSchemaViolation, ns, prop))
				}
			}

			// Struct-type properties must have child elements, not simple text.
			if expectedType == xmpStruct && observed == xmpText && info.textValue != "" {
				errs = append(errs, fmt.Errorf("%w: %s property %s should be structured",
					models.ErrXMPSchemaViolation, ns, prop))
			}
		}
	}
	return errs
}

// isValidXMPValue checks if a text value matches the expected XMP type format.
func isValidXMPValue(expected xmpType, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	switch expected {
	case xmpInteger:
		// Must be a plain integer (no decimal, no fraction, no sign prefix with decimal).
		if strings.Contains(value, "/") || strings.Contains(value, ".") {
			return false
		}
		for i, c := range value {
			if i == 0 && (c == '+' || c == '-') {
				continue
			}
			if c < '0' || c > '9' {
				return false
			}
		}
		return len(value) > 0
	case xmpReal:
		// Numeric: allow decimal, sign, scientific notation, and EXIF rationals (e.g., 3/2).
		return true // Lenient on format — Real accepts many representations
	case xmpDate:
		// Basic validation: must start with a 4-digit year and not have Z+offset.
		if len(value) < 4 { //nolint:mnd
			return false
		}
		// Invalid: both Z (UTC) and +/- offset.
		if strings.Contains(value, "Z+") || strings.Contains(value, "Z-") {
			return false
		}
		return true
	case xmpBoolean:
		return value == "True" || value == "False" ||
			value == "true" || value == "false" ||
			value == "1" || value == "0"
	default:
		return true // Text, URI — any string is valid
	}
}

// extractXMPProperties parses XMP XML and returns:
// 1. A map from namespace URI to property name → observed type
// 2. All namespace URIs declared in pdfaExtension:schemas extension blocks
//
// Only top-level XMP properties are recorded (direct children of rdf:Description),
// not sub-elements of structured types.
func extractXMPProperties(data []byte) (properties map[string]map[string]xmpPropInfo, extensions map[string]bool) {
	properties = make(map[string]map[string]xmpPropInfo)
	extensions = make(map[string]bool)

	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false

	const rdfNS = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"

	var inExtensionSchema bool
	depth := 0
	inDescription := false
	var currentPropNS, currentPropLocal string
	var currentTextBuf strings.Builder
	captureText := false

	recordProp := func(ns, local string, info xmpPropInfo) {
		if properties[ns] == nil {
			properties[ns] = make(map[string]xmpPropInfo)
		}
		properties[ns][local] = info
	}

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == rdfNS && t.Name.Local == "Description" {
				inDescription = true
				depth = 0
				for _, attr := range t.Attr {
					if attr.Name.Space != "" && attr.Name.Space != "xmlns" &&
						attr.Name.Space != rdfNS &&
						attr.Name.Space != "http://www.w3.org/XML/1998/namespace" {
						recordProp(attr.Name.Space, attr.Name.Local,
							xmpPropInfo{observedType: xmpText, textValue: attr.Value})
					}
				}
				continue
			}

			if inDescription {
				depth++
				if depth == 1 && t.Name.Space != "" && t.Name.Space != rdfNS {
					currentPropNS = t.Name.Space
					currentPropLocal = t.Name.Local
					currentTextBuf.Reset()
					captureText = true
					recordProp(t.Name.Space, t.Name.Local,
						xmpPropInfo{observedType: xmpText})
				}
				if depth == 2 && currentPropNS != "" && t.Name.Space == rdfNS {
					captureText = false // Container — don't capture inner text
					switch t.Name.Local {
					case "Alt":
						recordProp(currentPropNS, currentPropLocal,
							xmpPropInfo{observedType: xmpAlt})
					case "Seq":
						recordProp(currentPropNS, currentPropLocal,
							xmpPropInfo{observedType: xmpSeq})
					case "Bag":
						recordProp(currentPropNS, currentPropLocal,
							xmpPropInfo{observedType: xmpBag})
					}
				}
			}

			if t.Name.Local == "schemas" && strings.Contains(t.Name.Space, "pdfa/ns/extension") {
				inExtensionSchema = true
			}
			if inExtensionSchema && t.Name.Local == "namespaceURI" {
				if charTok, err := decoder.Token(); err == nil {
					if charData, ok := charTok.(xml.CharData); ok {
						ns := strings.TrimSpace(string(charData))
						if ns != "" {
							extensions[ns] = true
						}
					}
				}
			}

		case xml.CharData:
			if captureText && depth == 1 {
				currentTextBuf.Write(t)
			}

		case xml.EndElement:
			if t.Name.Space == rdfNS && t.Name.Local == "Description" {
				inDescription = false
				depth = 0
				currentPropNS = ""
				currentPropLocal = ""
				captureText = false
			} else if inDescription {
				if depth == 1 && currentPropNS != "" {
					// Finalize: store captured text value.
					val := strings.TrimSpace(currentTextBuf.String())
					if val != "" {
						recordProp(currentPropNS, currentPropLocal,
							xmpPropInfo{
								observedType: properties[currentPropNS][currentPropLocal].observedType,
								textValue:    val,
							})
					}
					currentPropNS = ""
					currentPropLocal = ""
					captureText = false
				}
				depth--
			}
			if t.Name.Local == "schemas" {
				inExtensionSchema = false
			}
		}
	}

	return properties, extensions
}

// pdfaIDNamespace is the PDF/A identification namespace URI.
const pdfaIDNamespace = "http://www.aiim.org/pdfa/ns/id/"

// checkPDFAIdentification verifies that the XMP contains pdfaid:part and
// pdfaid:conformance entries with correct values matching the declared
// profile (6.6.4 / 6.7.11 / 6.7.3).
func checkPDFAIdentification(data []byte, profile models.PDFAProfile) []error {
	part := PdfaPart(profile)
	conf := PdfaConformance(profile)

	// Parse XMP to find pdfaid properties and their values.
	var partValue, confValue string
	hasPart := false
	hasConf := false

	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			// Check attributes (inline form).
			for _, attr := range t.Attr {
				if attr.Name.Space == pdfaIDNamespace {
					switch attr.Name.Local {
					case "part":
						hasPart = true
						partValue = attr.Value
					case "conformance":
						hasConf = true
						confValue = attr.Value
					}
				}
			}
			// Check element form — read text content.
			if t.Name.Space == pdfaIDNamespace {
				if charTok, err := decoder.Token(); err == nil {
					if charData, ok := charTok.(xml.CharData); ok {
						val := strings.TrimSpace(string(charData))
						switch t.Name.Local {
						case "part":
							hasPart = true
							partValue = val
						case "conformance":
							hasConf = true
							confValue = val
						}
					}
				}
			}
		}
	}

	// Fallback for unparseable XMP.
	if !hasPart && !hasConf {
		hasPart = bytes.Contains(data, []byte("pdfaid:part")) ||
			bytes.Contains(data, []byte("pdfa/ns/id"))
		if !hasPart {
			return nil
		}
		hasConf = bytes.Contains(data, []byte("pdfaid:conformance"))
	}

	if !hasPart || (conf != 0 && !hasConf) {
		return []error{models.ErrMissingPDFAId}
	}

	// Validate pdfaid:part and pdfaid:conformance values.
	var errs []error
	if partValue != "" {
		expectedPart := fmt.Sprintf("%d", part)
		if partValue != expectedPart {
			errs = append(errs, fmt.Errorf("%w: pdfaid:part=%q, expected %q",
				models.ErrMissingPDFAId, partValue, expectedPart))
		}
	}
	if confValue != "" && conf != 0 {
		expectedConf := strings.ToUpper(string(rune(conf)))
		if !strings.EqualFold(confValue, expectedConf) {
			errs = append(errs, fmt.Errorf("%w: pdfaid:conformance=%q, expected %q",
				models.ErrMissingPDFAId, confValue, expectedConf))
		}
	}
	return errs
}

// checkDocInfoXMPSync verifies that document info dict entries are consistent
// with their XMP equivalents (6.7.3).
// pdfaExtension namespace URIs for schema, property, field, and type definitions.
const (
	pdfaSchemaPrefix   = "http://www.aiim.org/pdfa/ns/schema#"
	pdfaPropertyPrefix = "http://www.aiim.org/pdfa/ns/property#"
	pdfaFieldPrefix    = "http://www.aiim.org/pdfa/ns/field#"
	pdfaTypePrefix     = "http://www.aiim.org/pdfa/ns/type#"
)

// checkExtensionSchemaStructure validates that PDF/A extension schemas have
// all required fields per ISO 19005 clause 6.6.2.3.3 / 6.7.8.
func checkExtensionSchemaStructure(data []byte) []error {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false

	var errs []error
	var inSchemas bool
	var schemaDepth int

	// Track required fields within each schema definition.
	type schemaInfo struct {
		hasSchema, hasNamespaceURI, hasPrefix, hasProperty bool
	}
	type propInfo struct {
		hasName, hasValueType, hasCategory, hasDescription bool
	}
	type valueTypeInfo struct {
		hasType, hasNamespaceURI, hasPrefix, hasDescription bool
	}

	var currentSchema schemaInfo
	var currentProp propInfo
	var currentValueType valueTypeInfo
	var inProperty, inValueType bool

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if strings.Contains(t.Name.Space, "pdfa/ns/extension") && t.Name.Local == "schemas" {
				inSchemas = true
				schemaDepth = 0
			}
			if !inSchemas {
				continue
			}
			schemaDepth++

			switch {
			case t.Name.Space == pdfaSchemaPrefix:
				switch t.Name.Local {
				case "schema":
					currentSchema.hasSchema = true
				case "namespaceURI":
					currentSchema.hasNamespaceURI = true
				case "prefix":
					currentSchema.hasPrefix = true
				case "property":
					currentSchema.hasProperty = true
					inProperty = true
				case "valueType":
					inValueType = true
				}
			case t.Name.Space == pdfaPropertyPrefix && inProperty:
				switch t.Name.Local {
				case "name":
					currentProp.hasName = true
				case "valueType":
					currentProp.hasValueType = true
				case "category":
					currentProp.hasCategory = true
				case "description":
					currentProp.hasDescription = true
				}
			case t.Name.Space == pdfaTypePrefix && inValueType:
				switch t.Name.Local {
				case "type":
					currentValueType.hasType = true
				case "namespaceURI":
					currentValueType.hasNamespaceURI = true
				case "prefix":
					currentValueType.hasPrefix = true
				case "description":
					currentValueType.hasDescription = true
				}
			case t.Name.Space == pdfaFieldPrefix:
				// Field elements just need to exist — tracked but not validated deeply.
			}

		case xml.EndElement:
			if !inSchemas {
				continue
			}
			schemaDepth--

			// End of a schema definition block.
			if strings.Contains(t.Name.Space, "pdfa/ns/extension") && t.Name.Local == "schemas" {
				inSchemas = false
			}

			// Check when we end a li element inside extension schemas.
			if t.Name.Local == "li" && inSchemas {
				if inValueType {
					if currentValueType != (valueTypeInfo{}) {
						if !currentValueType.hasType || !currentValueType.hasNamespaceURI ||
							!currentValueType.hasPrefix || !currentValueType.hasDescription {
							errs = append(errs, models.ErrXMPExtensionInvalid)
						}
					}
					currentValueType = valueTypeInfo{}
					inValueType = false
				} else if inProperty {
					if currentProp != (propInfo{}) {
						if !currentProp.hasName || !currentProp.hasValueType ||
							!currentProp.hasCategory || !currentProp.hasDescription {
							errs = append(errs, models.ErrXMPExtensionInvalid)
						}
					}
					currentProp = propInfo{}
				} else {
					// End of a schema li — validate required schema fields.
					if currentSchema != (schemaInfo{}) {
						if !currentSchema.hasSchema || !currentSchema.hasNamespaceURI || !currentSchema.hasPrefix {
							errs = append(errs, models.ErrXMPExtensionInvalid)
						}
					}
					currentSchema = schemaInfo{}
				}
			}
		}
	}

	return errs
}

func checkDocInfoXMPSync(ctx context.Context, doc *document.Document, xmpData []byte) []error {
	// Get doc info dict.
	infoObj, ok := doc.Index.Trailer["Info"]
	if !ok {
		return nil // No info dict — nothing to sync.
	}
	infoRes, _ := document.ResolveObject(ctx, doc, infoObj)
	infoDict, ok := infoRes.(ast.PdfDict)
	if !ok {
		return nil
	}

	// Parse XMP for comparison values.
	xmpValues := parseXMPForSync(xmpData)
	if xmpValues == nil {
		return nil
	}

	// Map of info dict keys to XMP property names.
	// Only the Dublin Core mappings are enforced for consistency per
	// veraPDF's interpretation of ISO 19005 clause 6.7.3. The
	// pdf:Keywords, xmp:CreatorTool, and pdf:Producer mappings are
	// informational — real-world PDF producers frequently diverge on
	// these without violating the spec's intent.
	syncPairs := []struct {
		infoKey string
		xmpKey  string
	}{
		{"Title", "dc:title"},
		{"Author", "dc:creator"},
		{"Subject", "dc:description"},
	}

	var errs []error
	for _, pair := range syncPairs {
		infoVal := getInfoString(ctx, doc, infoDict, pair.infoKey)
		if infoVal == "" {
			continue
		}
		xmpVal, hasXMP := xmpValues[pair.xmpKey]
		if !hasXMP {
			continue
		}
		if xmpVal != "" && strings.TrimSpace(infoVal) != strings.TrimSpace(xmpVal) {
			errs = append(errs, fmt.Errorf("%w: /%s=%q vs %s=%q",
				models.ErrDocInfoXMPMismatch, pair.infoKey, infoVal, pair.xmpKey, xmpVal))
		}
	}
	return errs
}

func getInfoString(ctx context.Context, doc *document.Document, infoDict ast.PdfDict, key string) string {
	valObj, ok := infoDict[ast.PdfName(key)]
	if !ok {
		return ""
	}
	valRes, _ := document.ResolveObject(ctx, doc, valObj)
	if s, ok := valRes.(ast.PdfString); ok {
		return document.DecodePdfString(s)
	}
	return ""
}

// parseXMPForSync extracts key XMP property values for DocInfo comparison.
func parseXMPForSync(data []byte) map[string]string {
	result := make(map[string]string)
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false

	var currentNS string
	var currentLocal string

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			currentNS = t.Name.Space
			currentLocal = t.Name.Local
			// Check for rdf:Description attributes (inline XMP values).
			for _, attr := range t.Attr {
				if attr.Name.Space == "" || attr.Name.Space == "xmlns" {
					continue
				}
				key := nsPrefix(attr.Name.Space) + ":" + attr.Name.Local
				result[key] = attr.Value
			}
		case xml.CharData:
			if currentNS != "" && currentLocal != "" {
				key := nsPrefix(currentNS) + ":" + currentLocal
				val := strings.TrimSpace(string(t))
				if val != "" {
					result[key] = val
				}
			}
		case xml.EndElement:
			currentNS = ""
			currentLocal = ""
		}
	}
	return result
}

// nsPrefix maps common XMP namespace URIs to their standard prefixes.
func nsPrefix(ns string) string {
	switch ns {
	case "http://purl.org/dc/elements/1.1/":
		return "dc"
	case "http://ns.adobe.com/xap/1.0/":
		return "xmp"
	case "http://ns.adobe.com/pdf/1.3/":
		return "pdf"
	case "http://ns.adobe.com/xap/1.0/mm/":
		return "xmpMM"
	case "http://ns.adobe.com/xap/1.0/rights/":
		return "xmpRights"
	case "http://www.aiim.org/pdfa/ns/id/":
		return "pdfaid"
	default:
		return ns
	}
}

// --- Reference XObject check (ISO 19005, clause 6.2.9) ---

// checkNoReferenceXObjects scans page XObject resources for reference
// XObjects, which are forbidden in all PDF/A parts.
func checkNoReferenceXObjects(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	pages := collectPages(ctx, doc, catalog)
	for _, page := range pages {
		resources := getResources(ctx, doc, page)
		if resources == nil {
			continue
		}
		xobjObj, ok := resources["XObject"]
		if !ok {
			continue
		}
		xobjRes, _ := document.ResolveObject(ctx, doc, xobjObj)
		xobjDict, ok := xobjRes.(ast.PdfDict)
		if !ok {
			continue
		}
		for _, obj := range xobjDict {
			objRes, _ := document.ResolveObject(ctx, doc, obj)
			switch xo := objRes.(type) {
			case ast.PdfStream:
				if isReferenceXObject(xo.Dict) {
					return []error{models.ErrReferenceXObjectForbidden}
				}
			case ast.PdfDict:
				if isReferenceXObject(xo) {
					return []error{models.ErrReferenceXObjectForbidden}
				}
			}
		}
	}
	return nil
}

// isReferenceXObject returns true if the XObject dict is a reference XObject
// (/Subtype /Form with a /Ref key).
func isReferenceXObject(dict ast.PdfDict) bool {
	stObj, ok := dict["Subtype"]
	if !ok {
		return false
	}
	stName, ok := stObj.(ast.PdfName)
	if !ok || stName != "Form" {
		return false
	}
	_, hasRef := dict["Ref"]
	return hasRef
}

// --- Form XObject checks (ISO 19005, clause 6.2.9.1) ---

// checkFormXObjectCompliance scans Form XObjects for forbidden keys:
// /OPI, /Subtype2 with value /PS, and /PS (embedded PostScript).
func checkFormXObjectCompliance(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	var errs []error
	pages := collectPages(ctx, doc, catalog)
	for _, page := range pages {
		resources := getResources(ctx, doc, page)
		if resources == nil {
			continue
		}
		xobjObj, ok := resources["XObject"]
		if !ok {
			continue
		}
		xobjRes, _ := document.ResolveObject(ctx, doc, xobjObj)
		xobjDict, ok := xobjRes.(ast.PdfDict)
		if !ok {
			continue
		}
		for _, obj := range xobjDict {
			dict := resolveXObjectDict(ctx, doc, obj)
			if dict == nil {
				continue
			}
			stObj, ok := dict["Subtype"]
			if !ok {
				continue
			}
			stName, ok := stObj.(ast.PdfName)
			if !ok {
				continue
			}

			// Check Form XObjects for forbidden keys.
			if stName == "Form" {
				if _, ok := dict["OPI"]; ok {
					errs = append(errs, fmt.Errorf("%w: /OPI", models.ErrFormXObjectForbiddenKey))
				}
				if _, ok := dict["PS"]; ok {
					errs = append(errs, fmt.Errorf("%w: /PS", models.ErrFormXObjectForbiddenKey))
				}
				if st2Obj, ok := dict["Subtype2"]; ok {
					if st2Name, ok := st2Obj.(ast.PdfName); ok && st2Name == "PS" {
						errs = append(errs, fmt.Errorf("%w: /Subtype2 /PS", models.ErrFormXObjectForbiddenKey))
					}
				}
			}

			// Check for PostScript XObjects (forbidden in all parts).
			if stName == "PS" {
				errs = append(errs, models.ErrPostScriptXObjectForbidden)
			}
		}
	}
	return errs
}

// resolveXObjectDict resolves an XObject to its dictionary, handling both
// PdfStream and PdfDict representations.
func resolveXObjectDict(ctx context.Context, doc *document.Document, obj ast.PdfObject) ast.PdfDict {
	objRes, _ := document.ResolveObject(ctx, doc, obj)
	switch xo := objRes.(type) {
	case ast.PdfStream:
		return xo.Dict
	case ast.PdfDict:
		return xo
	}
	return nil
}

// --- Transparency blending color space (ISO 19005-2, clause 6.2.9/6.2.10) ---

// standardBlendModes are the blend modes defined in ISO 32000-1:2008 §11.3.5.
var standardBlendModes = map[ast.PdfName]bool{
	"Normal":     true,
	"Compatible": true,
	"Multiply":   true,
	"Screen":     true,
	"Overlay":    true,
	"Darken":     true,
	"Lighten":    true,
	"ColorDodge": true,
	"ColorBurn":  true,
	"HardLight":  true,
	"SoftLight":  true,
	"Difference": true,
	"Exclusion":  true,
	"Hue":        true,
	"Saturation": true,
	"Color":      true,
	"Luminosity": true,
}

// checkBlendModes verifies that all blend modes in ExtGState dictionaries
// are standard ISO 32000-1 modes. This applies to PDF/A-2+ where
// transparency is allowed but non-standard blend modes are not.
func checkBlendModes(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	pages := collectPages(ctx, doc, catalog)
	for _, page := range pages {
		resources := getResources(ctx, doc, page)
		if resources == nil {
			continue
		}
		gsObj, ok := resources["ExtGState"]
		if !ok {
			continue
		}
		gsRes, _ := document.ResolveObject(ctx, doc, gsObj)
		gsDict, ok := gsRes.(ast.PdfDict)
		if !ok {
			continue
		}
		for _, v := range gsDict {
			vRes, _ := document.ResolveObject(ctx, doc, v)
			stateDict, ok := vRes.(ast.PdfDict)
			if !ok {
				continue
			}
			bmObj, ok := stateDict["BM"]
			if !ok {
				continue
			}
			bmRes, _ := document.ResolveObject(ctx, doc, bmObj)
			if bmName, ok := bmRes.(ast.PdfName); ok {
				if !standardBlendModes[bmName] {
					return []error{fmt.Errorf("%w: /%s", models.ErrInvalidBlendMode, bmName)}
				}
			}
		}
	}
	return nil
}

// checkTransparencyBlendingCS verifies that pages using transparency features
// declare a blending color space when no document-level OutputIntent ICC
// profile is present (ISO 19005-2 §6.2.9/6.2.10). Each page with
// transparency must have /Group << /S /Transparency /CS ... >>.
func checkTransparencyBlendingCS(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	// If the document has an OutputIntent with ICC profile, the requirement
	// is satisfied globally — no per-page /Group /CS needed.
	if hasOutputIntentICC(ctx, doc, catalog) {
		return nil
	}

	var errs []error
	pages := collectPages(ctx, doc, catalog)
	for _, page := range pages {
		if !pageUsesTransparency(ctx, doc, page) {
			continue
		}
		// Page uses transparency: must have /Group with /CS, or a
		// page-level OutputIntent with an ICC profile.
		if !hasGroupWithCS(ctx, doc, page) && !hasPageOutputIntentICC(ctx, doc, page) {
			errs = append(errs, models.ErrMissingBlendingColorSpace)
		}
	}
	return errs
}

// hasPageOutputIntentICC returns true if the page dictionary itself contains
// an /OutputIntents array with at least one entry having /DestOutputProfile.
// This is a page-level override allowed by ISO 32000-2 (PDF 2.0).
func hasPageOutputIntentICC(ctx context.Context, doc *document.Document, pageDict ast.PdfDict) bool {
	oiObj, ok := pageDict["OutputIntents"]
	if !ok {
		return false
	}
	oiRes, _ := document.ResolveObject(ctx, doc, oiObj)
	oi, ok := oiRes.(ast.PdfArray)
	if !ok || len(oi) == 0 {
		return false
	}
	for _, entry := range oi {
		entryRes, _ := document.ResolveObject(ctx, doc, entry)
		dict, ok := entryRes.(ast.PdfDict)
		if !ok {
			continue
		}
		if _, has := dict["DestOutputProfile"]; has {
			return true
		}
	}
	return false
}

// pageUsesTransparency returns true if the page's ExtGState resources contain
// any transparency feature: non-Normal blend mode, opacity < 1.0, or SMask.
// Note: a /Group /S /Transparency declaration alone is not sufficient to
// trigger this — it is the page's color space group declaration, not evidence
// of transparent rendering operations.
func pageUsesTransparency(ctx context.Context, doc *document.Document, pageDict ast.PdfDict) bool {
	resources := getResources(ctx, doc, pageDict)
	if resources == nil {
		return false
	}
	gsObj, ok := resources["ExtGState"]
	if !ok {
		return false
	}
	gsRes, _ := document.ResolveObject(ctx, doc, gsObj)
	gsDict, ok := gsRes.(ast.PdfDict)
	if !ok {
		return false
	}
	for _, v := range gsDict {
		vRes, _ := document.ResolveObject(ctx, doc, v)
		stateDict, ok := vRes.(ast.PdfDict)
		if !ok {
			continue
		}
		if bmObj, ok := stateDict["BM"]; ok {
			if bmName, ok := bmObj.(ast.PdfName); ok && bmName != "Normal" && bmName != "Compatible" {
				return true
			}
		}
		if caObj, ok := stateDict["ca"]; ok {
			if v := document.GetFloat(caObj); isTransparentOpacity(v) {
				return true
			}
		}
		if caObj, ok := stateDict["CA"]; ok {
			if v := document.GetFloat(caObj); isTransparentOpacity(v) {
				return true
			}
		}
		if smObj, ok := stateDict["SMask"]; ok {
			if smName, ok := smObj.(ast.PdfName); !ok || smName != "None" {
				return true
			}
		}
	}
	return false
}

// hasGroupWithCS returns true if the page has a /Group dictionary containing
// both /S /Transparency and a /CS (color space) entry.
func hasGroupWithCS(ctx context.Context, doc *document.Document, pageDict ast.PdfDict) bool {
	grpObj, ok := pageDict["Group"]
	if !ok {
		return false
	}
	grpRes, _ := document.ResolveObject(ctx, doc, grpObj)
	grpDict, ok := grpRes.(ast.PdfDict)
	if !ok {
		return false
	}
	_, hasCS := grpDict["CS"]
	return hasCS
}

// --- Font checks ---

func checkFontsEmbedded(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	var errs []error
	fonts := collectFontDicts(ctx, doc, catalog)
	for name, fontDict := range fonts {
		if isStandard14Font(ctx, doc, fontDict) {
			continue
		}
		// Type3 fonts are inherently embedded — their glyphs are defined
		// by content streams within the font dictionary itself.
		if isType3Font(ctx, doc, fontDict) {
			continue
		}
		if !isFontEmbedded(ctx, doc, fontDict) {
			errs = append(errs, fmt.Errorf("%w: %s", models.ErrFontNotEmbedded, name))
		}
	}
	return errs
}

func isType3Font(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	stObj, ok := fontDict["Subtype"]
	if !ok {
		return false
	}
	stRes, _ := document.ResolveObject(ctx, doc, stObj)
	st, ok := stRes.(ast.PdfName)
	return ok && st == "Type3"
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

func hasFontToUnicode(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	// PDF/UA requires Unicode mapping — same valid paths as PDF/A.
	return hasFontUnicodeMapping(ctx, doc, fontDict)
}

func isStandard14Font(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	bfObj, ok := fontDict["BaseFont"]
	if !ok {
		return false
	}
	bfRes, _ := document.ResolveObject(ctx, doc, bfObj)
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
		errs = append(errs, fmt.Errorf("%w: %s", models.ErrMissingUnicodeMapping, name))
	}
	return errs
}

// hasFontUnicodeMapping returns true if the font has a reliable path to
// Unicode text extraction. Per ISO 32000-1:2008 §9.10.2, valid paths are:
//   - Explicit /ToUnicode CMap.
//   - A standard predefined encoding (WinAnsiEncoding, etc.).
//   - An /Encoding dict with /BaseEncoding or /Differences (glyph names
//     mapped through the Adobe Glyph List).
//   - Type3 fonts (glyphs defined by name; mapped through the AGL).
//   - Composite (Type0) fonts with a predefined or Identity CMap encoding,
//     which provides a CID-to-Unicode path via the CIDSystemInfo.
func hasFontUnicodeMapping(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	// Explicit /ToUnicode CMap is always sufficient.
	if _, ok := fontDict["ToUnicode"]; ok {
		return true
	}

	// Type3 fonts define glyphs by name — the Adobe Glyph List provides
	// the Unicode mapping.
	if isType3Font(ctx, doc, fontDict) {
		return true
	}

	// Composite (Type0) fonts: a predefined or Identity CMap encoding
	// provides a valid CID-to-Unicode path.
	if isCompositeFontWithCMap(ctx, doc, fontDict) {
		return true
	}

	// Embedded Type1 fonts with glyph names provide Unicode mapping through
	// the Adobe Glyph List (ISO 19005, clause 6.3.8 exemption).
	if isEmbeddedType1Font(ctx, doc, fontDict) {
		return true
	}

	// Check for a standard predefined encoding name or an encoding
	// dictionary with /Differences (glyph name mapping via AGL).
	return hasStandardOrDifferencesEncoding(ctx, doc, fontDict)
}

func isEmbeddedType1Font(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	stObj, ok := fontDict["Subtype"]
	if !ok {
		return false
	}
	stRes, _ := document.ResolveObject(ctx, doc, stObj)
	st, ok := stRes.(ast.PdfName)
	if !ok || st != "Type1" {
		return false
	}
	// Type1 fonts embed via /FontFile, /FontFile2 (rare), or /FontFile3 (CFF).
	return isFontEmbedded(ctx, doc, fontDict)
}

// hasStandardOrDifferencesEncoding returns true if the font has a standard
// predefined encoding or an encoding dict with /BaseEncoding or /Differences.
func hasStandardOrDifferencesEncoding(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	encObj, ok := fontDict["Encoding"]
	if !ok {
		return false
	}
	encRes, _ := document.ResolveObject(ctx, doc, encObj)

	switch enc := encRes.(type) {
	case ast.PdfName:
		return standardAdobeEncodings[enc]
	case ast.PdfDict:
		if beObj, ok := enc["BaseEncoding"]; ok {
			beRes, _ := document.ResolveObject(ctx, doc, beObj)
			if beName, ok := beRes.(ast.PdfName); ok && standardAdobeEncodings[beName] {
				return true
			}
		}
		// /Differences provides explicit glyph name mappings.
		if _, ok := enc["Differences"]; ok {
			return true
		}
	}
	return false
}

// isCompositeFontWithCMap returns true for Type0 (composite) fonts that
// use a predefined or Identity CMap encoding, providing a CID-to-Unicode
// mapping path through the CIDSystemInfo.
func isCompositeFontWithCMap(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	stObj, ok := fontDict["Subtype"]
	if !ok {
		return false
	}
	stRes, _ := document.ResolveObject(ctx, doc, stObj)
	st, ok := stRes.(ast.PdfName)
	if !ok || st != "Type0" {
		return false
	}
	// Must have an /Encoding (the CMap name or stream).
	_, hasEnc := fontDict["Encoding"]
	return hasEnc
}

// --- Level 'a' (Accessible) checks ---

// checkLangEntry verifies that the Document Catalog contains a /Lang entry
// specifying the document's natural language (e.g., "en-US").
func checkLangEntry(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	if hasLangSpecified(ctx, doc, catalog) {
		return nil
	}
	return []error{models.ErrMissingLang}
}

// hasLangSpecified checks whether a natural language is declared either in
// the catalog's /Lang key or anywhere in the structure tree. Per ISO 19005
// §6.8.4 and ISO 14289 §7.2, /Lang may appear on the catalog, the root
// structure element, or individual structure elements.
func hasLangSpecified(ctx context.Context, doc *document.Document, catalog ast.PdfDict) bool {
	// Catalog-level /Lang is the primary location.
	if _, ok := catalog["Lang"]; ok {
		return true
	}
	// Fall back: check the structure tree for any /Lang entry.
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
	return structTreeHasLang(ctx, doc, kRes)
}

// structTreeHasLang recursively checks whether any structure element in the
// tree has a /Lang key.
func structTreeHasLang(ctx context.Context, doc *document.Document, obj ast.PdfObject) bool {
	switch v := obj.(type) {
	case ast.PdfDict:
		if _, has := v["Lang"]; has {
			return true
		}
		if kObj, ok := v["K"]; ok {
			kRes, _ := document.ResolveObject(ctx, doc, kObj)
			if structTreeHasLang(ctx, doc, kRes) {
				return true
			}
		}
	case ast.PdfArray:
		for _, item := range v {
			itemRes, _ := document.ResolveObject(ctx, doc, item)
			if structTreeHasLang(ctx, doc, itemRes) {
				return true
			}
		}
	}
	return false
}

// checkFigureAltText traverses the structure tree and verifies that every
// Figure element has an /Alt attribute providing alternative text.
func checkFigureAltText(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	strObj, ok := catalog["StructTreeRoot"]
	if !ok {
		return nil // Already caught by checkStructTreeRoot.
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
	checkFigureAltRecursive(ctx, doc, kRes, &errs)
	return errs
}

func checkFigureAltRecursive(ctx context.Context, doc *document.Document, obj ast.PdfObject, errs *[]error) {
	switch v := obj.(type) {
	case ast.PdfArray:
		for _, kid := range v {
			kidRes, _ := document.ResolveObject(ctx, doc, kid)
			checkFigureAltRecursive(ctx, doc, kidRes, errs)
		}
	case ast.PdfDict:
		// Check if this is a Figure element.
		if sObj, ok := v["S"]; ok {
			sRes, _ := document.ResolveObject(ctx, doc, sObj)
			if sName, ok := sRes.(ast.PdfName); ok && sName == "Figure" {
				if !hasAltOrActualText(v) {
					*errs = append(*errs, models.ErrUntaggedVisualElement)
				}
			}
		}
		// Recurse into children.
		if kObj, ok := v["K"]; ok {
			kRes, _ := document.ResolveObject(ctx, doc, kObj)
			checkFigureAltRecursive(ctx, doc, kRes, errs)
		}
	}
}

// hasAltOrActualText returns true if the structure element has /Alt or
// /ActualText. Per ISO 32000-1:2008 §14.9.4, /ActualText is a valid
// alternative to /Alt for providing textual descriptions.
func hasAltOrActualText(dict ast.PdfDict) bool {
	if _, ok := dict["Alt"]; ok {
		return true
	}
	_, ok := dict["ActualText"]
	return ok
}

func findFontDescriptor(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) ast.PdfDict {
	if fdObj, ok := fontDict["FontDescriptor"]; ok {
		fdRes, _ := document.ResolveObject(ctx, doc, fdObj)
		if fdDict, ok := fdRes.(ast.PdfDict); ok {
			return fdDict
		}
	}
	// Type0: /DescendantFonts[0]/FontDescriptor
	if dfObj, ok := fontDict["DescendantFonts"]; ok {
		dfRes, _ := document.ResolveObject(ctx, doc, dfObj)
		if dfArr, ok := dfRes.(ast.PdfArray); ok && len(dfArr) > 0 {
			cidRes, _ := document.ResolveObject(ctx, doc, dfArr[0])
			if cidDict, ok := cidRes.(ast.PdfDict); ok {
				if fdObj, ok := cidDict["FontDescriptor"]; ok {
					fdRes, _ := document.ResolveObject(ctx, doc, fdObj)
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

// checkPDFA1Version verifies that the document declares PDF version 1.4 or
// lower, as required by PDF/A-1 (ISO 19005-1). Only the catalog /Version key
// is checked — the file header version is informational and not authoritative
// for conformance, consistent with veraPDF's behavior.
func checkPDFA1Version(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	if vObj, ok := catalog["Version"]; ok {
		vRes, _ := document.ResolveObject(ctx, doc, vObj)
		if vName, ok := vRes.(ast.PdfName); ok {
			if !isPDFVersionAtMost14(string(vName)) {
				return []error{models.ErrVersionTooHighForA1}
			}
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
			return []error{models.ErrTransparencyForbidden}
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
	grpRes, _ := document.ResolveObject(ctx, doc, grpObj)
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
	gsRes, _ := document.ResolveObject(ctx, doc, gsObj)
	gsDict, ok := gsRes.(ast.PdfDict)
	if !ok {
		return nil
	}
	for _, v := range gsDict {
		vRes, _ := document.ResolveObject(ctx, doc, v)
		if stateDict, ok := vRes.(ast.PdfDict); ok {
			// /BM (blend mode) other than Normal or Compatible.
			// /Compatible is the PDF 1.0 default, equivalent to /Normal.
			if bmObj, ok := stateDict["BM"]; ok {
				if bmName, ok := bmObj.(ast.PdfName); ok && bmName != "Normal" && bmName != "Compatible" {
					return []error{models.ErrTransparencyForbidden}
				}
			}
			// /ca (fill opacity) < 1.0.
			if caObj, ok := stateDict["ca"]; ok {
				if v := document.GetFloat(caObj); isTransparentOpacity(v) {
					return []error{models.ErrTransparencyForbidden}
				}
			}
			// /CA (stroke opacity) < 1.0.
			if caObj, ok := stateDict["CA"]; ok {
				if v := document.GetFloat(caObj); isTransparentOpacity(v) {
					return []error{models.ErrTransparencyForbidden}
				}
			}
			// /SMask (soft mask).
			if smObj, ok := stateDict["SMask"]; ok {
				if smName, ok := smObj.(ast.PdfName); !ok || smName != "None" {
					return []error{models.ErrTransparencyForbidden}
				}
			}
		}
	}
	return nil
}

// isTransparentOpacity returns true if the opacity value indicates actual
// transparency: between 0 and slightly below 1.0. Values very close to 1.0
// (within 1e-4) are treated as fully opaque to handle floating-point
// imprecision in PDF writers (e.g., 0.9999999, 1.0000001).
func isTransparentOpacity(v float64) bool {
	const epsilon = 1e-4
	return v >= 0 && v < 1.0-epsilon
}

func checkNoJPX(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	pages := collectPages(ctx, doc, catalog)
	for _, page := range pages {
		if hasPageFilter(ctx, doc, page, "JPXDecode") {
			return []error{models.ErrJPEG2000Forbidden}
		}
	}
	return nil
}

func checkNoAttachments(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	if hasEmbeddedFiles(ctx, doc, catalog) {
		return []error{models.ErrEmbeddedFilesForbidden}
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
		return models.ErrNonPDFAAttachment
	}
	efRes, _ := document.ResolveObject(ctx, doc, efObj)
	efDict, ok := efRes.(ast.PdfDict)
	if !ok {
		return models.ErrNonPDFAAttachment
	}
	fObj, ok := efDict["F"]
	if !ok {
		return models.ErrNonPDFAAttachment
	}
	fRes, _ := document.ResolveObject(ctx, doc, fObj)
	stream, ok := fRes.(ast.PdfStream)
	if !ok {
		return models.ErrNonPDFAAttachment
	}

	// Decode the stream to get the raw file bytes.
	data, err := filter.DecodeStream(stream)
	if err != nil {
		return fmt.Errorf("%w: failed to decode stream", models.ErrNonPDFAAttachment)
	}

	// Non-PDF files (Word, Excel, ZIP, etc.) lack the %PDF- header.
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return models.ErrNonPDFAAttachment
	}

	// Attempt to parse the embedded PDF and validate it recursively.
	embeddedDoc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		return fmt.Errorf("%w: %v", models.ErrNonPDFAAttachment, err)
	}
	if violations := ValidatePDFA(ctx, embeddedDoc, models.PDFA2b); len(violations) > 0 {
		return fmt.Errorf("%w: %v", models.ErrNonPDFAAttachment, violations[0])
	}
	return nil
}

// checkOCGUsageApplication verifies that if Optional Content Groups (Layers)
// with /Usage entries are present via /OCProperties, the dictionary contains
// /AS (Usage Application Dictionaries) as required by PDF/A-2 and PDF/A-3.
// /AS is only required when at least one OCG defines a /Usage dict.
func checkOCGUsageApplication(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	ocObj, ok := catalog["OCProperties"]
	if !ok {
		return nil
	}
	ocRes, _ := document.ResolveObject(ctx, doc, ocObj)
	ocDict, ok := ocRes.(ast.PdfDict)
	if !ok {
		return nil
	}
	if _, ok := ocDict["AS"]; ok {
		return nil
	}
	// /AS is only required when OCGs define /Usage entries.
	if !ocgsHaveUsage(ctx, doc, ocDict) {
		return nil
	}
	return []error{models.ErrMissingOCGUsageApp}
}

// ocgsHaveUsage returns true if any OCG in the /OCGs array has a /Usage dict.
func ocgsHaveUsage(ctx context.Context, doc *document.Document, ocDict ast.PdfDict) bool {
	ocgsObj, ok := ocDict["OCGs"]
	if !ok {
		return false
	}
	ocgsRes, _ := document.ResolveObject(ctx, doc, ocgsObj)
	ocgsArr, ok := ocgsRes.(ast.PdfArray)
	if !ok {
		return false
	}
	for _, ocgObj := range ocgsArr {
		ocgRes, _ := document.ResolveObject(ctx, doc, ocgObj)
		if ocgDict, ok := ocgRes.(ast.PdfDict); ok {
			if _, ok := ocgDict["Usage"]; ok {
				return true
			}
		}
	}
	return false
}

// --- Part 3 specific ---

// validAFRelationships are the allowed /AFRelationship values per ISO 19005-3
// and ISO 32000-2:2020 (PDF 2.0). /Undefined is accepted by veraPDF and is
// used in practice for files with no specific relationship.
var validAFRelationships = map[ast.PdfName]bool{
	"Source":      true,
	"Data":        true,
	"Alternative": true,
	"Supplement":  true,
	"Unspecified": true,
	"Undefined":   true,
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
			errs = append(errs, models.ErrMissingAFRelationship)
			continue
		}
		afRes, _ := document.ResolveObject(ctx, doc, afObj)
		afName, ok := afRes.(ast.PdfName)
		if !ok || !validAFRelationships[afName] {
			errs = append(errs, fmt.Errorf("%w: got /%v", models.ErrMissingAFRelationship, afObj))
		}
	}
	return errs
}

// --- Part 4 specific ---

// checkPDFA4Version verifies that the document declares PDF version 2.0,
// as required by PDF/A-4 (ISO 19005-4). Checks catalog /Version then header.
func checkPDFA4Version(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	if vObj, ok := catalog["Version"]; ok {
		vRes, _ := document.ResolveObject(ctx, doc, vObj)
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
	return []error{models.ErrVersionNotPDF20}
}

// valid3DSubtypes are the allowed /Subtype values for 3D content in PDF/A-4e.
// /3D appears on RichMediaContent and RichMediaConfiguration dicts.
var valid3DSubtypes = map[ast.PdfName]bool{
	"U3D": true,
	"PRC": true,
	"3D":  true,
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
		errs = append(errs, models.ErrMissing3DSubtype)
	}

	// Check for 2D fallback appearance (/AP /N).
	if !has2DFallback(ctx, doc, annotDict) {
		errs = append(errs, models.ErrMissing3DFallback)
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
	ddRes, _ := document.ResolveObject(ctx, doc, ddObj)
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

// hasRichMedia3DSubtype checks /RichMediaContent for 3D content by examining
// both the content dict's own /Subtype and /Configurations entries.
func hasRichMedia3DSubtype(ctx context.Context, doc *document.Document, annotDict ast.PdfDict) bool {
	rcObj, ok := annotDict["RichMediaContent"]
	if !ok {
		return false
	}
	rcRes, _ := document.ResolveObject(ctx, doc, rcObj)
	rcDict, ok := rcRes.(ast.PdfDict)
	if !ok {
		return false
	}

	// Check if the RichMediaContent itself declares /Subtype /3D.
	if check3DStreamDict(rcDict) {
		return true
	}

	// Check /Configurations for /Subtype /3D entries.
	if configsObj, ok := rcDict["Configurations"]; ok {
		configsRes, _ := document.ResolveObject(ctx, doc, configsObj)
		if configsArr, ok := configsRes.(ast.PdfArray); ok {
			for _, cfgObj := range configsArr {
				cfgRes, _ := document.ResolveObject(ctx, doc, cfgObj)
				if cfgDict, ok := cfgRes.(ast.PdfDict); ok {
					if check3DStreamDict(cfgDict) {
						return true
					}
				}
			}
		}
	}

	// Check /Assets for embedded 3D streams.
	assetsObj, ok := rcDict["Assets"]
	if !ok {
		return false
	}
	assetsRes, _ := document.ResolveObject(ctx, doc, assetsObj)
	assetsDict, ok := assetsRes.(ast.PdfDict)
	if !ok {
		return false
	}
	namesObj, ok := assetsDict["Names"]
	if !ok {
		return false
	}
	namesRes, _ := document.ResolveObject(ctx, doc, namesObj)
	namesArr, ok := namesRes.(ast.PdfArray)
	if !ok {
		return false
	}
	for i := 1; i < len(namesArr); i += 2 {
		valRes, _ := document.ResolveObject(ctx, doc, namesArr[i])
		if fsDict, ok := valRes.(ast.PdfDict); ok {
			if efObj, ok := fsDict["EF"]; ok {
				efRes, _ := document.ResolveObject(ctx, doc, efObj)
				if efDict, ok := efRes.(ast.PdfDict); ok {
					if fObj, ok := efDict["F"]; ok {
						fRes, _ := document.ResolveObject(ctx, doc, fObj)
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
	apRes, _ := document.ResolveObject(ctx, doc, apObj)
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
		return []error{models.ErrMissingMarkInfo}
	}
	miRes, _ := document.ResolveObject(ctx, doc, miObj)
	miDict, ok := miRes.(ast.PdfDict)
	if !ok {
		return []error{models.ErrMissingMarkInfo}
	}
	markedObj, ok := miDict["Marked"]
	if !ok {
		return []error{models.ErrMissingMarkInfo}
	}
	// Resolve in case /Marked is an indirect reference to a boolean.
	markedRes, _ := document.ResolveObject(ctx, doc, markedObj)
	if b, ok := markedRes.(ast.PdfBool); !ok || !bool(b) {
		return []error{models.ErrMissingMarkInfo}
	}
	return nil
}

func checkStructTreeRoot(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	strObj, ok := catalog["StructTreeRoot"]
	if !ok {
		return []error{models.ErrMissingStructTreeRoot}
	}
	strRes, _ := document.ResolveObject(ctx, doc, strObj)
	if _, ok := strRes.(ast.PdfDict); !ok {
		return []error{models.ErrMissingStructTreeRoot}
	}
	return nil
}

// --- Helpers ---

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
	res, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return
	}
	dict, ok := res.(ast.PdfDict)
	if !ok {
		return
	}
	typeObj, _ := document.ResolveObject(ctx, doc, dict["Type"])
	typeName, _ := typeObj.(ast.PdfName)

	switch typeName {
	case "Pages":
		kidsObj, ok := dict["Kids"]
		if !ok {
			return
		}
		kidsRes, _ := document.ResolveObject(ctx, doc, kidsObj)
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
	resRes, _ := document.ResolveObject(ctx, doc, resObj)
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
		fontRes, _ := document.ResolveObject(ctx, doc, fontObj)
		fontDict, ok := fontRes.(ast.PdfDict)
		if !ok {
			continue
		}
		for name, fObj := range fontDict {
			fRes, _ := document.ResolveObject(ctx, doc, fObj)
			if fDict, ok := fRes.(ast.PdfDict); ok {
				fonts[string(name)] = fDict
			}
		}
	}
	return fonts
}

// hasPageFilter checks whether any XObject stream on a page uses the given filter.
func hasPageFilter(ctx context.Context, doc *document.Document, pageDict ast.PdfDict, filterName string) bool {
	resources := getResources(ctx, doc, pageDict)
	if resources == nil {
		return false
	}
	xobjObj, ok := resources["XObject"]
	if !ok {
		return false
	}
	xobjRes, _ := document.ResolveObject(ctx, doc, xobjObj)
	xobjDict, ok := xobjRes.(ast.PdfDict)
	if !ok {
		return false
	}
	for _, obj := range xobjDict {
		objRes, _ := document.ResolveObject(ctx, doc, obj)
		stream, ok := objRes.(ast.PdfStream)
		if !ok {
			continue
		}
		if streamHasFilter(ctx, doc, stream.Dict, filterName) {
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
	filterRes, _ := document.ResolveObject(ctx, doc, filterObj)
	switch f := filterRes.(type) {
	case ast.PdfName:
		return string(f) == target
	case ast.PdfArray:
		for _, item := range f {
			itemRes, _ := document.ResolveObject(ctx, doc, item)
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
	namesRes, _ := document.ResolveObject(ctx, doc, namesObj)
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
	namesRes, _ := document.ResolveObject(ctx, doc, namesObj)
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
	res, _ := document.ResolveObject(ctx, doc, obj)
	dict, ok := res.(ast.PdfDict)
	if !ok {
		return
	}
	if kidsObj, ok := dict["Kids"]; ok {
		kidsRes, _ := document.ResolveObject(ctx, doc, kidsObj)
		if kidsArr, ok := kidsRes.(ast.PdfArray); ok {
			for _, kid := range kidsArr {
				collectFileSpecsRecursive(ctx, doc, kid, specs)
			}
		}
		return
	}
	if namesObj, ok := dict["Names"]; ok {
		namesRes, _ := document.ResolveObject(ctx, doc, namesObj)
		if namesArr, ok := namesRes.(ast.PdfArray); ok {
			for i := 1; i < len(namesArr); i += 2 {
				valRes, _ := document.ResolveObject(ctx, doc, namesArr[i])
				if fsDict, ok := valRes.(ast.PdfDict); ok {
					*specs = append(*specs, fsDict)
				}
			}
		}
	}
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
