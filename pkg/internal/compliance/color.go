package compliance

import (
	"context"
	"crypto/md5" //nolint:gosec // G501: MD5 computes the ICC profile ID per ICC.1, not for security.
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// checkColorGroup validates colour-related PDF/A requirements:
//   - §6.2.2/6.2.3: OutputIntent ICC profiles, in the catalog and (PDF/A-4
//     only) on pages
//   - §6.2.3.3 / §6.2.4.3: device-dependent colour space restrictions
//   - §6.2.3.2 / §6.2.4.2: ICCBased profile restrictions
func checkColorGroup(ctx context.Context, vctx *validationCtx) []Violation {
	doc, catalog := vctx.doc, vctx.catalog
	part := vctx.part

	if part == 0 {
		return nil
	}

	doc.Logger.Debug("compliance.color: starting color checks", "profile", vctx.profile, "part", part)

	var results []Violation

	iccColorSpace, hasICC := checkOutputIntentICC(ctx, doc, catalog, part, &results)
	checkPageOutputIntents(ctx, doc, catalog, part, &results)
	checkDeviceColorSpaces(ctx, doc, vctx.pages, part, hasICC, iccColorSpace, &results)
	checkICCBasedProfiles(ctx, doc, catalog, vctx.pages, part, &results)

	// Run for all parts: part 1 needs DeviceN component limit (§6.1.12 t09),
	// parts 2+ need colorant consistency (§6.2.4.4 t01/t02).
	checkSeparationDeviceN(ctx, doc, vctx.pages, part, &results)

	doc.Logger.Debug("compliance.color: color checks complete", "violations", len(results))
	return results
}

// iccProfileHeader holds parsed fields from the 128-byte ICC profile header.
type iccProfileHeader struct {
	deviceClass string // bytes 12-16
	colorSpace  string // bytes 16-20
	version     int    // major version from byte 8
}

// iccHeaderSize is the minimum number of bytes needed to parse the ICC header.
const iccHeaderSize = 20

// parseICCHeader extracts the device class, colour space, and version from raw
// ICC profile data.
func parseICCHeader(data []byte) (iccProfileHeader, bool) {
	if len(data) < iccHeaderSize {
		return iccProfileHeader{}, false
	}
	hdr := iccProfileHeader{
		deviceClass: string(data[12:16]),
		colorSpace:  string(data[16:20]),
		version:     int(data[8]), // Major version byte.
	}
	return hdr, true
}

// iccVersionExceedsCeiling reports whether an ICC profile major version
// exceeds the ceiling a PDF/A part inherits from its base PDF version. PDF/A-1
// is based on PDF 1.4, which predates ICC v4, so a v4-or-higher profile fails
// ISO 19005-1 §6.2.3.2 (veraPDF: ICCInputProfile version < 3.0; no ICC v3
// exists, so this rejects v4). PDF/A-2/3 (PDF 1.7) and PDF/A-4 (PDF 2.0) admit
// ICC v4 and reject only v5+ (veraPDF: version < 5.0). version is the ICC
// header major-version byte (see [parseICCHeader]).
func iccVersionExceedsCeiling(version, part int) bool {
	if part == 1 {
		return version >= 3
	}
	return version >= 5
}

// validICCDeviceClasses are the allowed device classes for OutputIntent
// profiles. Per ISO 19005-2/3 §6.2.2 the destination profile must be a
// device class from ICC.1 (printer "prtr" or monitor "mntr").
var validICCDeviceClasses = map[string]bool{ //nolint:gochecknoglobals // constant set.
	"prtr": true,
	"mntr": true,
}

// validICCInputDeviceClasses and validICCInputColorSpaces are the device
// classes and colour spaces an ICCBased colour space's profile may declare:
// input (scnr), display (mntr), output (prtr) or colour space conversion (spac)
// profiles over RGB, CMYK, GRAY or Lab data (ISO 19005-1, 6.2.3.2; ISO 19005-2,
// 6.2.4.2). They mirror veraPDF's ICCInputProfile test in every part, measured
// on abstract, device link, named colour and XYZ profiles.
var validICCInputDeviceClasses = map[string]bool{ //nolint:gochecknoglobals // constant set.
	"prtr": true,
	"mntr": true,
	"scnr": true,
	"spac": true,
}

var validICCInputColorSpaces = map[string]bool{ //nolint:gochecknoglobals // constant set.
	"GRAY": true,
	"RGB ": true, //nolint:gocritic // ICC colour space signatures are exactly 4 bytes; trailing space is correct.
	"CMYK": true,
	"Lab ": true, //nolint:gocritic // ICC colour space signatures are exactly 4 bytes; trailing space is correct.
}

// iccInputHeaderProblem describes why an ICCBased colour space's profile
// header fails ISO 19005-1, 6.2.3.2 t1 / ISO 19005-2, 6.2.4.2 t1, or returns
// the empty string when it passes.
func iccInputHeaderProblem(hdr iccProfileHeader, part int) string {
	switch {
	case iccVersionExceedsCeiling(hdr.version, part):
		return fmt.Sprintf("ICC profile version %d exceeds the PDF/A part ceiling", hdr.version)
	case !validICCInputDeviceClasses[hdr.deviceClass]:
		return fmt.Sprintf("ICC profile device class %q is not prtr, mntr, scnr or spac", hdr.deviceClass)
	case !validICCInputColorSpaces[hdr.colorSpace]:
		return fmt.Sprintf("ICC profile colour space %q is not RGB, CMYK, GRAY or Lab", hdr.colorSpace)
	}
	return ""
}

// validICCColorSpaces are the allowed colour spaces for OutputIntent
// profiles: the ICC.1 data colour space signatures permitted by
// ISO 19005-2/3 §6.2.2.
// Note: ICC colour space signatures are exactly 4 bytes; "RGB " has a trailing space.
// Note: ICC colour space signatures are exactly 4 bytes; "RGB " has a trailing space.
var validICCColorSpaces = map[string]bool{ //nolint:gochecknoglobals // constant set.
	"GRAY": true,
	"RGB ": true, //nolint:gocritic // ICC colour space signatures are exactly 4 bytes; trailing space is correct.
	"CMYK": true,
}

// resolveCSName resolves a colour space object to its base name string.
func resolveCSName(ctx context.Context, doc *document.Document, obj ast.PdfObject) string {
	// Direct name.
	if name, err := document.ResolveName(ctx, doc, obj); err == nil && name != "" {
		return string(name)
	}

	// Array form: [/name ...].
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err == nil && len(arr) > 0 {
		if name, err := document.ResolveName(ctx, doc, arr[0]); err == nil {
			return string(name)
		}
	}

	return ""
}

// markDeviceCS sets the appropriate flag in usage if csName is a device colour space.
func markDeviceCS(csName string, usage *deviceCSUsage) {
	switch csName {
	case "DeviceRGB":
		usage.rgb = true
	case "DeviceCMYK":
		usage.cmyk = true
	case "DeviceGray":
		usage.gray = true
	}
}

// sameIndirectRef returns true if both objects are indirect references to the
// same object number.
func sameIndirectRef(a, b ast.PdfObject) bool {
	refA, okA := a.(ast.PdfIndirectRef)
	refB, okB := b.(ast.PdfIndirectRef)
	if okA && okB {
		return refA.ObjectNum == refB.ObjectNum && refA.GenNum == refB.GenNum
	}
	return false
}

// bytesEqual compares two byte slices for equality.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ICCProfilesEqual compares two decoded ICC profiles for equivalence.
// If either profile has an all-zero Profile ID (bytes 84–99, meaning "not
// computed"), the computed MD5 is substituted before a raw byte comparison.
// Per ICC.1, an all-zero Profile ID is a placeholder signalling the ID was
// never computed, not a distinct value, so two profiles that differ only by a
// missing versus computed ID are the same profile.
//
// Both arguments are profile data, not stream bytes. Remediation shares this
// comparison so a profile the detector calls a duplicate is one the fixer
// replaces (ISO 19005-4, 6.2.4.2 test 3).
func ICCProfilesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	const profileIDEnd = 100
	if len(a) < profileIDEnd {
		return bytesEqual(a, b)
	}
	// Normalise: if either Profile ID is all-zero, compute and fill it.
	na := normaliseICCProfileID(a)
	nb := normaliseICCProfileID(b)
	return bytesEqual(na, nb)
}

