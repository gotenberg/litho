package compliance

import (
	"context"
	"crypto/md5" //nolint:gosec
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// checkColorGroup validates colour-related PDF/A requirements:
//   - §6.2.2/6.2.3: OutputIntent ICC profiles
//   - §6.2.3.3 / §6.2.4.3: device-dependent colour space restrictions
//   - §6.2.3.2 / §6.2.4.2: ICCBased profile restrictions
func checkColorGroup(ctx context.Context, vctx *validationCtx) []Violation {
	doc, catalog := vctx.doc, vctx.catalog
	part := vctx.part

	if part == 0 {
		return nil
	}

	slog.Debug("compliance.color: starting color checks", "profile", vctx.profile, "part", part)

	var results []Violation

	// --- §6.2.2 / §6.2.3: OutputIntent ICC profile validation ---
	iccColorSpace, hasICC := checkOutputIntentICC(ctx, doc, catalog, part, &results)

	// --- §6.2.3.3 / §6.2.4.3: device-dependent colour space restrictions ---
	checkDeviceColorSpaces(ctx, doc, catalog, part, hasICC, iccColorSpace, &results)

	// --- §6.2.3.2 / §6.2.4.2: ICCBased profile restrictions ---
	checkICCBasedProfiles(ctx, doc, catalog, part, &results)

	// --- §6.2.4.4: Separation and DeviceN colour space consistency ---
	// Run for all parts: part 1 needs DeviceN component limit (§6.1.12 t09),
	// parts 2+ need colorant consistency (§6.2.4.4 t01/t02).
	checkSeparationDeviceN(ctx, doc, catalog, part, &results)

	slog.Debug("compliance.color: color checks complete", "violations", len(results))
	return results
}

// --- Shared colour helpers ---

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

// validICCDeviceClasses are the allowed device classes for OutputIntent profiles.
var validICCDeviceClasses = map[string]bool{ //nolint:gochecknoglobals // constant set.
	"prtr": true,
	"mntr": true,
}

// validICCColorSpaces are the allowed colour spaces for OutputIntent profiles.
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

// iccProfilesEqual compares two decoded ICC profiles for equivalence.
// If either profile has an all-zero Profile ID (bytes 84–99, meaning "not
// computed"), the computed MD5 is substituted before a raw byte comparison.
// This matches VeraPDF's behaviour: a zeroed Profile ID is treated as a
// placeholder for the correct value, not as a distinct identity.
func iccProfilesEqual(a, b []byte) bool {
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
	h := md5.Sum(buf) //nolint:gosec
	result := make([]byte, len(data))
	copy(result, data)
	copy(result[profileIDStart:profileIDEnd], h[:])
	return result
}

// pdfObjectEqual compares two PDF objects for structural equality,
// resolving indirect references.
func pdfObjectEqual(ctx context.Context, doc *document.Document, a, b ast.PdfObject) bool {
	// Resolve indirect refs.
	a, _ = document.ResolveObject(ctx, doc, a)
	b, _ = document.ResolveObject(ctx, doc, b)

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
			if !pdfObjectEqual(ctx, doc, va[i], vb[i]) {
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
			if !exists || !pdfObjectEqual(ctx, doc, valA, valB) {
				return false
			}
		}
		return true
	case nil:
		return b == nil
	}
	return false
}

func isWhitespace(b byte) bool {
	return b == ' ' || b == '\n' || b == '\r' || b == '\t' || b == '\x00' || b == '\x0c'
}

// formatDictAddr returns a stable string key for cycle detection on a PdfDict.
func formatDictAddr(d ast.PdfDict) string {
	return fmt.Sprintf("%p", d)
}
