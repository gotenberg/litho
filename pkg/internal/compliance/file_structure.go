package compliance

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
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
	results = append(results, checkFormatViolations(vctx, profile)...)
	results = append(results, checkObjHeaderByteFormat(doc, profile)...)
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
	// ISO 19005-4, 6.1.3 t4 and t5 are stated once, by checkInfoDictPart4 in
	// document.go.

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

	var results []Violation
	if !valid {
		results = append(results, fail(fileHeaderRule(profile),
			fmt.Sprintf("header %q does not match required pattern", "%PDF-"+version))...)
		// The §6.1.2 t2 binary comment on the next line is an independent check
		// veraPDF evaluates even when the version VALUE is wrong for the profile
		// (e.g. %PDF-1.4 forced to PDF/A-4 fires both t1 and t2). Continue only
		// when the header is structurally %PDF-d.d, so byte 8 reliably locates
		// the EOL and the comment; a structurally malformed version line has no
		// locatable comment to check.
		structuralVersion := buf[5] >= '0' && buf[5] <= '9' && buf[6] == '.' && buf[7] >= '0' && buf[7] <= '9'
		if !structuralVersion {
			return results
		}
	}

	// After %PDF-x.y, byte 8 should be an EOL marker immediately. A trailing
	// character (e.g. a stray space) is a header-format violation for parts
	// 2-4, whose §6.1.2 t1 anchors the header line to exactly %PDF-x.n
	// (veraPDF `/^%PDF-1\.[0-7]$/`). ISO 19005-1 §6.1.2 is more lenient: its
	// predicate `/%PDF-\d\.\d/` is unanchored, so any %PDF-d.d at byte zero
	// conforms regardless of what follows on the line. Do not flag a trailing
	// byte at part 1, but still scan on to the EOL so the t2 binary comment on
	// the next line is evaluated (it is independent of the version line).
	eol := 8
	if buf[eol] != '\n' && buf[eol] != '\r' {
		if part >= 2 {
			results = append(results, fail(fileHeaderRule(profile),
				fmt.Sprintf("header version not immediately followed by EOL marker (byte 8 = 0x%02X)", buf[8]))...)
		}
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
//
// The presence rule and the linearized-consistency rule are separate tests over
// the same key, so a trailer with no /ID answers both: it fails presence, and
// its absent value cannot equal the /ID the last trailer carries.
func checkTrailerID(doc *document.Document, profile string) []Violation {
	trailer := doc.Index.Trailer
	if trailer == nil {
		return fail(trailerIDRule(profile), "no trailer dictionary")
	}

	var results []Violation
	idObj, hasID := trailer["ID"]
	switch {
	case !hasID:
		results = fail(trailerIDRule(profile), "missing ID in trailer")
	case PDFAPart(profile) != 1:
		// ISO 19005-1 §6.1.3 t1 asks only that the trailer carry an /ID
		// ("lastID != null"), so a part-1 file satisfies it whatever the entry
		// holds. ISO 19005-2/3 §6.1.3 t1 and ISO 19005-4 §6.1.3 t1 add
		// "lastID.length() > 0", which veraPDF measures over the entry as a
		// whole: measured on a five-cell matrix, [<AB><>] passes because its
		// first string carries content, while [<><>] and [] fail because the
		// entry carries none.
		if arr, ok := idObj.(ast.PdfArray); ok && fileIDContentLength(arr) == 0 {
			results = fail(trailerIDRule(profile), "ID array carries no content")
		}
	}

	// Check linearized ID consistency (§6.1.3 t04). PDF/A-1 only.
	if PDFAPart(profile) == 1 && doc.Linearization != nil {
		results = append(results, checkLinearizedID(doc)...)
	}

	return results
}

// fileIDContentLength returns the total number of bytes the string elements of
// a file-identifier array carry. Non-string elements contribute nothing, which
// is what veraPDF's lastID reports for them.
func fileIDContentLength(arr ast.PdfArray) int {
	n := 0
	for _, el := range arr {
		if str, ok := el.(ast.PdfString); ok {
			n += len(str)
		}
	}
	return n
}

// checkLinearizedID validates ISO 19005-1, 6.1.3 test 4: when a linearized file
// states /ID in both the first-page trailer and the last trailer, the two shall
// be identical. The test reads `(isLinearized != true) || lastID == null ||
// (firstPageID == lastID)`, so a last trailer with no /ID passes and a
// first-page trailer with no /ID fails: an absent value equals nothing.
//
// Both array elements count. A file whose first-page trailer differs from the
// last one in either element was measured to fail on veraPDF 1.30.2.
func checkLinearizedID(doc *document.Document) []Violation {
	last := doc.Index.LastTrailer
	if last == nil {
		return nil
	}
	lastID, hasLast := last["ID"]
	if !hasLast {
		return nil
	}
	if sameFileID(doc.Index.Trailer["ID"], lastID) {
		return nil
	}
	return fail(Rule_19005_1_6_1_3_t04,
		"linearized file: first-page trailer /ID differs from the last trailer /ID")
}

// sameFileID reports whether two trailer /ID values are the same file
// identifier: the same number of elements, each holding the same bytes. A
// missing value equals nothing, including another missing value, because the
// caller only reaches it once one side is known present.
func sameFileID(a, b ast.PdfObject) bool {
	aArr, aOK := a.(ast.PdfArray)
	bArr, bOK := b.(ast.PdfArray)
	if !aOK || !bOK || len(aArr) != len(bArr) {
		return false
	}
	for i := range aArr {
		aStr, ok := aArr[i].(ast.PdfString)
		if !ok {
			return false
		}
		bStr, ok := bArr[i].(ast.PdfString)
		if !ok || aStr != bStr {
			return false
		}
	}
	return true
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
	//
	// Discovery goes through [collectFileSpecs] rather than a cross-reference
	// sweep. The xref map holds only indirect objects, and ISO 32000-1 §7.11.3
	// does not require a file specification to be one, so a sweep never sees a
	// filespec written inline. That let a producer evade the clause outright:
	// PDF/A-1 forbids embedded files, yet moving the same /EF-carrying filespec
	// from an indirect object into an annotation's /FS turned a violation into
	// a silent pass. Parts 2, 3 and 4 already discover both forms this way.
	for _, fs := range collectFileSpecs(ctx, doc, catalog) {
		if ctx.Err() != nil {
			return results
		}
		if !fs.hasEF {
			continue
		}
		results = append(results, Violation{
			Rule:    embeddedFilesEFRule(profile),
			Context: fmt.Sprintf("%s: file specification contains EF key", fs.label()),
		})
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
		// A hybrid-reference file keeps a classic table for readers that
		// predate xref streams and an xref STREAM for the rest, linked from the
		// trailer's /XRefStm (ISO 32000-1, 7.5.8.4). Neither test above sees it:
		// the classic table supplies every entry, so none is compressed, and the
		// trailer is its own dictionary rather than the stream's. The file uses
		// an xref stream all the same, which is what this clause forbids, and
		// veraPDF answers containsXRefStream true for govdocs
		// thread_400_400545.pdf, a hybrid file litho passed.
		if _, hybrid := trailer["XRefStm"]; hybrid {
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

		declaredLen, judged := declaredStreamLength(ctx, doc, lengthObj)
		if !judged {
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

		// Phase 2: byte-exact §6.1.7.1 t1. The real length is the number of
		// bytes from the stream data start up to (but not including) the EOL
		// marker preceding endstream (ISO 19005-2 §6.1.7.1 t1). Read a window
		// spanning a few bytes before the declared end through where endstream
		// should sit, locate endstream, back up over exactly one EOL marker, and
		// require the length to equal the declared /Length. veraPDF is strict
		// here: a /Length off by even the trailing EOL byte fails.
		absDataStart := entry.Offset + int64(streamDataStart)
		absExpectedEnd := absDataStart + int64(declaredLen)

		const preWindow = 4
		const tailBufSize = 32
		winStart := max(absExpectedEnd-preWindow, absDataStart)
		win, err := doc.ReadFileBytes(ctx, winStart, preWindow+tailBufSize)
		if err != nil {
			continue
		}
		expOff := int(absExpectedEnd - winStart)

		esRel := bytes.Index(win, []byte("endstream"))
		if esRel < 0 || !streamLengthMatches(win, expOff, esRel) {
			// endstream is not one valid EOL past the declared end (or not within
			// the window at all): the /Length does not match the real length.
			results = append(results, Violation{
				Rule:    rule,
				Context: fmt.Sprintf("object %d: declared /Length=%d does not match the stream data extent", objNum, declaredLen),
			})
		}

		// §6.1.7 t2 / §6.1.7.1 t2: the endstream keyword shall be immediately
		// preceded by an EOL marker (veraPDF's endstreamKeywordEOLCompliant). It
		// is non-compliant when the byte before endstream is not CR or LF, be it
		// stream data abutting endstream, or a space or tab left after the EOL.
		if eolRule := streamEOLRule(profile); eolRule.Specification != "" && esRel >= 1 && win[esRel-1] != '\n' && win[esRel-1] != '\r' {
			results = append(results, Violation{
				Rule:    eolRule,
				Context: fmt.Sprintf("object %d: endstream keyword not immediately preceded by an EOL marker", objNum),
			})
		}
	}

	return results
}

// declaredStreamLength reads a stream's /Length the way ISO 19005-2, 6.1.7.1 t1
// judges it, and reports whether the stream can be judged at all.
//
// ISO 32000-1, Table 5 makes /Length an integer, and 7.3.10 treats a reference
// to an undefined object as a reference to the null object. veraPDF reads any
// /Length that is not a number as null, which matches no real extent: measured
// on a name, a string, a boolean, an array, a dictionary, a stream, null, a free
// entry and an undefined object. pdfjs issue19510.pdf gives its XRef stream
// /Length 9 0 R, and object 9 is the page's content stream; veraPDF reports it,
// and litho, which skipped any /Length it could not read as an integer, did not.
// Such a /Length reads as 0 here, as a null one already did. A real stays
// unjudged, as before: veraPDF passes one equal to the extent, and how it reads
// a fractional one was not measured.
func declaredStreamLength(ctx context.Context, doc *document.Document, lengthObj ast.PdfObject) (ast.PdfInt, bool) {
	resolved, err := document.ResolveObject(ctx, doc, lengthObj)
	if err != nil {
		if errors.Is(err, document.ErrObjectNotFound) {
			return 0, true
		}
		doc.Logger.Debug("compliance.file_structure: cannot resolve stream length", "error", err)
		return 0, false
	}
	switch v := resolved.(type) {
	case ast.PdfInt:
		return v, true
	case ast.PdfReal:
		return 0, false
	default:
		return 0, true
	}
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

// streamLengthMatches reports whether the endstream keyword (at offset esRel in
// win) sits exactly one EOL marker past the declared stream end (at offset
// expOff), which is how ISO 19005-2 §6.1.7.1 t1 accepts a stream. veraPDF is
// lenient about the CR of a CRLF: the declared length may be followed by LF or
// by CRLF before endstream (the CR of a CRLF may be counted as stream data), so
// both a one-byte and a two-byte gap pass. An endstream sitting exactly at the
// declared end fails when the last declared byte is itself an EOL that belongs
// before endstream (the length counted the EOL); it passes when the last byte
// is real data (no EOL present, a §6.1.7.1 t2 concern reported separately).
func streamLengthMatches(win []byte, expOff, esRel int) bool {
	switch esRel - expOff {
	case 0:
		return esRel < 1 || (win[esRel-1] != '\n' && win[esRel-1] != '\r')
	case 1:
		return expOff < len(win) && (win[expOff] == '\n' || win[expOff] == '\r')
	case 2:
		return expOff+1 < len(win) && win[expOff] == '\r' && win[expOff+1] == '\n'
	}
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
					Context: fmt.Sprintf("Pages tree /%s: width=%.1f height=%.1f (valid: %.0f-%.0f)", string(key), width, height, minPageBound, maxPageBound),
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
					Context: fmt.Sprintf("page %d /%s: width=%.1f height=%.1f (valid: %.0f-%.0f)", pageIdx+1, string(key), width, height, minPageBound, maxPageBound),
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

// checkSignatureReferences validates §6.1.12 t02 (PDF/A-2+): if /Perms/DocMDP
// is present, the Signature References dictionary shall not contain
// DigestLocation, DigestMethod, or DigestValue keys.
func checkSignatureReferences(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	rule := sigRefRule(PDFAPart(profile))
	if rule.Specification == "" {
		return nil
	}
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
	forbidden := []ast.PdfName{"DigestLocation", "DigestMethod", "DigestValue"}

	for i, refObj := range refArr {
		refDict, err := document.ResolveDict(ctx, doc, refObj)
		if err != nil || refDict == nil {
			continue
		}
		for _, key := range forbidden {
			if _, has := refDict[key]; has {
				results = append(results, failf(rule,
					"Signature Reference[%d] contains forbidden key %s", i, key)...)
			}
		}
	}
	return results
}

// sigRefRule returns the rule forbidding a digest key on a signature reference
// under /Perms /DocMDP. Only ISO 19005-2/3, 6.1.12 t2 states it: ISO 19005-4
// has no PDSigRef rule at all, and its 6.1.12 t1 is the unrelated PDDocument
// requirement that the catalog /Version read 2.n. Reporting a reference defect
// there answered a signature question with the catalog's rule.
func sigRefRule(part int) Rule {
	return ruleByPart(part, Rule{}, Rule_19005_2_6_1_12_t02, Rule_19005_3_6_1_12_t02, Rule{})
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
		res := resolveInheritedResources(ctx, doc, page)
		if res == nil {
			continue
		}

		// Font names: check /BaseFont on each font.
		fontDict, err := document.ResolveDict(ctx, doc, res["Font"])
		if err == nil && fontDict != nil {
			// Sorted, like the RoleMap walk below: a page naming several fonts
			// with invalid names reports one violation each, and ranging the
			// resource dictionary ordered them randomly between runs.
			for _, fontName := range sortedNames(fontDict) {
				fontObj := fontDict[fontName]
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
			for _, csName := range sortedNames(csDict) {
				csObj := csDict[csName]
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
