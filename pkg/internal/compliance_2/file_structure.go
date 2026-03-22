package compliance2

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
)

// --- File structure numeric limits (ISO 32000-1 / ISO 32000-2) ---

const (
	maxIntegerValue = 2_147_483_647
	minIntegerValue = -2_147_483_648
	maxNameBytes    = 127
	maxIndirectObjs = 8_388_607
	maxStringBytes1 = 65535
	maxStringBytes2 = 32767
	maxArrayCap1    = 8191
	maxDictCap1     = 4095
	maxRealAbs1     = 32767.0
	maxRealAbs2     = 3.403e38
	minRealNonZero2 = 1.175e-38
	maxDeviceN1     = 8
	maxDeviceN2     = 32
	maxQNesting     = 28
	maxCID          = 65535
	headerReadSize  = 64
	minBinaryBytes  = 4
	eofMarker       = "%%EOF"
	tailReadSize    = 4096
	minPageBound    = 3.0
	maxPageBound    = 14400.0
)

// checkFileStructureGroup validates PDF file structure rules (§6.1).
func checkFileStructureGroup(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	slog.Debug("compliance_2: file_structure: starting checks", "profile", profile)

	var results []Result

	results = append(results, checkFileHeader(doc, profile)...)
	results = append(results, checkTrailerID(doc, profile)...)
	results = append(results, checkEncryptionForbidden(doc, profile)...)
	results = append(results, checkPostEOFData(doc, profile)...)
	results = append(results, checkStreamExternalKeys(ctx, doc, profile)...)
	results = append(results, checkStreamLZW(ctx, doc, profile)...)
	results = append(results, checkEmbeddedFiles(ctx, doc, catalog, profile)...)
	results = append(results, checkFormatViolations(doc, profile)...)
	results = append(results, checkPageBoundaries(ctx, doc, catalog, profile)...)
	results = append(results, checkNumericLimits(ctx, doc, profile)...)
	results = append(results, checkXRefFormat(doc, profile)...)

	if part := pdfaPart(profile); part == 4 {
		results = append(results, checkPDFA4TrailerInfo(doc, catalog, profile)...)
	}

	slog.Debug("compliance_2: file_structure: checks complete",
		"profile", profile,
		"violations", len(results),
	)
	return results
}

// --- Rule selectors ---

func fileHeaderRule(profile string) Rule {
	switch pdfaPart(profile) {
	case 1:
		return Rule_19005_1_6_1_2_t01
	case 2:
		return Rule_19005_2_6_1_2_t01
	case 3:
		return Rule_19005_3_6_1_2_t01
	case 4:
		return Rule_19005_4_6_1_2_t01
	default:
		return Rule_19005_1_6_1_2_t01
	}
}

func fileBinaryCommentRule(profile string) Rule {
	switch pdfaPart(profile) {
	case 1:
		return Rule_19005_1_6_1_2_t02
	case 2:
		return Rule_19005_2_6_1_2_t02
	case 3:
		return Rule_19005_3_6_1_2_t02
	case 4:
		return Rule_19005_4_6_1_2_t02
	default:
		return Rule_19005_1_6_1_2_t02
	}
}

func trailerIDRule(profile string) Rule {
	switch pdfaPart(profile) {
	case 1:
		return Rule_19005_1_6_1_3_t01
	case 2:
		return Rule_19005_2_6_1_3_t01
	case 3:
		return Rule_19005_3_6_1_3_t01
	case 4:
		return Rule_19005_4_6_1_3_t01
	default:
		return Rule_19005_1_6_1_3_t01
	}
}

func encryptRule(profile string) Rule {
	switch pdfaPart(profile) {
	case 1:
		return Rule_19005_1_6_1_3_t02
	case 2:
		return Rule_19005_2_6_1_3_t02
	case 3:
		return Rule_19005_3_6_1_3_t02
	case 4:
		return Rule_19005_4_6_1_3_t02
	default:
		return Rule_19005_1_6_1_3_t02
	}
}

func postEOFRule(profile string) Rule {
	switch pdfaPart(profile) {
	case 1:
		return Rule_19005_1_6_1_3_t03
	case 2:
		return Rule_19005_2_6_1_3_t03
	case 3:
		return Rule_19005_3_6_1_3_t03
	case 4:
		return Rule_19005_4_6_1_3_t03
	default:
		return Rule_19005_1_6_1_3_t03
	}
}

