package compliance

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"slices"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
)

const (
	headerReadSize = 64
	minBinaryBytes = 4
	eofMarker      = "%%EOF"
	tailReadSize   = 4096
	minPageBound   = 3.0
	maxPageBound   = 14400.0
)

// sortedEntryNums returns the document's cross-reference object numbers in
// ascending order. Per-object structure checks iterate it instead of ranging
// the entries map directly, so violations emit in a deterministic order
// (CONTRIBUTING "Deterministic output").
func sortedEntryNums(doc *document.Document) []int {
	nums := make([]int, 0, len(doc.Index.Entries))
	for objNum := range doc.Index.Entries {
		nums = append(nums, objNum)
	}
	slices.Sort(nums)
	return nums
}

// checkFileStructureGroup validates PDF file structure rules (§6.1).
func checkFileStructureGroup(ctx context.Context, vctx *validationCtx) []Violation {
	doc, catalog, profile := vctx.doc, vctx.catalog, vctx.profile
	part := vctx.part

	doc.Logger.Debug("compliance.file_structure: starting checks", "profile", profile)

	var results []Violation

	results = append(results, checkFileHeader(ctx, doc, profile)...)
	results = append(results, checkTrailerID(doc, profile)...)
	results = append(results, checkEncryptionForbidden(doc, profile)...)
	results = append(results, checkPostEOFData(ctx, doc, profile)...)
	results = append(results, checkStreamExternalKeys(ctx, doc, profile)...)
	results = append(results, checkStreamLZW(ctx, doc, profile)...)
	results = append(results, checkEmbeddedFiles(ctx, doc, catalog, profile)...)
	results = append(results, checkFormatViolations(doc, profile)...)
	results = append(results, checkObjHeaderLeadingWhitespace(doc, profile)...)
	results = append(results, checkStreamLengths(ctx, doc, profile)...)
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
		results = append(results, checkPDFA4TrailerInfo(ctx, doc, catalog, profile)...)
	}

	doc.Logger.Debug("compliance.file_structure: checks complete",
		"profile", profile,
		"violations", len(results),
	)
	return results
}

func fileHeaderRule(profile string) Rule {
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_1_2_t01, Rule_19005_2_6_1_2_t01, Rule_19005_3_6_1_2_t01, Rule_19005_4_6_1_2_t01)
}

func fileBinaryCommentRule(profile string) Rule {
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_1_2_t02, Rule_19005_2_6_1_2_t02, Rule_19005_3_6_1_2_t02, Rule_19005_4_6_1_2_t02)
}

func trailerIDRule(profile string) Rule {
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_1_3_t01, Rule_19005_2_6_1_3_t01, Rule_19005_3_6_1_3_t01, Rule_19005_4_6_1_3_t01)
}

func encryptRule(profile string) Rule {
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_1_3_t02, Rule_19005_2_6_1_3_t02, Rule_19005_3_6_1_3_t02, Rule_19005_4_6_1_3_t02)
}

func postEOFRule(profile string) Rule {
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_1_3_t03, Rule_19005_2_6_1_3_t03, Rule_19005_3_6_1_3_t03, Rule_19005_4_6_1_3_t03)
}

func streamExternalKeysRule(profile string) Rule {
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_1_7_t03, Rule_19005_2_6_1_7_1_t03, Rule_19005_3_6_1_7_1_t03, Rule_19005_4_6_1_6_1_t02)
}

func lzwFilterRule(profile string) Rule {
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_1_10_t01, Rule_19005_2_6_1_7_2_t01, Rule_19005_3_6_1_7_2_t01, Rule_19005_4_6_1_6_2_t01)
}

func embeddedFilesEFRule(_ string) Rule {
	return Rule_19005_1_6_1_11_t01
}

func embeddedFilesNameRule(_ string) Rule {
	return Rule_19005_1_6_1_11_t02
}

func pageBoundRule(profile string) Rule {
	// PDF/A-1 uses the same clause numbering as part 2 for this check.
	return ruleByPart(PDFAPart(profile), Rule_19005_2_6_1_13_t11, Rule_19005_2_6_1_13_t11, Rule_19005_3_6_1_13_t11, Rule{})
}

