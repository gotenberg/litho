package writer

import (
	"maps"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/intconv"
)

// minXRefStreamVersion is the earliest PDF version that defines object streams
// and cross-reference streams (ISO 32000-1, 7.5.7 and 7.5.8). It is not a
// tuning knob: a file using either construct while declaring an earlier
// version misdescribes itself, and a reader honoring that declaration cannot
// parse the cross-reference at all.
const minXRefStreamVersion = "1.5"

// minAESV2Version is the earliest PDF version defining the AESV2 crypt filter
// that 128-bit AES encryption selects (ISO 32000-1, 7.6.5 and Table 25).
// Keyed on the filter method rather than /V, which is less precise: /V 4 means
// crypt filters generally, a PDF 1.5 feature, while AESV2 specifically arrived
// in 1.6.
const minAESV2Version = "1.6"

// minAESV3Version is the base version litho declares for the AESV3 crypt
// filter that 256-bit AES selects.
//
// ISO 32000-2 defines /V 5 /R 6 outright, so 2.0 would also be defensible, but
// it overstates the file: only the /Encrypt dictionary is 2.0, and the body
// remains whatever the source was. Adobe introduced AES-256 as an extension to
// PDF 1.7, and that is what the ecosystem writes: qpdf 12.4.1 emits %PDF-1.7
// with an /ADBE extension dictionary rather than %PDF-2.0. litho matches it,
// which means the base version is only half the declaration; see
// [ensureADBEExtension] for the other half.
const minAESV3Version = "1.7"

// isoPDF2Version is the first PDF version that defines the AESV3 crypt filter
// natively, so a file already declaring it needs no Adobe extension to explain
// /V 5. Below it the extension is what makes the handler legible; at or above
// it the extension is noise, and a /BaseVersion of 1.7 would contradict what
// the file declares. qpdf draws the same line: it writes /ADBE when encrypting
// a 1.4 source and omits it entirely on a 2.0 one.
const isoPDF2Version = "2.0"

// aesV3ExtensionLevel is the Adobe extension level that introduced the AESV3
// crypt filter with /R 6. qpdf writes a later blanket level (8) covering
// everything it can emit; 3 is the narrower claim, naming only the extension
// this file actually relies on.
const aesV3ExtensionLevel = 3

// MinAssociatedFilesVersion is the earliest PDF version litho declares for the
// associated-file keys /AF and /AFRelationship.
//
// ISO 32000-2, 14.13 standardises them, but ISO 19005-3 introduced them as an
// extension to PDF 1.7, and that is the reading litho already applies
// elsewhere: remediation pins a PDF/A-3 target to 1.7 while writing catalog
// /AF. Declaring 2.0 for an attachment would contradict the version litho's
// own remediation assigns to the same construct.
const MinAssociatedFilesVersion = "1.7"

// AtLeastVersion returns whichever of v and floor names the later PDF version.
//
// Exported for callers outside this package that write a construct carrying a
// version requirement and must declare it. An incremental update cannot
// rewrite the header, so such a caller records the result in the catalog
// /Version instead (ISO 32000-1, 7.5.2); see [MinAssociatedFilesVersion].
func AtLeastVersion(v, floor string) string {
	return atLeastVersion(v, floor)
}

// IsVersion reports whether v is a "major.minor" PDF version number, the form
// ISO 32000-1, 7.5.2 gives the header version and Table 28 the catalog
// /Version. A caller raising a version it read from a file checks it first:
// [AtLeastVersion] answers its floor for a value that is not a version, and
// readers ignore such a value rather than read it as an earlier version.
func IsVersion(v string) bool {
	_, _, ok := splitVersion(v)
	return ok
}

// maxVersionComponent bounds one parsed version component. It is an overflow
// guard, not a spec limit: ISO 32000-1, 7.5.2 writes the header version as a
// single digit either side of the period, so a component beyond this comes
// from a damaged file rather than a later PDF.
const maxVersionComponent = 1 << 20

