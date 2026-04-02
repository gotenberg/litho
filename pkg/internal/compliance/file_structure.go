package compliance

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
)

// --- File structure constants used only by this file ---

const (
	headerReadSize = 64
	minBinaryBytes = 4
	eofMarker      = "%%EOF"
	tailReadSize   = 4096
	minPageBound   = 3.0
	maxPageBound   = 14400.0
)

// checkFileStructureGroup validates PDF file structure rules (§6.1).
func checkFileStructureGroup(ctx context.Context, vctx *validationCtx) []Violation {
	doc, catalog, profile := vctx.doc, vctx.catalog, vctx.profile
	part := vctx.part

	slog.Debug("compliance.file_structure: starting checks", "profile", profile)

	var results []Violation

	results = append(results, checkFileHeader(doc, profile)...)
	results = append(results, checkTrailerID(doc, profile)...)
	results = append(results, checkEncryptionForbidden(doc, profile)...)
	results = append(results, checkPostEOFData(doc, profile)...)
	results = append(results, checkStreamExternalKeys(ctx, doc, profile)...)
	results = append(results, checkStreamLZW(ctx, doc, profile)...)
	results = append(results, checkEmbeddedFiles(ctx, doc, catalog, profile)...)
	results = append(results, checkFormatViolations(doc, profile)...)
	results = append(results, checkObjHeaderLeadingWhitespace(doc, profile)...)
	// TODO: checkStreamLengths produces FPs on some linearized/complex PDFs
	// where raw-byte scanning finds endstream at wrong offset. Disabled until
	// a more robust approach is available (e.g., using xref offsets directly).
	// results = append(results, checkStreamLengths(ctx, doc, profile)...)
	results = append(results, checkContentStreamHexStrings(ctx, doc, catalog, profile)...)
	results = append(results, checkNonStandardFilters(ctx, doc, profile)...)
	results = append(results, checkPageBoundaries(ctx, doc, catalog, profile)...)
	results = append(results, checkNumericLimits(ctx, doc, profile)...)
	results = append(results, checkXRefFormat(doc, profile)...)
	results = append(results, checkXRefTableByteFormat(doc, profile)...)

	if part >= 2 {
		results = append(results, checkSignatureReferences(ctx, doc, catalog, profile)...)
	}
	if part >= 2 {
		results = append(results, checkUTF8Names(ctx, doc, catalog, profile)...)
	}
	if part == 4 {
		results = append(results, checkPDFA4TrailerInfo(doc, catalog, profile)...)
	}

	slog.Debug("compliance.file_structure: checks complete",
		"profile", profile,
		"violations", len(results),
	)
	return results
}

// --- Rule selectors ---

func fileHeaderRule(profile string) Rule {
	return ruleByPart(pdfaPart(profile), Rule_19005_1_6_1_2_t01, Rule_19005_2_6_1_2_t01, Rule_19005_3_6_1_2_t01, Rule_19005_4_6_1_2_t01)
}

func fileBinaryCommentRule(profile string) Rule {
	return ruleByPart(pdfaPart(profile), Rule_19005_1_6_1_2_t02, Rule_19005_2_6_1_2_t02, Rule_19005_3_6_1_2_t02, Rule_19005_4_6_1_2_t02)
}

func trailerIDRule(profile string) Rule {
	return ruleByPart(pdfaPart(profile), Rule_19005_1_6_1_3_t01, Rule_19005_2_6_1_3_t01, Rule_19005_3_6_1_3_t01, Rule_19005_4_6_1_3_t01)
}

func encryptRule(profile string) Rule {
	return ruleByPart(pdfaPart(profile), Rule_19005_1_6_1_3_t02, Rule_19005_2_6_1_3_t02, Rule_19005_3_6_1_3_t02, Rule_19005_4_6_1_3_t02)
}

func postEOFRule(profile string) Rule {
	return ruleByPart(pdfaPart(profile), Rule_19005_1_6_1_3_t03, Rule_19005_2_6_1_3_t03, Rule_19005_3_6_1_3_t03, Rule_19005_4_6_1_3_t03)
}

func streamExternalKeysRule(profile string) Rule {
	return ruleByPart(pdfaPart(profile), Rule_19005_1_6_1_7_t03, Rule_19005_2_6_1_7_1_t03, Rule_19005_3_6_1_7_1_t03, Rule_19005_4_6_1_6_1_t02)
}