// checkFileHeader validates §6.1.2: file must start with %PDF-x.y and have
// a binary comment marker.
func checkFileHeader(ctx context.Context, doc *document.Document, profile string) []Violation {
	buf, err := safeReadHeader(ctx, doc, headerReadSize)
	if err != nil {
		doc.Logger.Debug("compliance.file_structure: cannot read header", "error", err)
		return nil
	}
	return checkHeaderBytes(buf, profile)
}

// checkHeaderBytes validates the §6.1.2 header rules on the leading file bytes:
// t1 (the %PDF-x.y version line) and t2 (the binary comment on the next line).
// The two are independent, so a malformed version line still lets the binary
// comment check run.
func checkHeaderBytes(buf []byte, profile string) []Violation {
	part := PDFAPart(profile)

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

	// After %PDF-x.y, byte 8 should be an EOL marker immediately. A trailing
	// character (e.g. a stray space) is a header-format violation, but the
	// §6.1.2 t2 binary comment on the next line is evaluated independently:
	// veraPDF reports both, so record the format issue and scan on to the EOL
	// rather than stopping here.
	var results []Violation
	eol := 8
	if buf[eol] != '\n' && buf[eol] != '\r' {
		results = append(results, fail(fileHeaderRule(profile),
			fmt.Sprintf("header version not immediately followed by EOL marker (byte 8 = 0x%02X)", buf[8]))...)
		for eol < len(buf) && buf[eol] != '\n' && buf[eol] != '\r' {
			eol++
		}
		if eol >= len(buf) {
			return results
		}
	}

	// The EOL must be a single marker: LF, CR, or CR+LF.
	commentStart := eol + 1
	if buf[eol] == '\r' && commentStart < len(buf) && buf[commentStart] == '\n' {
		commentStart++ // CR+LF is a single EOL
	}

	// Verify no extra EOL before the binary comment. Only meaningful when the
	// header line ended cleanly at byte 8; a scanned-past trailing run already
	// consumed any stray characters.
	if eol == 8 && commentStart < len(buf) && (buf[commentStart] == '\r' || buf[commentStart] == '\n') {
		return append(results, fail(fileBinaryCommentRule(profile),
			"header followed by multiple EOL markers instead of single EOL")...)
	}

	if commentStart >= len(buf) || buf[commentStart] != '%' {
		return append(results, failf(fileBinaryCommentRule(profile), "missing %% binary comment after header")...)
	}

	// Check the 4 bytes after the %.
	binaryStart := commentStart + 1
	if binaryStart+minBinaryBytes > len(buf) {
		return append(results, failf(fileBinaryCommentRule(profile), "binary comment has fewer than 4 bytes")...)
	}

	for i := range minBinaryBytes {
		b := buf[binaryStart+i]
		if b <= 127 {
			return append(results, Violation{
				Rule:    fileBinaryCommentRule(profile),
				Context: fmt.Sprintf("binary comment byte %d has value %d (must be > 127)", i, b),
			})
		}
	}

	return results
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

	// Check linearized ID consistency (§6.1.3 t04). PDF/A-1 only.
	if PDFAPart(profile) == 1 && doc.Linearization != nil {
		results := checkLinearizedID(doc, profile)
		if len(results) > 0 {
			return results
		}
	}

	return nil
}