// atLeastVersion returns whichever of v and floor names the later PDF version.
//
// Components compare numerically, not lexically: "1.10" is later than "1.5"
// and a string comparison reverses that. A v that is not a version number
// yields floor, since the caller is about to write a construct floor
// describes, and a value litho cannot read is no reason to understate the
// file.
func atLeastVersion(v, floor string) string {
	fMajor, fMinor, okFloor := splitVersion(floor)
	if !okFloor {
		return v
	}
	vMajor, vMinor, okV := splitVersion(v)
	if !okV {
		return floor
	}
	if vMajor > fMajor || (vMajor == fMajor && vMinor >= fMinor) {
		return v
	}
	return floor
}

// splitVersion parses a "major.minor" PDF version. It accepts exactly what
// document.isPDFVersion accepts, so a value litho reports as a document's
// version is one this compares rather than discards.
func splitVersion(v string) (major, minor int, ok bool) {
	majorStr, minorStr, found := strings.Cut(v, ".")
	if !found {
		return 0, 0, false
	}
	major, ok = versionComponent(majorStr)
	if !ok {
		return 0, 0, false
	}
	minor, ok = versionComponent(minorStr)
	if !ok {
		return 0, 0, false
	}
	return major, minor, true
}

// versionComponent parses one side of a version number: at least one digit,
// digits only, bounded by maxVersionComponent.
func versionComponent(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
		if n > maxVersionComponent {
			return 0, false
		}
	}
	return n, true
}

// raiseCatalogVersion raises the catalog's /Version entry to floor when it
// names an earlier version, returning a function that puts the original back.
//
// Per ISO 32000-1, 7.5.2 the catalog entry overrides the file header, and
// litho's reader applies it in both directions: document.initCatalogVersion
// follows pdf.js in letting a catalog /1.4 win over a 1.7 header. Raising the
// header alone would leave such a file still reporting 1.4 while carrying PDF
// 1.5 constructs.
//
// This runs BEFORE packing. Packing serializes an object's body into the
// compressed stream and drops it from the pending set, so a catalog that has
// been packed can no longer be edited. Whether the output truly needs the
// raise is known only after packing returns, which is what the undo is for.
//
// A catalog carrying no /Version is left alone: the header already answers the
// question, and adding the key would write an entry the source never had. One
// whose /Version is not a version number is also left alone, since readers
// ignore it (document.initCatalogVersion does) and it therefore misdescribes
// nothing.
func raiseCatalogVersion(alloc *ObjAllocator, trailer ast.PdfDict, floor string) func() {
	noop := func() {}
	if alloc == nil || trailer == nil {
		return noop
	}
	root, ok := trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return noop
	}
	catalog, ok := alloc.Pending()[root.ObjectNum].(ast.PdfDict)
	if !ok {
		return noop
	}
	current, ok := catalog["Version"].(ast.PdfName)
	if !ok {
		return noop
	}
	if _, _, valid := splitVersion(string(current)); !valid {
		return noop
	}
	raised := atLeastVersion(string(current), floor)
	if raised == string(current) {
		return noop
	}

	// Replaced rather than mutated: [ObjAllocator.Snapshot] captures pending
	// entries by reference, so editing this dictionary in place would also
	// alter any snapshot taken before now.
	next := make(ast.PdfDict, len(catalog))
	maps.Copy(next, catalog)
	next["Version"] = ast.PdfName(raised)
	if !alloc.AllocAt(root.ObjectNum, next) {
		return noop
	}
	return func() { alloc.AllocAt(root.ObjectNum, catalog) }
}

// encryptionVersionFloor returns the earliest PDF version that defines the
// encryption the trailer selects, or "" when it needs nothing later than the
// document already declares.
//
// The crypt filter method is what carries the requirement, not /V: /V 4 means
// crypt filters, a PDF 1.5 feature, while the AESV2 method specifically
// arrived in 1.6 (ISO 32000-1, Table 25) and AESV3 in ISO 32000-2. The RC4
// algorithms select /V 1 or /V 2 and write no /CF dictionary at all, so they
// answer "" here and keep the source's version, which is correct: /V 2 is a
// PDF 1.4 construct.
func encryptionVersionFloor(alloc *ObjAllocator, trailer ast.PdfDict) string {
	if alloc == nil || trailer == nil {
		return ""
	}
	encrypt, ok := resolvePending(alloc, trailer["Encrypt"])
	if !ok {
		return ""
	}
	cf, ok := resolvePending(alloc, encrypt["CF"])
	if !ok {
		return ""
	}
	std, ok := resolvePending(alloc, cf["StdCF"])
	if !ok {
		return ""
	}
	switch std["CFM"] {
	case ast.PdfName("AESV3"):
		return minAESV3Version
	case ast.PdfName("AESV2"):
		return minAESV2Version
	}
	return ""
}