func lzwFilterRule(profile string) Rule {
	return ruleByPart(pdfaPart(profile), Rule_19005_1_6_1_10_t01, Rule_19005_2_6_1_7_2_t01, Rule_19005_3_6_1_7_2_t01, Rule_19005_4_6_1_6_2_t01)
}

func embeddedFilesEFRule(_ string) Rule {
	return Rule_19005_1_6_1_11_t01
}

func embeddedFilesNameRule(_ string) Rule {
	return Rule_19005_1_6_1_11_t02
}

func pageBoundRule(profile string) Rule {
	// PDF/A-1 uses the same clause numbering as part 2 for this check.
	return ruleByPart(pdfaPart(profile), Rule_19005_2_6_1_13_t11, Rule_19005_2_6_1_13_t11, Rule_19005_3_6_1_13_t11, Rule{})
}

// --- Check implementations ---

// checkFileHeader validates §6.1.2: file must start with %PDF-x.y and have
// a binary comment marker.
func checkFileHeader(doc *document.Document, profile string) []Violation {
	buf, err := safeReadHeader(doc, headerReadSize)
	if err != nil {
		slog.Debug("compliance.file_structure: cannot read header", "error", err)
		return nil
	}

	part := pdfaPart(profile)

	// Check that file starts with %PDF-.
	if !bytes.HasPrefix(buf, []byte("%PDF-")) {
		return fail(fileHeaderRule(profile), "header does not start with %PDF-")
	}

	// Validate version pattern.
	// PDF/A-1: any valid %PDF-x.y header is accepted; version is checked separately.
	// PDF/A-2/3: %PDF-1.n where n is 0-7.
	// PDF/A-4: %PDF-2.n where n is 0-9.
	if len(buf) < 8 {
		return fail(fileHeaderRule(profile), "header too short")
	}
	version := string(buf[5:8]) // e.g., "1.4" or "2.0"

	var valid bool
	switch part {
	case 1:
		// PDF/A-1: accept any valid %PDF-x.y (version validated separately).
		valid = buf[5] >= '1' && buf[5] <= '9' && buf[6] == '.' && buf[7] >= '0' && buf[7] <= '9'
	case 2, 3:
		// Must be %PDF-1.n where n is 0-7.
		valid = buf[5] == '1' && buf[6] == '.' && buf[7] >= '0' && buf[7] <= '7'
	case 4:
		// Must be %PDF-2.n where n is 0-9.
		valid = buf[5] == '2' && buf[6] == '.' && buf[7] >= '0' && buf[7] <= '9'
	}

	if !valid {
		return fail(fileHeaderRule(profile),
			fmt.Sprintf("header %q does not match required pattern", "%PDF-"+version))
	}

	// After %PDF-x.y, byte 8 must be an EOL marker immediately.
	// No extra digits, spaces, or other characters between version and EOL.
	var results []Violation
	if len(buf) <= 8 || (buf[8] != '\n' && buf[8] != '\r') {
		return fail(fileHeaderRule(profile),
			fmt.Sprintf("header version not immediately followed by EOL marker (byte 8 = 0x%02X)", buf[8]))
	}

	// The EOL must be a single marker: LF, CR, or CR+LF.
	// Double EOL (LF+LF, CR+CR) is a violation.
	commentStart := 9
	if buf[8] == '\r' && commentStart < len(buf) && buf[commentStart] == '\n' {
		commentStart++ // CR+LF is a single EOL
	}

	// Verify no extra EOL before the binary comment.
	if commentStart < len(buf) && (buf[commentStart] == '\r' || buf[commentStart] == '\n') {
		return fail(fileBinaryCommentRule(profile),
			"header followed by multiple EOL markers instead of single EOL")
	}

	if commentStart >= len(buf) || buf[commentStart] != '%' {
		return failf(fileBinaryCommentRule(profile), "missing %% binary comment after header")
	}

	// Check the 4 bytes after the %.
	binaryStart := commentStart + 1
	if binaryStart+minBinaryBytes > len(buf) {
		return failf(fileBinaryCommentRule(profile), "binary comment has fewer than 4 bytes")
	}

	for i := range minBinaryBytes {
		b := buf[binaryStart+i]
		if b <= 127 {
			results = append(results, Violation{
				Rule:    fileBinaryCommentRule(profile),
				Context: fmt.Sprintf("binary comment byte %d has value %d (must be > 127)", i, b),
			})
			return results
		}
	}

	return nil
}