// normaliseICCProfileID returns a copy of the profile with its Profile ID
// (bytes 84–99) replaced by the computed MD5 if it is currently all zeros.
// Per the ICC spec, the MD5 is computed over the profile data with bytes
// 44–47, 64–67, and 84–99 zeroed.
func normaliseICCProfileID(data []byte) []byte {
	const profileIDStart = 84
	const profileIDEnd = 100
	// ICCProfilesEqual guarantees len(data) >= profileIDEnd; guard anyway so
	// the function is panic-safe on arbitrary input (CONTRIBUTING fuzz contract).
	if len(data) < profileIDEnd {
		return data
	}
	allZero := true
	for _, b := range data[profileIDStart:profileIDEnd] {
		if b != 0 {
			allZero = false
			break
		}
	}
	if !allZero {
		return data
	}
	// Compute the ICC Profile ID (MD5) per ICC spec.
	buf := make([]byte, len(data))
	copy(buf, data)
	// Zero rendering intent (44–47), date/time flags (64–67), profile ID (84–99).
	for i := 44; i < 48; i++ {
		buf[i] = 0
	}
	for i := 64; i < 68; i++ {
		buf[i] = 0
	}
	for i := profileIDStart; i < profileIDEnd; i++ {
		buf[i] = 0
	}
	h := md5.Sum(buf) //nolint:gosec // G401: MD5 computes the ICC profile ID per ICC.1 §7.2.18, not for security.
	result := make([]byte, len(data))
	copy(result, data)
	copy(result[profileIDStart:profileIDEnd], h[:])
	return result
}

