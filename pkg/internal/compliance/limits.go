package compliance

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// pageBoundaryBoxes are the page boundary keys defined in ISO 32000-1 §14.11.2.
var pageBoundaryBoxes = []ast.PdfName{ //nolint:gochecknoglobals // constant table
	"MediaBox", "CropBox", "BleedBox", "TrimBox", "ArtBox",
}

// checkPageSizeBounds verifies that all page boundary dimensions (MediaBox,
// CropBox, BleedBox, TrimBox, ArtBox) are within the allowed range of
// [3, 14400] user units per ISO 19005 §6.1.12 / §6.1.13.
func checkPageSizeBounds(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	const (
		minDim = 3.0
		maxDim = 14400.0
	)

	pages := collectPages(ctx, doc, catalog)
	for _, page := range pages {
		for _, boxName := range pageBoundaryBoxes {
			boxObj, ok := page[boxName]
			if !ok {
				continue
			}
			boxRes, _ := document.ResolveObject(ctx, doc, boxObj)
			boxArr, ok := boxRes.(ast.PdfArray)
			if !ok || len(boxArr) < 4 { //nolint:mnd // PDF rectangles have 4 elements
				continue
			}
			x1 := document.GetFloat(boxArr[0])
			y1 := document.GetFloat(boxArr[1])
			x2 := document.GetFloat(boxArr[2])
			y2 := document.GetFloat(boxArr[3])

			width := x2 - x1
			height := y2 - y1
			if width < 0 {
				width = -width
			}
			if height < 0 {
				height = -height
			}

			if width < minDim || width > maxDim || height < minDim || height > maxDim {
				return []error{fmt.Errorf("%w: /%s %.1f x %.1f",
					models.ErrPageSizeOutOfBounds, boxName, width, height)}
			}
		}
	}
	return nil
}

// decodedNameLength returns the byte length of a PdfName after decoding
// #XX hex escape sequences. Each #XX sequence (3 raw bytes) represents
// 1 decoded byte.
func decodedNameLength(name ast.PdfName) int {
	s := string(name)
	if !strings.Contains(s, "#") {
		return len(s)
	}
	decoded := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '#' && i+2 < len(s) {
			if _, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
				decoded++
				i += 2 // skip the two hex digits
				continue
			}
		}
		decoded++
	}
	return decoded
}

// checkNameLengths verifies that PdfName values in the catalog and page
// resources do not exceed 127 decoded bytes per ISO 19005 §6.1.13.
func checkNameLengths(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	const maxNameLen = 127

	// Check catalog-level names.
	for key := range catalog {
		if decodedNameLength(key) > maxNameLen {
			return []error{fmt.Errorf("%w: /%s (%d bytes)",
				models.ErrNameTooLong, key, decodedNameLength(key))}
		}
	}

	// Check page resource names.
	pages := collectPages(ctx, doc, catalog)
	for _, page := range pages {
		resources := getResources(ctx, doc, page)
		if resources == nil {
			continue
		}
		if err := checkDictNameLengths(ctx, doc, resources, maxNameLen); err != nil {
			return []error{err}
		}
	}
	return nil
}

// checkDictNameLengths checks PdfName key lengths in a dictionary.
func checkDictNameLengths(_ context.Context, _ *document.Document, dict ast.PdfDict, maxLen int) error {
	for key := range dict {
		if decodedNameLength(key) > maxLen {
			return fmt.Errorf("%w: /%s (%d bytes)",
				models.ErrNameTooLong, key, decodedNameLength(key))
		}
	}
	return nil
}

// checkStringLengths verifies that PdfString values do not exceed the
// maximum length allowed by the profile. PDF/A-2+ allows 32767 bytes;
// PDF/A-1 and general PDF allow 65535.
func checkStringLengths(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile models.PDFAProfile) []error {
	maxLen := 65535 //nolint:mnd // PDF spec string limit
	if PdfaPart(profile) >= 2 {
		maxLen = 32767 //nolint:mnd // PDF/A-2+ string limit
	}

	// Check document info dict strings.
	if infoObj, ok := doc.Index.Trailer["Info"]; ok {
		infoRes, _ := document.ResolveObject(ctx, doc, infoObj)
		if infoDict, ok := infoRes.(ast.PdfDict); ok {
			for key, val := range infoDict {
				valRes, _ := document.ResolveObject(ctx, doc, val)
				if s, ok := valRes.(ast.PdfString); ok {
					if len(s) > maxLen {
						return []error{fmt.Errorf("%w: /%s (%d bytes, max %d)",
							models.ErrStringTooLong, key, len(s), maxLen)}
					}
				}
			}
		}
	}
	return nil
}