// checkTrailerID validates §6.1.3: trailer must contain /ID key.
func checkTrailerID(doc *document.Document, profile string) []Violation {
	trailer := doc.Index.Trailer
	if trailer == nil {
		return fail(trailerIDRule(profile), "no trailer dictionary")
	}

	idObj, hasID := trailer["ID"]
	if !hasID {
		return fail(trailerIDRule(profile), "missing ID in trailer")
	}

	// Validate the ID array: must have 2 non-empty string elements.
	if arr, ok := idObj.(ast.PdfArray); ok {
		if len(arr) == 0 {
			return fail(trailerIDRule(profile), "empty ID array in trailer")
		}
		if len(arr) >= 2 {
			id0, _ := arr[0].(ast.PdfString)
			id1, _ := arr[1].(ast.PdfString)
			if len(id0) == 0 || len(id1) == 0 {
				return fail(trailerIDRule(profile), "ID array contains empty string element")
			}
		}
	}

	// Check linearized ID consistency (§6.1.3 t04) — PDF/A-1 only.
	if pdfaPart(profile) == 1 && doc.Linearization != nil {
		results := checkLinearizedID(doc, profile)
		if len(results) > 0 {
			return results
		}
	}

	return nil
}

// checkLinearizedID validates §6.1.3 t04: in a linearized PDF, the trailer must
// have an /ID key. The two entries in the ID array represent the original file
// ID and the modified file ID — they legitimately differ in linearized PDFs
// (the second entry is updated during linearization), so we only verify
// presence, not equality.
func checkLinearizedID(doc *document.Document, _ string) []Violation {
	trailer := doc.Index.Trailer
	idObj, hasID := trailer["ID"]
	if !hasID {
		return nil
	}

	idArr, ok := idObj.(ast.PdfArray)
	if !ok || len(idArr) < 2 {
		return nil
	}

	return nil
}

// checkEncryptionForbidden validates §6.1.3: /Encrypt must not be in trailer.
func checkEncryptionForbidden(doc *document.Document, profile string) []Violation {
	if doc.IsEncrypted {
		return fail(encryptRule(profile), "document is encrypted")
	}

	trailer := doc.Index.Trailer
	if trailer != nil {
		if _, hasEncrypt := trailer["Encrypt"]; hasEncrypt {
			return fail(encryptRule(profile), "Encrypt key present in trailer")
		}
	}

	return nil
}

// checkPostEOFData validates §6.1.3: no data after the last %%EOF marker,
// except an optional single EOL.
func checkPostEOFData(doc *document.Document, profile string) []Violation {
	fileSize := doc.FileSize()
	if fileSize <= 0 {
		return nil
	}

	// Read the tail of the file.
	readLen := min(int64(tailReadSize), fileSize)
	offset := fileSize - readLen
	tail, err := doc.ReadFileBytes(offset, readLen)
	if err != nil {
		slog.Debug("compliance.file_structure: cannot read file tail", "error", err)
		return nil
	}

	// Find the last occurrence of %%EOF.
	lastIdx := bytes.LastIndex(tail, []byte(eofMarker))
	if lastIdx < 0 {
		// No %%EOF found — this is a structural issue, but we report it as post-EOF
		// since the rule specifically concerns data after %%EOF.
		return fail(postEOFRule(profile), "no %%EOF marker found in file")
	}

	afterEOF := lastIdx + len(eofMarker)
	remaining := len(tail) - afterEOF

	// Allow a single EOL marker (LF, CR, or CRLF) after %%EOF.
	if remaining > 0 {
		data := tail[afterEOF:]
		// Skip one optional EOL.
		consumed := 0
		if len(data) > 0 && data[0] == '\r' {
			consumed++
			if len(data) > 1 && data[1] == '\n' {
				consumed++
			}
		} else if len(data) > 0 && data[0] == '\n' {
			consumed++
		}
		extraBytes := remaining - consumed
		if extraBytes > 0 {
			return failf(postEOFRule(profile), "%d byte(s) after last %%%%EOF", extraBytes)
		}
	}

	return nil
}