// pdfObjectEqual compares two PDF objects for structural equality,
// resolving indirect references.
func pdfObjectEqual(ctx context.Context, doc *document.Document, a, b ast.PdfObject) bool {
	return pdfObjectEqualAt(ctx, doc, a, b, make(map[[2]int]bool))
}

// pdfObjectEqualAt is pdfObjectEqual carrying the object pairs already under
// comparison. Both operands are resolved and then walked element-wise, and
// ResolveObject's cycle guard is per-resolution, so an array naming itself
// compared with itself recursed until the goroutine stack was exhausted. That
// is a fatal runtime error no recover() converts, on the ValidatePDFA,
// ValidatePDFUA and ValidateWTPDF paths.
//
// The key is the PAIR, not one operand. Keyed on a single object number, a
// walk that had compared object 6 under one dictionary key would skip it under
// the next and report two DIFFERING dictionaries equal: a false compliance
// verdict rather than a hang.
//
// A pair seen again answers EQUAL. Two objects already under comparison are
// equal unless something else proves otherwise, so a cyclic but identical pair
// compares equal instead of unequal. Marks are never cleared, which is safe
// here though not in every walk (see findAndSetField, which searches on a
// path-dependent name): a false result short-circuits the caller before any
// sibling branch can observe the mark.
func pdfObjectEqualAt(ctx context.Context, doc *document.Document, a, b ast.PdfObject, seen map[[2]int]bool) bool {
	// Mark before resolving: the references are gone once a and b are
	// reassigned below. Only an indirect pair can close a cycle, since a direct
	// value cannot name itself.
	if aRef, ok := a.(ast.PdfIndirectRef); ok {
		if bRef, ok := b.(ast.PdfIndirectRef); ok {
			key := [2]int{aRef.ObjectNum, bRef.ObjectNum}
			if seen[key] {
				return true
			}
			seen[key] = true
		}
	}

	// Resolve indirect refs.
	a, _ = document.ResolveObject(ctx, doc, a) // Best-effort dereference for comparison; unresolvable objects compare as-is.
	b, _ = document.ResolveObject(ctx, doc, b) // Best-effort dereference for comparison; unresolvable objects compare as-is.

	switch va := a.(type) {
	case ast.PdfName:
		vb, ok := b.(ast.PdfName)
		return ok && va == vb
	case ast.PdfInt:
		vb, ok := b.(ast.PdfInt)
		return ok && va == vb
	case ast.PdfReal:
		vb, ok := b.(ast.PdfReal)
		return ok && va == vb
	case ast.PdfString:
		vb, ok := b.(ast.PdfString)
		return ok && va == vb
	case ast.PdfBool:
		vb, ok := b.(ast.PdfBool)
		return ok && va == vb
	case ast.PdfArray:
		vb, ok := b.(ast.PdfArray)
		if !ok || len(va) != len(vb) {
			return false
		}
		for i := range va {
			if !pdfObjectEqualAt(ctx, doc, va[i], vb[i], seen) {
				return false
			}
		}
		return true
	case ast.PdfDict:
		vb, ok := b.(ast.PdfDict)
		if !ok || len(va) != len(vb) {
			return false
		}
		for k, valA := range va {
			valB, exists := vb[k]
			if !exists || !pdfObjectEqualAt(ctx, doc, valA, valB, seen) {
				return false
			}
		}
		return true
	case nil:
		return b == nil
	}
	return false
}

// formatDictAddr returns a stable string key for cycle detection on a PdfDict.
func formatDictAddr(d ast.PdfDict) string {
	return fmt.Sprintf("%p", d)
}