// DeclaresPDF2 reports whether a file whose header names header and whose
// catalog is the trailer's /Root in alloc declares PDF 2.0 or later.
//
// Per ISO 32000-1, 7.5.2 a catalog /Version later than the header is the
// version the file declares, so the later of the two answers. A /Version that
// is not a version number is ignored, as readers ignore it. The raises
// [WriteRewrite] applies for cross-reference streams and encryption stop at
// 1.7, so the answer before them is the answer after.
func DeclaresPDF2(alloc *ObjAllocator, trailer ast.PdfDict, header string) bool {
	declared := header
	if alloc != nil {
		if catalog, ok := resolvePending(alloc, trailer["Root"]); ok {
			if v, ok := catalog["Version"].(ast.PdfName); ok {
				declared = atLeastVersion(declared, string(v))
			}
		}
	}
	return atLeastVersion(declared, isoPDF2Version) == declared
}

// resolvePending returns obj as a dictionary, following one level of
// indirection through the allocator. mutations.applyEncryption stores the
// /Encrypt dictionary as an indirect reference, while a nested /CF sits inline.
func resolvePending(alloc *ObjAllocator, obj ast.PdfObject) (ast.PdfDict, bool) {
	switch v := obj.(type) {
	case ast.PdfDict:
		return v, true
	case ast.PdfIndirectRef:
		dict, ok := alloc.Pending()[v.ObjectNum].(ast.PdfDict)
		return dict, ok
	}
	return nil, false
}

// ensureADBEExtension records in the catalog the Adobe extension level that
// legitimises AES-256 under a PDF 1.7 base version.
//
// Declaring 1.7 alone would misdescribe the file just as 1.4 did: /V 5 is not
// a PDF 1.7 construct, it is an extension to it, and ISO 32000-1, 7.12 gives
// /Extensions as the way a file names the extension it relies on. qpdf writes
// the same pair.
//
// Existing entries are raised, never lowered. A source that already declares a
// later base version or a higher extension level knows something this writer
// does not, and overwriting it would narrow a claim the document depends on.
func ensureADBEExtension(alloc *ObjAllocator, trailer ast.PdfDict, baseVersion string, level int) {
	if alloc == nil || trailer == nil {
		return
	}
	root, ok := trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return
	}
	catalog, ok := alloc.Pending()[root.ObjectNum].(ast.PdfDict)
	if !ok {
		return
	}

	adbe := ast.PdfDict{
		"BaseVersion":    ast.PdfName(baseVersion),
		"ExtensionLevel": ast.PdfInt(level),
	}
	extensions := ast.PdfDict{}
	if existing, ok := resolvePending(alloc, catalog["Extensions"]); ok {
		extensions = make(ast.PdfDict, len(existing)+1)
		maps.Copy(extensions, existing)
		if prior, ok := resolvePending(alloc, existing["ADBE"]); ok {
			adbe = mergeADBE(prior, baseVersion, level)
		}
	}
	extensions["ADBE"] = adbe

	// Replaced rather than mutated, for the reason [raiseCatalogVersion] gives.
	next := make(ast.PdfDict, len(catalog)+1)
	maps.Copy(next, catalog)
	next["Extensions"] = extensions
	alloc.AllocAt(root.ObjectNum, next)
}

// mergeADBE returns prior with its base version and extension level raised to
// at least the given values, leaving every other entry untouched.
func mergeADBE(prior ast.PdfDict, baseVersion string, level int) ast.PdfDict {
	merged := make(ast.PdfDict, len(prior)+2)
	maps.Copy(merged, prior)

	base := baseVersion
	if name, ok := prior["BaseVersion"].(ast.PdfName); ok {
		base = atLeastVersion(string(name), baseVersion)
	}
	merged["BaseVersion"] = ast.PdfName(base)

	if n, ok := prior["ExtensionLevel"].(ast.PdfInt); ok && intconv.Clamp(n) > level {
		merged["ExtensionLevel"] = n
	} else {
		merged["ExtensionLevel"] = ast.PdfInt(level)
	}
	return merged
}
