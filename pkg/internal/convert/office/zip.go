package office

import "github.com/gotenberg/litho/pkg/internal/limits"

// MaxPackageEntries caps the entries of a package at 65,536, OOXML and
// OpenDocument alike. A DOCX or ODT holds a few dozen, one with many images
// a few hundred. Anti-DoS guard, not spec-mandated: APPNOTE.TXT allows 2^64
// in ZIP64, and every entry costs a central directory record read up
// front.
const MaxPackageEntries = 1 << 16

// DefaultMaxEntryBytes caps one inflated entry of a package, OOXML and
// OpenDocument alike: the bound litho puts on one decoded PDF stream, 256
// MiB. Anti-DoS guard, not spec-mandated: the content.xml or document.xml
// of a thousand-page document stays under 20 MiB, and a deflate stream
// inflates up to about a thousand times its size.
const DefaultMaxEntryBytes = limits.DefaultMaxStreamBytes

// MaxPackageBytes caps the uncompressed bytes the reads of one package take
// together, 512 MiB, so a zip bomb of many entries each under
// [DefaultMaxEntryBytes] cannot exhaust memory. Anti-DoS guard, not
// spec-mandated: two entries at the cap of one.
const MaxPackageBytes = 2 * DefaultMaxEntryBytes

// MaxInflateRatio caps how many times its compressed size one package
// entry may inflate to, past [inflateRatioFloor] bytes: 600. It is an
// anti-DoS guard, not spec-mandated: APPNOTE.TXT puts no bound on a
// deflate stream's output, which reaches about 1,032 times its input, so
// a package of a few hundred kilobytes can inflate to gigabytes of
// parts, honest or lying in its headers. The read stops at the bound
// whatever the header declares.
//
// # Corpus evidence
//
//   - All Office corpora: of the 300 entries of 2,698 packages that
//     inflate past 64 KiB, the most compressed is an embedded object at
//     341 times (libreoffice/odp tdf169979.odp), the most compressed XML
//     part 141 times (libreoffice/ods tdf136551.ods content.xml).
const MaxInflateRatio = 600

// inflateRatioFloor is the inflated size below which [MaxInflateRatio]
// does not apply: 1 MiB. A small entry costs little whatever its ratio,
// and an empty part or a run of one repeated byte compresses far past it.
const inflateRatioFloor = 1 << 20

// InflateLimit returns the most bytes an entry of compressed bytes may
// inflate to under [MaxInflateRatio], at most limit.
func InflateLimit(compressed uint64, limit int64) int64 {
	if limit <= 0 || compressed > uint64(limit/MaxInflateRatio) { // limit is positive, checked first
		return limit
	}
	return min(limit, max(int64(compressed)*MaxInflateRatio, inflateRatioFloor)) //nolint:gosec // fits int64: compressed is at most limit / MaxInflateRatio, checked above
}
