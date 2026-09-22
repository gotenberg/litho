package writer

import (
	"maps"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// minXRefStreamVersion is the earliest PDF version that defines object streams
// and cross-reference streams (ISO 32000-1, 7.5.7 and 7.5.8). It is not a
// tuning knob: a file using either construct while declaring an earlier
// version misdescribes itself, and a reader honoring that declaration cannot
// parse the cross-reference at all.
const minXRefStreamVersion = "1.5"

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