// linearizedIDRule returns the linearized ID consistency rule.
// Currently the check only validates presence, not equality, so this selector
// is kept for reference.
var _ = linearizedIDRule //nolint:gochecknoglobals // keep rule selector reachable.

func linearizedIDRule(_ string) Rule {
	// Only PDF/A-1 has a dedicated linearized ID rule (6.1.3 t04).
	return Rule_19005_1_6_1_3_t04
}

func streamExternalKeysRule(profile string) Rule {
	switch pdfaPart(profile) {
	case 1:
		return Rule_19005_1_6_1_7_t03
	case 2:
		return Rule_19005_2_6_1_7_1_t03
	case 3:
		return Rule_19005_3_6_1_7_1_t03
	case 4:
		return Rule_19005_4_6_1_6_1_t02
	default:
		return Rule_19005_1_6_1_7_t03
	}
}

func lzwFilterRule(profile string) Rule {
	switch pdfaPart(profile) {
	case 1:
		return Rule_19005_1_6_1_10_t01
	case 2:
		return Rule_19005_2_6_1_7_2_t01
	case 3:
		return Rule_19005_3_6_1_7_2_t01
	case 4:
		return Rule_19005_4_6_1_6_2_t01
	default:
		return Rule_19005_1_6_1_10_t01
	}
}

func embeddedFilesEFRule(_ string) Rule {
	return Rule_19005_1_6_1_11_t01
}

func embeddedFilesNameRule(_ string) Rule {
	return Rule_19005_1_6_1_11_t02
}

func integerRangeRule(profile string) Rule {
	switch pdfaPart(profile) {
	case 1:
		return Rule_19005_1_6_1_12_t01
	case 2:
		return Rule_19005_2_6_1_13_t01
	case 3:
		return Rule_19005_3_6_1_13_t01
	default:
		return Rule_19005_1_6_1_12_t01
	}
}

func realRangeRule(profile string) Rule {
	switch pdfaPart(profile) {
	case 1:
		return Rule_19005_1_6_1_12_t02
	case 2:
		return Rule_19005_2_6_1_13_t02
	case 3:
		return Rule_19005_3_6_1_13_t02
	default:
		return Rule_19005_1_6_1_12_t02
	}
}

func stringLengthRule(profile string) Rule {
	switch pdfaPart(profile) {
	case 1:
		return Rule_19005_1_6_1_12_t03
	case 2:
		return Rule_19005_2_6_1_13_t03
	case 3:
		return Rule_19005_3_6_1_13_t03
	default:
		return Rule_19005_1_6_1_12_t03
	}
}

func nameLengthRule(profile string) Rule {
	switch pdfaPart(profile) {
	case 1:
		return Rule_19005_1_6_1_12_t04
	case 2:
		return Rule_19005_2_6_1_13_t04
	case 3:
		return Rule_19005_3_6_1_13_t04
	default:
		return Rule_19005_1_6_1_12_t04
	}
}

func indirectObjCountRule(profile string) Rule {
	switch pdfaPart(profile) {
	case 1:
		return Rule_19005_1_6_1_12_t07
	case 2:
		return Rule_19005_2_6_1_13_t07
	case 3:
		return Rule_19005_3_6_1_13_t07
	default:
		return Rule_19005_1_6_1_12_t07
	}
}

func objHeaderRule(profile string) Rule {
	switch pdfaPart(profile) {
	case 1:
		return Rule_19005_1_6_1_8_t01
	case 2:
		return Rule_19005_2_6_1_9_t01
	case 3:
		return Rule_19005_3_6_1_9_t01
	case 4:
		return Rule_19005_4_6_1_8_t01
	default:
		return Rule_19005_1_6_1_8_t01
	}
}

func pageBoundRule(profile string) Rule {
	switch pdfaPart(profile) {
	case 2:
		return Rule_19005_2_6_1_13_t11
	case 3:
		return Rule_19005_3_6_1_13_t11
	default:
		// PDF/A-1 uses the same clause numbering as part 2 for this check.
		return Rule_19005_2_6_1_13_t11
	}
}