// checkLinearizedID validates §6.1.3 t04: in a linearized PDF, the trailer must
// have an /ID key. The two entries in the ID array represent the original file
// ID and the modified file ID. They legitimately differ in linearized PDFs
// (the second entry is updated during linearization), so only presence is
// checked, not equality.
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
func checkPostEOFData(ctx context.Context, doc *document.Document, profile string) []Violation {
	fileSize := doc.FileSize()
	if fileSize <= 0 {
		return nil
	}

	// Read the tail of the file.
	readLen := min(int64(tailReadSize), fileSize)
	offset := fileSize - readLen
	tail, err := doc.ReadFileBytes(ctx, offset, readLen)
	if err != nil {
		doc.Logger.Debug("compliance.file_structure: cannot read file tail", "error", err)
		return nil
	}

	// Find the last occurrence of %%EOF.
	lastIdx := bytes.LastIndex(tail, []byte(eofMarker))
	if lastIdx < 0 {
		// No %%EOF found. The rule specifically concerns data after %%EOF,
		// so report this structural issue under the post-EOF clause.
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

	for _, objNum := range sortedEntryNums(doc) {
		entry := doc.Index.Entries[objNum]
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

	for _, objNum := range sortedEntryNums(doc) {
		entry := doc.Index.Entries[objNum]
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
//
// This is scoped to parts 2-4. ISO 19005-1 §6.1.10 restricts only the LZWDecode
// filter (handled by [checkStreamLZW]); it imposes no general standard-filter
// requirement, so a non-LZW non-standard name (for example /Flatedecode with the
// wrong case) is not a filter violation under PDF/A-1. Parts 2-4 (§6.1.7.2 /
// §6.1.6.2) do require the standard filter set, so the same name fails there.
func checkNonStandardFilters(ctx context.Context, doc *document.Document, profile string) []Violation {
	if PDFAPart(profile) == 1 {
		return nil
	}
	rule := streamFilterRule(profile)
	if rule.Specification == "" {
		return nil
	}

	var results []Violation
	for _, objNum := range sortedEntryNums(doc) {
		entry := doc.Index.Entries[objNum]
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
	if PDFAPart(profile) == 4 {
		return Rule_19005_4_6_1_6_2_t01
	}
	// Parts 1-3 use the LZW/filter clause for non-standard filters.
	return lzwFilterRule(profile)
}

// checkEmbeddedFiles validates §6.1.11: embedded files are forbidden in PDF/A-1.
func checkEmbeddedFiles(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if PDFAPart(profile) != 1 {
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
	for _, objNum := range sortedEntryNums(doc) {
		entry := doc.Index.Entries[objNum]
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
	if PDFAPart(profile) != 1 {
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
//
// Approach: for each uncompressed stream object, use the xref offset to read
// the object header and locate the "stream" keyword. Then use the declared
// /Length to seek directly to where "endstream" should appear. If "endstream"
// is not found at the expected position, the declared /Length is wrong.
//
// This avoids scanning the entire stream data for "endstream", which was
// fragile due to binary data containing the keyword or EOL normalization
// differences between producers.
func checkStreamLengths(ctx context.Context, doc *document.Document, profile string) []Violation {
	var results []Violation
	rule := streamLengthRule(profile)

	for _, objNum := range sortedEntryNums(doc) {
		entry := doc.Index.Entries[objNum]
		if ctx.Err() != nil {
			return results
		}
		if entry.Free || entry.Type != xref.XRefTypeNormal {
			continue
		}

		// Resolve the object to check if it's a stream with a /Length key.
		stream, err := document.ResolveStream(ctx, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if err != nil || stream.Dict == nil {
			continue
		}

		lengthObj, ok := stream.Dict["Length"]
		if !ok {
			continue
		}

		declaredLen, err := document.ResolveInt(ctx, doc, lengthObj)
		if err != nil {
			doc.Logger.Debug("compliance.file_structure: cannot resolve stream length", "error", err)
			continue
		}

		if declaredLen < 0 {
			results = append(results, Violation{
				Rule:    rule,
				Context: fmt.Sprintf("object %d: negative /Length=%d", objNum, declaredLen),
			})
			continue
		}

		// Phase 1: Read a small header window (up to 4 KiB) from the xref
		// offset to find the "stream" keyword position within the object.
		const headerBufSize = 4096
		headerBuf, err := doc.ReadFileBytes(ctx, entry.Offset, headerBufSize)
		if err != nil {
			continue
		}

		// Find the "stream" keyword that is NOT part of "endstream".
		streamDataStart := findStreamDataStart(ctx, headerBuf)
		if streamDataStart < 0 {
			continue // not a stream, or header too small to find it
		}

		// Phase 2: Read a small window at where "endstream" should appear.
		// The absolute file position of stream data start:
		absDataStart := entry.Offset + int64(streamDataStart)
		// The expected end of stream data:
		absExpectedEnd := absDataStart + int64(declaredLen)

		// Read enough bytes to find "endstream" after optional EOL.
		// "endstream" is 9 bytes, plus up to 2 EOL bytes = 11 bytes needed.
		const tailBufSize = 32
		tailBuf, err := doc.ReadFileBytes(ctx, absExpectedEnd, tailBufSize)
		if err != nil {
			continue
		}

		if !hasEndstreamAt(tailBuf) {
			results = append(results, Violation{
				Rule:    rule,
				Context: fmt.Sprintf("object %d: declared /Length=%d does not match actual stream data extent", objNum, declaredLen),
			})
		}

		// §6.1.7 t2 / §6.1.7.1 t2: the endstream keyword shall be immediately
		// preceded by an EOL marker. veraPDF reports extra whitespace inserted
		// between the EOL and endstream independently of the /Length check. The
		// check is conservative: it fires only when a valid EOL is present but
		// separated from endstream by spaces or tabs, not when the EOL is absent
		// entirely (a shape lenient real-world producers emit), to avoid a false
		// positive the verapdf-only gate could not surface.
		if eolRule := streamEOLRule(profile); eolRule.Specification != "" && endstreamHasExtraSpacing(tailBuf) {
			results = append(results, Violation{
				Rule:    eolRule,
				Context: fmt.Sprintf("object %d: endstream keyword not immediately preceded by an EOL marker", objNum),
			})
		}
	}

	return results
}

// endstreamHasExtraSpacing reports whether the first endstream keyword in buf is
// separated from its preceding EOL marker by spaces or tabs (ISO 19005 §6.1.7 t2
// / §6.1.7.1 t2: "...\r\n   endstream"). It deliberately does not fire when no
// EOL precedes endstream at all, keeping the check to the extra-spacing defect.
func endstreamHasExtraSpacing(buf []byte) bool {
	idx := bytes.Index(buf, []byte("endstream"))
	if idx < 2 {
		return false
	}
	if buf[idx-1] != ' ' && buf[idx-1] != '\t' {
		return false
	}
	j := idx - 1
	for j >= 0 && (buf[j] == ' ' || buf[j] == '\t') {
		j--
	}
	return j >= 0 && (buf[j] == '\n' || buf[j] == '\r')
}

// findStreamDataStart locates the byte offset within buf where stream data
// begins (immediately after the "stream" keyword and its required EOL).
// Returns -1 if the "stream" keyword is not found in buf or when ctx is
// cancelled mid-scan; outer callers re-check ctx.
func findStreamDataStart(ctx context.Context, buf []byte) int {
	// Scan for the "stream" keyword. Skip occurrences that are part of
	// "endstream" or part of a PDF name or other token
	// (e.g., /application#2Foctet-stream).
	// Per ISO 32000, the stream keyword is a standalone keyword preceded by
	// whitespace or a delimiter (typically ">>" closing the stream dictionary)
	// and followed immediately by a single EOL (CR+LF or LF).
	searchFrom := 0
	for {
		if ctx.Err() != nil {
			return -1
		}
		idx := bytes.Index(buf[searchFrom:], []byte("stream"))
		if idx < 0 {
			return -1
		}
		absIdx := searchFrom + idx

		// Skip "endstream".
		if absIdx >= 3 && bytes.Equal(buf[absIdx-3:absIdx], []byte("end")) {
			searchFrom = absIdx + 6
			continue
		}

		// The stream keyword must be preceded by whitespace or a PDF delimiter
		// to be a real keyword (not embedded in a name like /octet-stream).
		if absIdx > 0 && !isPdfWhitespaceOrDelim(buf[absIdx-1]) {
			searchFrom = absIdx + 6
			continue
		}

		afterKeyword := absIdx + 6 // position right after "stream"

		// A conforming stream keyword is followed by CRLF or a single LF; the data
		// then begins after that EOL. A keyword followed by anything else is
		// malformed (ISO 19005 §6.1.7 t2 / §6.1.7.1 t2, reported separately), but
		// veraPDF still takes the stream data to begin immediately after "stream",
		// so the /Length check proceeds from there rather than abandoning the
		// stream and missing the length defect.
		pos := afterKeyword
		if pos < len(buf) && buf[pos] == '\r' {
			pos++
		}
		if pos < len(buf) && buf[pos] == '\n' {
			pos++
		}
		return pos
	}
}

// isPdfWhitespaceOrDelim reports whether b is a PDF whitespace character
// (NUL, TAB, LF, FF, CR, SPACE) or a PDF delimiter character
// ( ) < > [ ] { } / % per ISO 32000-1 §7.2).
func isPdfWhitespaceOrDelim(b byte) bool {
	switch b {
	case 0, '\t', '\n', '\f', '\r', ' ', // whitespace
		'(', ')', '<', '>', '[', ']', '{', '}', '/', '%': // delimiters
		return true
	}
	return false
}

// hasEndstreamAt checks whether buf (read from the expected stream-data end
// position) contains an "endstream" keyword, optionally preceded by EOL bytes.
// Per the PDF spec, the stream data may be followed by an optional EOL (CR,
// LF, or CRLF) before "endstream".
func hasEndstreamAt(buf []byte) bool {
	endstream := []byte("endstream")

	// Try: "endstream" immediately at position 0.
	if bytes.HasPrefix(buf, endstream) {
		return true
	}

	// Try: optional EOL then "endstream".
	off := 0
	if off < len(buf) && buf[off] == '\r' {
		off++
	}
	if off < len(buf) && buf[off] == '\n' {
		off++
	}
	if off > 0 && bytes.HasPrefix(buf[off:], endstream) {
		return true
	}

	// Try: single \n then "endstream" (already covered above if \r was absent).
	// Try: single \r then "endstream" (some producers omit \n).
	// Both cases are covered by the logic above.

	return false
}

// streamLengthRule returns the rule for stream length mismatch.
func streamLengthRule(profile string) Rule {
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_1_7_t01, Rule_19005_2_6_1_7_1_t01, Rule_19005_3_6_1_7_1_t01, Rule_19005_4_6_1_6_1_t01)
}

// pageBoundaryKeys lists all page boundary box keys defined in ISO 32000-1, §14.11.2.
var pageBoundaryKeys = []ast.PdfName{"MediaBox", "CropBox", "BleedBox", "TrimBox", "ArtBox"} //nolint:gochecknoglobals // constant set.

// checkPageBoundaries validates §6.1.13 t11: page boundary box dimensions must
// be within [3, 14400] units in both directions.
func checkPageBoundaries(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	part := PDFAPart(profile)
	// Page boundary size limits apply to PDF/A-2 and -3 only: PDF/A-4 builds
	// on PDF 2.0, which drops the ISO 32000-1 media-box implementation limit,
	// so ISO 19005-4 carries no equivalent rule. An unguarded zero Rule here
	// would also serialize as a clause-less "-t00" residual (21 cells).
	if part < 2 {
		return nil
	}
	rule := pageBoundRule(profile)
	if rule.Specification == "" {
		return nil
	}
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
			// Only validate if at least one page inherits this key.
			anyInherits := false
			for _, page := range pages {
				if _, has := page[key]; !has {
					anyInherits = true
					break
				}
			}
			if !anyInherits {
				continue // Every page has its own, so tree root value is irrelevant.
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
	if f, ok := document.Number(resolved); ok {
		return f, nil
	}
	return 0, fmt.Errorf("expected numeric, got %T", resolved)
}

// checkPDFA4TrailerInfo validates PDF/A-4 §6.1.3 t04/t05: /Info key
// restrictions in PDF/A-4.
func checkPDFA4TrailerInfo(ctx context.Context, doc *document.Document, catalog ast.PdfDict, _ string) []Violation {
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
	infoDict, _ := document.ResolveDict(ctx, doc, infoObj)
	if infoDict != nil {
		var badKeys []string
		for key := range infoDict {
			if key != "ModDate" {
				badKeys = append(badKeys, string(key))
			}
		}
		slices.Sort(badKeys) // deterministic message order
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
	part := PDFAPart(profile)
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
	return ruleByPart(PDFAPart(profile), Rule{}, Rule_19005_2_6_1_8_t01, Rule_19005_3_6_1_8_t01, Rule_19005_4_6_1_7_t01)
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
		res := resolveResources(ctx, doc, page)
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
			for _, srcName := range sortedNames(roleMap) {
				dstObj := roleMap[srcName]
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