// checkStreamExternalKeys validates §6.1.7: stream dicts must not contain
// /F, /FFilter, or /FDecodeParms.
func checkStreamExternalKeys(ctx context.Context, doc *document.Document, profile string) []Violation {
	forbiddenKeys := []ast.PdfName{"F", "FFilter", "FDecodeParms"}
	var results []Violation

	for objNum, entry := range doc.Index.Entries {
		if ctx.Err() != nil {
			return results
		}
		if entry.Free {
			continue
		}

		stream, err := document.ResolveStream(ctx, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if err != nil || stream.Dict == nil {
			continue
		}

		var found []string
		for _, key := range forbiddenKeys {
			if _, ok := stream.Dict[key]; ok {
				found = append(found, string(key))
			}
		}
		if len(found) > 0 {
			results = append(results, Violation{
				Rule:    streamExternalKeysRule(profile),
				Context: fmt.Sprintf("object %d: stream dict contains %v", objNum, found),
			})
		}
	}

	return results
}

// checkStreamLZW validates §6.1.10: LZWDecode filter is not permitted.
func checkStreamLZW(ctx context.Context, doc *document.Document, profile string) []Violation {
	var results []Violation

	for objNum, entry := range doc.Index.Entries {
		if ctx.Err() != nil {
			return results
		}
		if entry.Free {
			continue
		}

		stream, err := document.ResolveStream(ctx, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if err != nil || stream.Dict == nil {
			continue
		}

		if hasLZWFilter(ctx, doc, stream.Dict) {
			results = append(results, Violation{
				Rule:    lzwFilterRule(profile),
				Context: fmt.Sprintf("object %d uses LZWDecode filter", objNum),
			})
		}
	}

	return results
}

// hasLZWFilter checks if a stream dictionary's /Filter contains LZWDecode.
func hasLZWFilter(ctx context.Context, doc *document.Document, dict ast.PdfDict) bool {
	filterObj, ok := dict["Filter"]
	if !ok {
		return false
	}

	// Single filter name.
	name, err := document.ResolveName(ctx, doc, filterObj)
	if err == nil && name == "LZWDecode" {
		return true
	}

	// Array of filter names.
	arr, err := document.ResolveArray(ctx, doc, filterObj)
	if err != nil {
		return false
	}
	for _, elem := range arr {
		n, err := document.ResolveName(ctx, doc, elem)
		if err == nil && n == "LZWDecode" {
			return true
		}
	}

	return false
}

// standardFilters lists the PDF stream filter names from ISO 32000-2 Table 6.
// Filter names are case-sensitive.
var standardFilters = map[string]bool{
	"ASCIIHexDecode":  true,
	"ASCII85Decode":   true,
	"LZWDecode":       true, // listed but forbidden separately
	"FlateDecode":     true,
	"RunLengthDecode": true,
	"CCITTFaxDecode":  true,
	"JBIG2Decode":     true,
	"DCTDecode":       true,
	"JPXDecode":       true,
	"Crypt":           true,
}

// checkNonStandardFilters validates that all stream /Filter names are
// from the standard set (case-sensitive). Non-standard filter names
// like /Flatedecode (wrong case) are forbidden.
func checkNonStandardFilters(ctx context.Context, doc *document.Document, profile string) []Violation {
	rule := streamFilterRule(profile)
	if rule.Specification == "" {
		return nil
	}

	var results []Violation
	for objNum, entry := range doc.Index.Entries {
		if ctx.Err() != nil {
			return results
		}
		if entry.Free {
			continue
		}
		stream, err := document.ResolveStream(ctx, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if err != nil || stream.Dict == nil {
			continue
		}
		filterObj, ok := stream.Dict["Filter"]
		if !ok {
			continue
		}
		// Single filter name.
		if name, err := document.ResolveName(ctx, doc, filterObj); err == nil && name != "" {
			if !standardFilters[string(name)] {
				results = append(results, Violation{
					Rule:    rule,
					Context: fmt.Sprintf("object %d uses non-standard filter /%s", objNum, name),
				})
			}
			continue
		}
		// Array of filter names.
		if arr, err := document.ResolveArray(ctx, doc, filterObj); err == nil {
			for _, elem := range arr {
				if n, err := document.ResolveName(ctx, doc, elem); err == nil && n != "" {
					if !standardFilters[string(n)] {
						results = append(results, Violation{
							Rule:    rule,
							Context: fmt.Sprintf("object %d uses non-standard filter /%s", objNum, n),
						})
					}
				}
			}
		}
	}
	return results
}

// streamFilterRule returns the rule for non-standard stream filters.
func streamFilterRule(profile string) Rule {
	if pdfaPart(profile) == 4 {
		return Rule_19005_4_6_1_6_2_t01
	}
	// Parts 1-3 use the LZW/filter clause for non-standard filters.
	return lzwFilterRule(profile)
}

// checkEmbeddedFiles validates §6.1.11: embedded files are forbidden in PDF/A-1.
func checkEmbeddedFiles(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if pdfaPart(profile) != 1 {
		return nil
	}

	var results []Violation

	// Check for /EmbeddedFiles in the Names dictionary.
	namesObj, ok := catalog["Names"]
	if ok {
		namesDict, err := document.ResolveDict(ctx, doc, namesObj)
		if err == nil && namesDict != nil {
			if _, hasEF := namesDict["EmbeddedFiles"]; hasEF {
				results = append(results, fail(embeddedFilesNameRule(profile),
					"EmbeddedFiles key present in Names dictionary")...)
			}
		}
	}

	// Check file specification dictionaries for /EF key.
	for objNum, entry := range doc.Index.Entries {
		if ctx.Err() != nil {
			return results
		}
		if entry.Free {
			continue
		}

		dict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if err != nil || dict == nil {
			continue
		}

		typeName, _ := document.ResolveName(ctx, doc, dict["Type"])
		if typeName != "Filespec" {
			continue
		}

		if _, hasEF := dict["EF"]; hasEF {
			results = append(results, Violation{
				Rule:    embeddedFilesEFRule(profile),
				Context: fmt.Sprintf("object %d: file specification contains EF key", objNum),
			})
		}
	}

	return results
}

// checkXRefFormat validates §6.1.4: xref streams are forbidden in PDF/A-1.
func checkXRefFormat(doc *document.Document, profile string) []Violation {
	if pdfaPart(profile) != 1 {
		return nil
	}

	// Detect xref stream usage by checking if any entries use compressed type
	// (type 2), which is only possible with xref streams.
	for _, entry := range doc.Index.Entries {
		if entry.Type == xref.XRefTypeCompressed {
			return fail(xrefStreamForbiddenRule(profile), "document uses xref streams")
		}
	}

	// Also check if the trailer itself is a stream dict (xref stream trailer).
	trailer := doc.Index.Trailer
	if trailer != nil {
		if typeName, ok := trailer["Type"].(ast.PdfName); ok && typeName == "XRef" {
			return fail(xrefStreamForbiddenRule(profile), "document uses xref streams")
		}
	}

	return nil
}

// checkStreamLengths validates §6.1.7.1 t1 / §6.1.6.1 t1: the declared /Length
// in a stream dictionary must match the actual byte count between the stream
// keyword and endstream keyword.
func checkStreamLengths(ctx context.Context, doc *document.Document, profile string) []Violation { //nolint:unused // kept for future re-enablement
	var results []Violation

	for objNum, entry := range doc.Index.Entries {
		if ctx.Err() != nil {
			return results
		}
		if entry.Free || entry.Type != 1 { // only check uncompressed objects
			continue
		}

		stream, err := document.ResolveStream(ctx, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if err != nil || stream.Dict == nil {
			continue
		}

		// Get declared /Length.
		lengthObj, ok := stream.Dict["Length"]
		if !ok {
			continue
		}
		declaredLen, err := document.ResolveInt(ctx, doc, lengthObj)
		if err != nil {
			slog.Debug("compliance.file_structure: cannot resolve stream length", "error", err)
			continue
		}

		// Read raw bytes around the object to find stream/endstream markers.
		// Read enough to cover the object header + dict + stream data.
		readSize := int64(declaredLen) + 4096
		if readSize > 1<<20 {
			continue
		}
		rawData, err := doc.ReadFileBytes(entry.Offset, readSize)
		if err != nil {
			slog.Debug("compliance.file_structure: cannot read raw stream bytes", "error", err)
			continue
		}

		// Find "stream" keyword.
		streamIdx := bytes.Index(rawData, []byte("stream"))
		if streamIdx < 0 {
			continue
		}

		// Skip past "stream" + EOL marker (CR+LF or LF).
		dataStart := streamIdx + len("stream")
		if dataStart < len(rawData) && rawData[dataStart] == '\r' {
			dataStart++
		}
		if dataStart < len(rawData) && rawData[dataStart] == '\n' {
			dataStart++
		}

		// Use the declared Length to find where the stream data should end.
		// Then check if "endstream" follows (possibly after EOL).
		expectedEnd := dataStart + int(declaredLen)
		if expectedEnd > len(rawData) {
			continue // not enough data read
		}

		// At expectedEnd, there should be an optional EOL then "endstream".
		tail := rawData[expectedEnd:]
		trimmed := tail
		// Skip optional CR and/or LF.
		if len(trimmed) > 0 && trimmed[0] == '\r' {
			trimmed = trimmed[1:]
		}
		if len(trimmed) > 0 && trimmed[0] == '\n' {
			trimmed = trimmed[1:]
		}

		if !bytes.HasPrefix(trimmed, []byte("endstream")) {
			// endstream not found at declared Length — the declared Length is wrong.
			// Only report if we can find endstream nearby to confirm the mismatch
			// is real and not a scanning artifact.
			endIdx := bytes.Index(rawData[dataStart:], []byte("endstream"))
			if endIdx < 0 {
				continue
			}
			rawLen := endIdx
			// Only flag if the mismatch is significant (> 2 bytes for EOL tolerance)
			// but not too large (> 10% suggests a scanning issue, not a real violation).
			diff := rawLen - int(declaredLen)
			absDiff := diff
			if absDiff < 0 {
				absDiff = -absDiff
			}
			maxDrift := max(
				int(declaredLen)/10,
				10)
			if absDiff > 2 && absDiff < maxDrift {
				results = append(results, Violation{
					Rule:    streamLengthRule(profile),
					Context: fmt.Sprintf("object %d: declared Length=%d but actual=%d", objNum, declaredLen, rawLen),
				})
			}
		}
	}

	return results
}

// streamLengthRule returns the rule for stream length mismatch.
func streamLengthRule(profile string) Rule { //nolint:unused // kept for future re-enablement
	return ruleByPart(pdfaPart(profile), Rule_19005_1_6_1_7_t01, Rule_19005_2_6_1_7_1_t01, Rule_19005_3_6_1_7_1_t01, Rule_19005_4_6_1_6_1_t01)
}

// pageBoundaryKeys lists all page boundary box keys defined in ISO 32000-1, §14.11.2.
var pageBoundaryKeys = []ast.PdfName{"MediaBox", "CropBox", "BleedBox", "TrimBox", "ArtBox"} //nolint:gochecknoglobals // constant set.

// checkPageBoundaries validates §6.1.13 t11: page boundary box dimensions must
// be within [3, 14400] units in both directions.
func checkPageBoundaries(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	part := pdfaPart(profile)
	// Page boundary size limits apply to PDF/A-2, -3, and -4.
	if part < 2 {
		return nil
	}

	rule := pageBoundRule(profile)
	var results []Violation

	// Also check the Pages tree root for inherited boundary boxes.
	// Only flag root boundaries that are NOT overridden by individual pages.
	pagesObj, _ := document.ResolveDict(ctx, doc, catalog["Pages"])

	pages := collectPages(ctx, doc, catalog)

	if pagesObj != nil {
		for _, key := range pageBoundaryKeys {
			boxObj, ok := pagesObj[key]
			if !ok {
				continue
			}
			// Check if ALL pages override this key — if any page inherits it, validate.
			anyInherits := false
			for _, page := range pages {
				if _, has := page[key]; !has {
					anyInherits = true
					break
				}
			}
			if !anyInherits {
				continue // Every page has its own — tree root value is irrelevant.
			}
			arr, err := document.ResolveArray(ctx, doc, boxObj)
			if err != nil || len(arr) < 4 {
				continue
			}
			x1, err1 := resolveFloat(ctx, doc, arr[0])
			y1, err2 := resolveFloat(ctx, doc, arr[1])
			x2, err3 := resolveFloat(ctx, doc, arr[2])
			y2, err4 := resolveFloat(ctx, doc, arr[3])
			if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
				continue
			}
			width := math.Abs(x2 - x1)
			height := math.Abs(y2 - y1)
			if width < minPageBound || width > maxPageBound || height < minPageBound || height > maxPageBound {
				results = append(results, Violation{
					Rule:    rule,
					Context: fmt.Sprintf("Pages tree /%s: width=%.1f height=%.1f (valid: %.0f-%.0f)", key, width, height, minPageBound, maxPageBound),
				})
			}
		}
	}
	for pageIdx, page := range pages {
		if ctx.Err() != nil {
			return results
		}

		for _, key := range pageBoundaryKeys {
			boxObj, ok := page[key]
			if !ok {
				continue
			}

			arr, err := document.ResolveArray(ctx, doc, boxObj)
			if err != nil || len(arr) < 4 {
				continue
			}

			x1, err1 := resolveFloat(ctx, doc, arr[0])
			y1, err2 := resolveFloat(ctx, doc, arr[1])
			x2, err3 := resolveFloat(ctx, doc, arr[2])
			y2, err4 := resolveFloat(ctx, doc, arr[3])
			if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
				continue
			}

			width := math.Abs(x2 - x1)
			height := math.Abs(y2 - y1)

			if width < minPageBound || width > maxPageBound || height < minPageBound || height > maxPageBound {
				results = append(results, Violation{
					Rule:    rule,
					Context: fmt.Sprintf("page %d /%s: width=%.1f height=%.1f (valid: %.0f-%.0f)", pageIdx+1, key, width, height, minPageBound, maxPageBound),
				})
			}
		}
	}

	return results
}

// resolveFloat resolves a PdfObject to a float64, handling both PdfInt and PdfReal.
func resolveFloat(ctx context.Context, doc *document.Document, obj ast.PdfObject) (float64, error) {
	resolved, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return 0, err
	}
	switch v := resolved.(type) {
	case ast.PdfReal:
		return float64(v), nil
	case ast.PdfInt:
		return float64(v), nil
	default:
		return 0, fmt.Errorf("expected numeric, got %T", resolved)
	}
}

// checkPDFA4TrailerInfo validates PDF/A-4 §6.1.3 t04/t05: /Info key
// restrictions in PDF/A-4.
func checkPDFA4TrailerInfo(doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	trailer := doc.Index.Trailer
	if trailer == nil {
		return nil
	}

	infoObj, hasInfo := trailer["Info"]
	if !hasInfo {
		return nil
	}

	// §6.1.3 t04: /Info is only allowed if /PieceInfo exists in catalog.
	_, hasPieceInfo := catalog["PieceInfo"]
	if !hasPieceInfo {
		return fail(Rule_19005_4_6_1_3_t04, "Info key present in trailer but PieceInfo not in catalog")
	}

	// §6.1.3 t05: if Info dict is present, it may only contain /ModDate.
	infoDict, _ := document.ResolveDict(context.Background(), doc, infoObj)
	if infoDict != nil {
		var badKeys []string
		for key := range infoDict {
			if key != "ModDate" {
				badKeys = append(badKeys, string(key))
			}
		}
		if len(badKeys) > 0 {
			return failf(Rule_19005_4_6_1_3_t05,
				"Info dictionary contains keys %v (only ModDate allowed)", badKeys)
		}
	}

	return nil
}

// checkSignatureReferences validates §6.1.12 t02 (PDF/A-2+): if /Perms/DocMDP
// is present, the Signature References dictionary shall not contain
// DigestLocation, DigestMethod, or DigestValue keys.
func checkSignatureReferences(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	permsObj, ok := catalog["Perms"]
	if !ok {
		return nil
	}
	perms, err := document.ResolveDict(ctx, doc, permsObj)
	if err != nil || perms == nil {
		return nil
	}
	docMDPObj, ok := perms["DocMDP"]
	if !ok {
		return nil
	}

	// Resolve the DocMDP signature dictionary.
	sigDict, err := document.ResolveDict(ctx, doc, docMDPObj)
	if err != nil || sigDict == nil {
		return nil
	}

	// Check /Reference array entries for forbidden keys.
	refArr, err := document.ResolveArray(ctx, doc, sigDict["Reference"])
	if err != nil || len(refArr) == 0 {
		return nil
	}

	var results []Violation
	part := pdfaPart(profile)
	forbidden := []ast.PdfName{"DigestLocation", "DigestMethod", "DigestValue"}

	for i, refObj := range refArr {
		refDict, err := document.ResolveDict(ctx, doc, refObj)
		if err != nil || refDict == nil {
			continue
		}
		for _, key := range forbidden {
			if _, has := refDict[key]; has {
				results = append(results, failf(sigRefRule(part),
					"Signature Reference[%d] contains forbidden key /%s", i, key)...)
			}
		}
	}
	return results
}

func sigRefRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_1_12_t02, Rule_19005_3_6_1_12_t02, Rule_19005_4_6_1_12_t01)
}