func hexStringRule(profile string) Rule {
	switch pdfaPart(profile) {
	case 1:
		return Rule_19005_1_6_1_6_t01
	case 2:
		return Rule_19005_2_6_1_6_t01
	case 3:
		return Rule_19005_3_6_1_6_t01
	case 4:
		return Rule_19005_4_6_1_5_t01
	default:
		return Rule_19005_1_6_1_6_t01
	}
}

func xrefStreamForbiddenRule(_ string) Rule {
	return Rule_19005_1_6_1_4_t03
}

// --- Check implementations ---

// checkFileHeader validates §6.1.2: file must start with %PDF-x.y and have
// a binary comment marker.
func checkFileHeader(doc *document.Document, profile string) []Result {
	buf, err := safeReadHeader(doc, headerReadSize)
	if err != nil {
		slog.Debug("compliance_2: file_structure: cannot read header", "error", err)
		return nil
	}

	part := pdfaPart(profile)

	// Check that file starts with %PDF-.
	if !bytes.HasPrefix(buf, []byte("%PDF-")) {
		slog.Debug("compliance_2: file_structure: header does not start with %PDF-")
		return fail(fileHeaderRule(profile), "header does not start with %PDF-")
	}

	// Validate version pattern.
	// PDF/A-4 requires %PDF-2.n; PDF/A-1/2/3 require %PDF-1.n.
	if len(buf) < 8 { //nolint:mnd // minimum for "%PDF-x.y"
		slog.Debug("compliance_2: file_structure: header too short for version")
		return fail(fileHeaderRule(profile), "header too short")
	}
	version := string(buf[5:8]) // e.g., "1.4" or "2.0"

	var valid bool
	switch part {
	case 4: //nolint:mnd // PDF/A-4
		// Must be %PDF-2.n where n is 0-9.
		valid = buf[5] == '2' && buf[6] == '.' && buf[7] >= '0' && buf[7] <= '9'
	default:
		// Must be %PDF-1.n where n is 0-7.
		valid = buf[5] == '1' && buf[6] == '.' && buf[7] >= '0' && buf[7] <= '7'
	}

	if !valid {
		slog.Debug("compliance_2: file_structure: header version mismatch",
			"version", version, "part", part)
		return fail(fileHeaderRule(profile),
			fmt.Sprintf("header %q does not match required pattern", "%PDF-"+version))
	}
	slog.Debug("compliance_2: file_structure: header version OK", "version", version)

	// Check binary comment marker after the header line.
	// After %PDF-x.y, there should be an EOL then % followed by 4 bytes > 127.
	var results []Result
	eolIdx := bytes.IndexAny(buf[8:], "\r\n")
	if eolIdx < 0 {
		slog.Debug("compliance_2: file_structure: no EOL after header version")
		return failf(fileBinaryCommentRule(profile), "no EOL after header line")
	}

	// Skip past all EOL characters.
	commentStart := 8 + eolIdx
	for commentStart < len(buf) && (buf[commentStart] == '\r' || buf[commentStart] == '\n') {
		commentStart++
	}

	if commentStart >= len(buf) || buf[commentStart] != '%' {
		slog.Debug("compliance_2: file_structure: missing binary comment marker")
		return failf(fileBinaryCommentRule(profile), "missing %% binary comment after header")
	}

	// Check the 4 bytes after the %.
	binaryStart := commentStart + 1
	if binaryStart+minBinaryBytes > len(buf) {
		slog.Debug("compliance_2: file_structure: binary comment too short")
		return failf(fileBinaryCommentRule(profile), "binary comment has fewer than 4 bytes")
	}

	for i := range minBinaryBytes {
		b := buf[binaryStart+i]
		if b <= 127 { //nolint:mnd // PDF spec: each byte > 127
			slog.Debug("compliance_2: file_structure: binary comment byte not > 127",
				"index", i, "value", b)
			results = append(results, Result{
				Rule:    fileBinaryCommentRule(profile),
				Context: fmt.Sprintf("binary comment byte %d has value %d (must be > 127)", i, b),
			})
			return results
		}
	}

	slog.Debug("compliance_2: file_structure: binary comment OK")
	return nil
}