// makeStringLengthCheck returns a catalog check function for string length
// validation with the given profile.
func makeStringLengthCheck(profile models.PDFAProfile) catalogCheckFunc {
	return func(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
		return checkStringLengths(ctx, doc, catalog, profile)
	}
}

// decodeName decodes #XX hex escape sequences in a PdfName, returning the
// decoded string.
func decodeName(name ast.PdfName) string {
	s := string(name)
	if !strings.Contains(s, "#") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '#' && i+2 < len(s) {
			if val, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
				b.WriteByte(byte(val))
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// checkCatalogVersionFormat verifies that if /Version is present in the
// catalog, it has exactly 3 characters in the format "x.y" where x and y
// are digits.
func checkCatalogVersionFormat(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	vObj, ok := catalog["Version"]
	if !ok {
		return nil
	}
	vRes, _ := document.ResolveObject(ctx, doc, vObj)
	vName, ok := vRes.(ast.PdfName)
	if !ok {
		return nil
	}
	v := decodeName(vName)
	// Must be exactly 3 characters: "x.y"
	if len(v) != 3 || v[1] != '.' || v[0] < '0' || v[0] > '9' || v[2] < '0' || v[2] > '9' { //nolint:mnd // version format "x.y"
		return []error{fmt.Errorf("%w: /%s", models.ErrInvalidHeaderVersion, v)}
	}
	return nil
}

// checkPermissionsDict verifies that the /Perms dictionary in the catalog
// contains only allowed keys: /UR3 and /DocMDP (6.1.12 test 1).
func checkPermissionsDict(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	permsObj, ok := catalog["Perms"]
	if !ok {
		return nil
	}
	permsRes, _ := document.ResolveObject(ctx, doc, permsObj)
	permsDict, ok := permsRes.(ast.PdfDict)
	if !ok {
		return nil
	}
	for key := range permsDict {
		if key != "UR3" && key != "DocMDP" {
			return []error{fmt.Errorf("%w: /Perms contains forbidden key /%s",
				models.ErrInvalidHeaderVersion, key)}
		}
	}
	return nil
}

// checkDocMDPSignatureRefs verifies that if /DocMDP is present in /Perms,
// the Signature References dictionary does not contain /DigestLocation,
// /DigestMethod, or /DigestValue (6.1.12 test 2).
func checkDocMDPSignatureRefs(ctx context.Context, doc *document.Document, catalog ast.PdfDict) []error {
	permsObj, ok := catalog["Perms"]
	if !ok {
		return nil
	}
	permsRes, _ := document.ResolveObject(ctx, doc, permsObj)
	permsDict, ok := permsRes.(ast.PdfDict)
	if !ok {
		return nil
	}
	docMDPObj, ok := permsDict["DocMDP"]
	if !ok {
		return nil
	}
	docMDPRes, _ := document.ResolveObject(ctx, doc, docMDPObj)
	sigDict, ok := docMDPRes.(ast.PdfDict)
	if !ok {
		return nil
	}
	// Check /Reference array for forbidden keys.
	refObj, ok := sigDict["Reference"]
	if !ok {
		return nil
	}
	refRes, _ := document.ResolveObject(ctx, doc, refObj)
	refArr, ok := refRes.(ast.PdfArray)
	if !ok {
		return nil
	}
	forbiddenKeys := []ast.PdfName{"DigestLocation", "DigestMethod", "DigestValue"}
	for _, item := range refArr {
		itemRes, _ := document.ResolveObject(ctx, doc, item)
		if itemDict, ok := itemRes.(ast.PdfDict); ok {
			for _, key := range forbiddenKeys {
				if _, ok := itemDict[key]; ok {
					return []error{fmt.Errorf("%w: Signature Reference contains forbidden /%s",
						models.ErrInvalidHeaderVersion, key)}
				}
			}
		}
	}
	return nil
}