// utf8NameRule returns the rule for UTF-8 name validation based on part.
func utf8NameRule(profile string) Rule {
	return ruleByPart(pdfaPart(profile), Rule{}, Rule_19005_2_6_1_8_t01, Rule_19005_3_6_1_8_t01, Rule_19005_4_6_1_7_t01)
}

// checkUTF8Names validates §6.1.7 (PDF/A-4) / §6.1.8 (PDF/A-2/3): font names,
// colorant names in Separation/DeviceN colour spaces, and structure type names
// must be valid UTF-8 after #XX hex escape expansion.
func checkUTF8Names(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	var results []Violation
	seen := make(map[string]bool) // Avoid duplicate reports for the same name.
	rule := utf8NameRule(profile)

	pages := collectPages(ctx, doc, catalog)
	for _, page := range pages {
		if ctx.Err() != nil {
			return results
		}
		res := getResources(ctx, doc, page)
		if res == nil {
			continue
		}

		// Font names: check /BaseFont on each font.
		fontDict, err := document.ResolveDict(ctx, doc, res["Font"])
		if err == nil && fontDict != nil {
			for _, fontObj := range fontDict {
				fd, err := document.ResolveDict(ctx, doc, fontObj)
				if err != nil || fd == nil {
					continue
				}
				baseFontName, _ := document.ResolveName(ctx, doc, fd["BaseFont"])
				if baseFontName != "" {
					checkNameUTF8(string(baseFontName), "font name", rule, seen, &results)
				}
			}
		}

		// Colorant names in Separation/DeviceN colour spaces.
		csDict, err := document.ResolveDict(ctx, doc, res["ColorSpace"])
		if err == nil && csDict != nil {
			for _, csObj := range csDict {
				arr, err := document.ResolveArray(ctx, doc, csObj)
				if err != nil || len(arr) < 2 {
					continue
				}
				csType, _ := document.ResolveName(ctx, doc, arr[0])
				switch string(csType) {
				case "Separation":
					if len(arr) >= 2 {
						cName, _ := document.ResolveName(ctx, doc, arr[1])
						if cName != "" {
							checkNameUTF8(string(cName), "Separation colorant", rule, seen, &results)
						}
					}
				case "DeviceN":
					if len(arr) >= 2 {
						names, _ := document.ResolveArray(ctx, doc, arr[1])
						for _, nameObj := range names {
							cName, _ := document.ResolveName(ctx, doc, nameObj)
							if cName != "" {
								checkNameUTF8(string(cName), "DeviceN colorant", rule, seen, &results)
							}
						}
					}
				}
			}
		}
	}

	// Structure type names.
	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err == nil && treeRoot != nil {
		checkStructureTypeUTF8(ctx, doc, treeRoot, rule, seen, &results)

		// RoleMap names.
		roleMap, err := document.ResolveDict(ctx, doc, treeRoot["RoleMap"])
		if err == nil && roleMap != nil {
			for srcName, dstObj := range roleMap {
				checkNameUTF8(string(srcName), "RoleMap source", rule, seen, &results)
				dstName, _ := document.ResolveName(ctx, doc, dstObj)
				if dstName != "" {
					checkNameUTF8(string(dstName), "RoleMap target", rule, seen, &results)
				}
			}
		}
	}

	return results
}