// checkTrailerID validates §6.1.3: trailer must contain /ID key.
func checkTrailerID(doc *document.Document, profile string) []Result {
	trailer := doc.Index.Trailer
	if trailer == nil {
		slog.Debug("compliance_2: file_structure: no trailer dictionary")
		return fail(trailerIDRule(profile), "no trailer dictionary")
	}

	idObj, hasID := trailer["ID"]
	if !hasID {
		slog.Debug("compliance_2: file_structure: trailer missing /ID")
		return fail(trailerIDRule(profile), "missing ID in trailer")
	}

	// For PDF/A-2+ the ID array must not be empty.
	if arr, ok := idObj.(ast.PdfArray); ok && len(arr) == 0 {
		slog.Debug("compliance_2: file_structure: trailer /ID array is empty")
		return fail(trailerIDRule(profile), "empty ID array in trailer")
	}

	slog.Debug("compliance_2: file_structure: trailer /ID present")

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
func checkLinearizedID(doc *document.Document, _ string) []Result {
	trailer := doc.Index.Trailer
	idObj, hasID := trailer["ID"]
	if !hasID {
		slog.Debug("compliance_2: file_structure: linearized PDF missing /ID in trailer")
		return nil
	}

	idArr, ok := idObj.(ast.PdfArray)
	if !ok || len(idArr) < 2 { //nolint:mnd // ID array needs 2 elements
		slog.Debug("compliance_2: file_structure: linearized ID array too short")
		return nil
	}

	slog.Debug("compliance_2: file_structure: linearized ID present and valid")
	return nil
}

// checkEncryptionForbidden validates §6.1.3: /Encrypt must not be in trailer.
func checkEncryptionForbidden(doc *document.Document, profile string) []Result {
	if doc.IsEncrypted {
		slog.Debug("compliance_2: file_structure: document is encrypted")
		return fail(encryptRule(profile), "document is encrypted")
	}

	trailer := doc.Index.Trailer
	if trailer != nil {
		if _, hasEncrypt := trailer["Encrypt"]; hasEncrypt {
			slog.Debug("compliance_2: file_structure: trailer contains /Encrypt")
			return fail(encryptRule(profile), "Encrypt key present in trailer")
		}
	}

	slog.Debug("compliance_2: file_structure: no encryption — pass")
	return nil
}

// checkPostEOFData validates §6.1.3: no data after the last %%EOF marker,
// except an optional single EOL.
func checkPostEOFData(doc *document.Document, profile string) []Result {
	fileSize := doc.FileSize()
	if fileSize <= 0 {
		slog.Debug("compliance_2: file_structure: unknown file size, skipping post-EOF check")
		return nil
	}

	// Read the tail of the file.
	readLen := min(int64(tailReadSize), fileSize)
	offset := fileSize - readLen
	tail, err := doc.ReadFileBytes(offset, readLen)
	if err != nil {
		slog.Debug("compliance_2: file_structure: cannot read file tail", "error", err)
		return nil
	}

	// Find the last occurrence of %%EOF.
	lastIdx := bytes.LastIndex(tail, []byte(eofMarker))
	if lastIdx < 0 {
		slog.Debug("compliance_2: file_structure: no %%EOF marker found in tail")
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
			slog.Debug("compliance_2: file_structure: data after %%EOF",
				"extra_bytes", extraBytes)
			return failf(postEOFRule(profile), "%d byte(s) after last %%%%EOF", extraBytes)
		}
	}

	slog.Debug("compliance_2: file_structure: no data after %%EOF — pass")
	return nil
}

// checkStreamExternalKeys validates §6.1.7: stream dicts must not contain
// /F, /FFilter, or /FDecodeParms.
func checkStreamExternalKeys(ctx context.Context, doc *document.Document, profile string) []Result {
	forbiddenKeys := []ast.PdfName{"F", "FFilter", "FDecodeParms"}
	var results []Result

	for objNum, entry := range doc.Index.Entries {
		if ctx.Err() != nil {
			slog.Debug("compliance_2: file_structure: context cancelled in stream external keys check")
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
			slog.Debug("compliance_2: file_structure: stream has forbidden external keys",
				"object", objNum, "keys", found)
			results = append(results, Result{
				Rule:    streamExternalKeysRule(profile),
				Context: fmt.Sprintf("object %d: stream dict contains %v", objNum, found),
			})
		}
	}

	if len(results) == 0 {
		slog.Debug("compliance_2: file_structure: no forbidden stream external keys found")
	}
	return results
}

// checkStreamLZW validates §6.1.10: LZWDecode filter is not permitted.
func checkStreamLZW(ctx context.Context, doc *document.Document, profile string) []Result {
	var results []Result

	for objNum, entry := range doc.Index.Entries {
		if ctx.Err() != nil {
			slog.Debug("compliance_2: file_structure: context cancelled in LZW check")
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
			slog.Debug("compliance_2: file_structure: LZW filter found",
				"object", objNum)
			results = append(results, Result{
				Rule:    lzwFilterRule(profile),
				Context: fmt.Sprintf("object %d uses LZWDecode filter", objNum),
			})
		}
	}

	if len(results) == 0 {
		slog.Debug("compliance_2: file_structure: no LZW filters found")
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

// checkEmbeddedFiles validates §6.1.11: embedded files are forbidden in PDF/A-1.
func checkEmbeddedFiles(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	if pdfaPart(profile) != 1 {
		slog.Debug("compliance_2: file_structure: embedded files check skipped (not PDF/A-1)")
		return nil
	}

	var results []Result

	// Check for /EmbeddedFiles in the Names dictionary.
	namesObj, ok := catalog["Names"]
	if ok {
		namesDict, err := document.ResolveDict(ctx, doc, namesObj)
		if err == nil && namesDict != nil {
			if _, hasEF := namesDict["EmbeddedFiles"]; hasEF {
				slog.Debug("compliance_2: file_structure: Names dict contains /EmbeddedFiles")
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
			slog.Debug("compliance_2: file_structure: file spec dict has /EF key",
				"object", objNum)
			results = append(results, Result{
				Rule:    embeddedFilesEFRule(profile),
				Context: fmt.Sprintf("object %d: file specification contains EF key", objNum),
			})
		}
	}

	if len(results) == 0 {
		slog.Debug("compliance_2: file_structure: no embedded files found — pass")
	}
	return results
}

// checkFormatViolations checks parser-level format violations collected during
// resolveAllObjects. These include hex string format issues (§6.1.6) and
// object header format issues (§6.1.8/§6.1.9) detected by the parser when
// ComplianceMode is enabled.
func checkFormatViolations(doc *document.Document, profile string) []Result {
	if len(doc.FormatViolations) == 0 {
		slog.Debug("compliance_2: file_structure: no parser format violations")
		return nil
	}

	var results []Result
	hexRule := hexStringRule(profile)
	objRule := objHeaderRule(profile)

	for _, violation := range doc.FormatViolations {
		msg := violation.Error()
		slog.Debug("compliance_2: file_structure: parser format violation",
			"error", msg)

		rule := hexRule
		if containsObjHeaderPattern(msg) {
			rule = objRule
		}

		results = append(results, Result{
			Rule:    rule,
			Context: msg,
		})
	}

	slog.Debug("compliance_2: file_structure: parser format violations reported",
		"count", len(results))
	return results
}

// containsObjHeaderPattern returns true if the error message relates to
// object header formatting (obj/endobj keywords, header line).
func containsObjHeaderPattern(msg string) bool {
	for _, pat := range []string{"obj", "endobj", "header"} {
		if bytes.Contains([]byte(msg), []byte(pat)) {
			return true
		}
	}
	return false
}

// checkNumericLimits validates §6.1.12/6.1.13: numeric value limits for
// names, strings, integers, reals, arrays, dicts, and indirect objects.
func checkNumericLimits(ctx context.Context, doc *document.Document, profile string) []Result {
	part := pdfaPart(profile)
	var results []Result

	// Check indirect object count.
	objCount := len(doc.Index.Entries)
	if objCount > maxIndirectObjs {
		slog.Debug("compliance_2: file_structure: too many indirect objects",
			"count", objCount)
		results = append(results, Result{
			Rule:    indirectObjCountRule(profile),
			Context: fmt.Sprintf("indirect object count %d exceeds %d", objCount, maxIndirectObjs),
		})
	} else {
		slog.Debug("compliance_2: file_structure: indirect object count OK",
			"count", objCount)
	}

	// Walk all objects checking names, strings, integers, reals, arrays, dicts.
	for objNum, entry := range doc.Index.Entries {
		if ctx.Err() != nil {
			slog.Debug("compliance_2: file_structure: context cancelled in numeric limits check")
			return results
		}
		if entry.Free {
			continue
		}

		obj, err := document.ResolveObject(ctx, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if err != nil {
			continue
		}

		results = append(results, checkObjectNumericLimits(obj, objNum, part, profile)...)
	}

	if len(results) == 0 {
		slog.Debug("compliance_2: file_structure: all numeric limits OK")
	}
	return results
}

// checkObjectNumericLimits checks a single resolved object for numeric limit
// violations. It handles nested dicts and arrays recursively.
func checkObjectNumericLimits(obj ast.PdfObject, objNum int, part int, profile string) []Result {
	var results []Result

	switch v := obj.(type) {
	case ast.PdfInt:
		val := int64(v)
		if val > maxIntegerValue || val < minIntegerValue {
			results = append(results, Result{
				Rule:    integerRangeRule(profile),
				Context: fmt.Sprintf("object %d: integer %d out of range", objNum, val),
			})
		}

	case ast.PdfReal:
		fval := float64(v)
		switch part {
		case 1:
			if math.Abs(fval) > maxRealAbs1 {
				results = append(results, Result{
					Rule:    realRangeRule(profile),
					Context: fmt.Sprintf("object %d: real %g exceeds +/-%g", objNum, fval, maxRealAbs1),
				})
			}
		case 2, 3: //nolint:mnd
			if math.Abs(fval) > maxRealAbs2 {
				results = append(results, Result{
					Rule:    realRangeRule(profile),
					Context: fmt.Sprintf("object %d: real %g exceeds +/-%g", objNum, fval, maxRealAbs2),
				})
			}
		}

	case ast.PdfString:
		maxStr := maxStringBytes1
		if part >= 2 { //nolint:mnd
			maxStr = maxStringBytes2
		}
		if len(v) > maxStr {
			results = append(results, Result{
				Rule:    stringLengthRule(profile),
				Context: fmt.Sprintf("object %d: string length %d exceeds %d", objNum, len(v), maxStr),
			})
		}

	case ast.PdfName:
		// PDF names may contain #XX hex escape sequences. The limit applies
		// to the decoded byte length, not the encoded representation.
		decodedLen := decodedNameLength(string(v))
		if decodedLen > maxNameBytes {
			results = append(results, Result{
				Rule:    nameLengthRule(profile),
				Context: fmt.Sprintf("object %d: name length %d exceeds %d", objNum, decodedLen, maxNameBytes),
			})
		}

	case ast.PdfArray:
		// PDF/A-1: array capacity limit.
		if part == 1 && len(v) > maxArrayCap1 {
			results = append(results, Result{
				Rule:    Rule_19005_1_6_1_12_t05,
				Context: fmt.Sprintf("object %d: array capacity %d exceeds %d", objNum, len(v), maxArrayCap1),
			})
		}
		for _, elem := range v {
			results = append(results, checkObjectNumericLimits(elem, objNum, part, profile)...)
		}

	case ast.PdfDict:
		// PDF/A-1: dictionary capacity limit.
		if part == 1 && len(v) > maxDictCap1 {
			results = append(results, Result{
				Rule:    Rule_19005_1_6_1_12_t06,
				Context: fmt.Sprintf("object %d: dict capacity %d exceeds %d", objNum, len(v), maxDictCap1),
			})
		}
		for key, val := range v {
			results = append(results, checkObjectNumericLimits(key, objNum, part, profile)...)
			results = append(results, checkObjectNumericLimits(val, objNum, part, profile)...)
		}

	case ast.PdfStream:
		// Check the stream dictionary.
		if v.Dict != nil {
			results = append(results, checkObjectNumericLimits(v.Dict, objNum, part, profile)...)
		}
	}

	return results
}

// decodedNameLength returns the byte length of a PDF name after decoding #XX
// hex escape sequences. Each #XX sequence represents a single byte in the
// decoded name.
func decodedNameLength(name string) int {
	decoded := 0
	i := 0
	for i < len(name) {
		if name[i] == '#' && i+2 < len(name) && isHexDigit(name[i+1]) && isHexDigit(name[i+2]) {
			decoded++
			i += 3 //nolint:mnd // #XX is 3 characters encoding 1 byte.
		} else {
			decoded++
			i++
		}
	}
	return decoded
}

// isHexDigit returns true if c is a valid hexadecimal digit.
func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// checkXRefFormat validates §6.1.4: xref streams are forbidden in PDF/A-1.
func checkXRefFormat(doc *document.Document, profile string) []Result {
	if pdfaPart(profile) != 1 {
		slog.Debug("compliance_2: file_structure: xref format check skipped (not PDF/A-1)")
		return nil
	}

	// Detect xref stream usage by checking if any entries use compressed type
	// (type 2), which is only possible with xref streams.
	for _, entry := range doc.Index.Entries {
		if entry.Type == xref.XRefTypeCompressed {
			slog.Debug("compliance_2: file_structure: xref streams detected (compressed objects found)")
			return fail(xrefStreamForbiddenRule(profile), "document uses xref streams")
		}
	}

	// Also check if the trailer itself is a stream dict (xref stream trailer).
	trailer := doc.Index.Trailer
	if trailer != nil {
		if typeName, ok := trailer["Type"].(ast.PdfName); ok && typeName == "XRef" {
			slog.Debug("compliance_2: file_structure: trailer is an xref stream")
			return fail(xrefStreamForbiddenRule(profile), "document uses xref streams")
		}
	}

	slog.Debug("compliance_2: file_structure: xref format OK (no xref streams)")
	return nil
}

// pageBoundaryKeys lists all page boundary box keys defined in ISO 32000-1, §14.11.2.
var pageBoundaryKeys = []ast.PdfName{"MediaBox", "CropBox", "BleedBox", "TrimBox", "ArtBox"} //nolint:gochecknoglobals // constant set.

// checkPageBoundaries validates §6.1.13 t11: page boundary box dimensions must
// be within [3, 14400] units in both directions.
func checkPageBoundaries(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	part := pdfaPart(profile)
	// Page boundary size limits apply to PDF/A-2, -3, and -4.
	if part < 2 { //nolint:mnd // PDF/A-1 does not have this rule.
		slog.Debug("compliance_2: file_structure: page boundary check skipped (not PDF/A-2+)")
		return nil
	}

	slog.Debug("compliance_2: file_structure: checking page boundaries", "profile", profile)

	rule := pageBoundRule(profile)
	pages := collectPages(ctx, doc, catalog)
	var results []Result

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
			if err != nil || len(arr) < 4 { //nolint:mnd // BBox requires 4 values.
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
				slog.Debug("compliance_2: file_structure: page boundary out of range",
					"page", pageIdx+1, "box", string(key),
					"width", width, "height", height,
				)
				results = append(results, Result{
					Rule:    rule,
					Context: fmt.Sprintf("page %d /%s: width=%.1f height=%.1f (valid: %.0f-%.0f)", pageIdx+1, key, width, height, minPageBound, maxPageBound),
				})
			}
		}
	}

	if len(results) == 0 {
		slog.Debug("compliance_2: file_structure: all page boundaries OK")
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
func checkPDFA4TrailerInfo(doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	trailer := doc.Index.Trailer
	if trailer == nil {
		return nil
	}

	infoObj, hasInfo := trailer["Info"]
	if !hasInfo {
		slog.Debug("compliance_2: file_structure: no /Info in trailer — pass (PDF/A-4)")
		return nil
	}

	// §6.1.3 t04: /Info is only allowed if /PieceInfo exists in catalog.
	_, hasPieceInfo := catalog["PieceInfo"]
	if !hasPieceInfo {
		slog.Debug("compliance_2: file_structure: /Info present without /PieceInfo in catalog")
		return fail(Rule_19005_4_6_1_3_t04, "Info key present in trailer but PieceInfo not in catalog")
	}

	// §6.1.3 t05: if Info dict is present, it may only contain /ModDate.
	if infoDict, ok := infoObj.(ast.PdfDict); ok {
		var badKeys []string
		for key := range infoDict {
			if key != "ModDate" {
				badKeys = append(badKeys, string(key))
			}
		}
		if len(badKeys) > 0 {
			slog.Debug("compliance_2: file_structure: Info dict has non-ModDate keys",
				"keys", badKeys)
			return failf(Rule_19005_4_6_1_3_t05,
				"Info dictionary contains keys %v (only ModDate allowed)", badKeys)
		}
	}

	slog.Debug("compliance_2: file_structure: PDF/A-4 trailer Info OK")
	return nil
}