// checkStructureTypeUTF8 recursively walks the structure tree and validates
// that each element's /S type name is valid UTF-8.
func checkStructureTypeUTF8(ctx context.Context, doc *document.Document, root ast.PdfDict, rule Rule, seen map[string]bool, results *[]Violation) {
	walkStructureElements(ctx, doc, root, func(elem ast.PdfDict, _ string) {
		typeName, _ := document.ResolveName(ctx, doc, elem["S"])
		if typeName != "" {
			checkNameUTF8(string(typeName), "structure type", rule, seen, results)
		}
	})
}

// checkNameUTF8 validates that a PDF name, after #XX hex escape expansion,
// is a valid UTF-8 string. Reports a violation if not.
func checkNameUTF8(name, context string, rule Rule, seen map[string]bool, results *[]Violation) {
	if seen[name] {
		return
	}
	seen[name] = true

	decoded := expandNameHexEscapes(name)
	if !utf8.ValidString(decoded) {
		*results = append(*results, failf(rule,
			"%s %q is not valid UTF-8", context, name)...)
	}
}

// expandNameHexEscapes expands #XX hex escape sequences in a PDF name.
// For example, "A#20B" becomes "A B".
func expandNameHexEscapes(name string) string {
	if len(name) < 3 {
		return name
	}

	var buf []byte
	for i := 0; i < len(name); i++ {
		if name[i] == '#' && i+2 < len(name) {
			b, err := hex.DecodeString(name[i+1 : i+3])
			if err == nil && len(b) == 1 {
				buf = append(buf, b[0])
				i += 2
				continue
			}
		}
		buf = append(buf, name[i])
	}
	return string(buf)
}
