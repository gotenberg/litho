package office

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// MaxPackageEntries caps the entries of a package at 65,536. A DOCX or ODT
// holds a few dozen, one with many images a few hundred. Anti-DoS guard,
// not spec-mandated: APPNOTE.TXT allows 2^64 in ZIP64.
const MaxPackageEntries = 1 << 16

// DefaultMaxEntryBytes caps one inflated entry when [Package.Read] is given
// no cap: the bound litho puts on one decoded PDF stream, 256 MiB.
// Anti-DoS guard, not spec-mandated: a deflate stream inflates up to about
// a thousand times its size.
const DefaultMaxEntryBytes = limits.DefaultMaxStreamBytes

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
	if compressed > uint64(limit/MaxInflateRatio) {
		return limit
	}
	return min(limit, max(int64(compressed)*MaxInflateRatio, inflateRatioFloor)) // fits int64: compressed is at most limit / MaxInflateRatio, checked above
}

// Package is a zip archive an Office document is (ECMA-376 Part 2, 8 and
// Annex C; ODF 1.3 Part 2, 3 and Appendix C), read from memory. It is safe
// for concurrent use: every method reads it without changing it.
type Package struct {
	files []*zip.File
	exact map[string]int
	fold  map[string]int
}

// OpenPackage reads the central directory of the zip archive data holds.
// data is retained, not copied, and must not change while the Package is in
// use. It fails with [ErrMalformed] when data is not a zip archive or names
// an entry twice, and with [ErrInputTooLarge] past [MaxPackageEntries]
// entries.
func OpenPackage(data []byte) (*Package, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		return nil, fmt.Errorf("%w: not a zip archive: %w", ErrMalformed, err)
	}
	if len(zr.File) > MaxPackageEntries {
		return nil, fmt.Errorf("%w: %d package entries past the cap of %d", ErrInputTooLarge, len(zr.File), MaxPackageEntries)
	}
	p := &Package{files: zr.File, exact: make(map[string]int, len(zr.File)), fold: make(map[string]int, len(zr.File))}
	for i, f := range zr.File {
		if _, dup := p.exact[f.Name]; dup {
			return nil, fmt.Errorf("%w: the package names %q twice", ErrMalformed, f.Name)
		}
		p.exact[f.Name] = i
		// ECMA-376 Part 2, 6.2.2.3: part names compare case-insensitively
		// (ASCII); the first of two names that fold together wins.
		if _, ok := p.fold[strings.ToLower(f.Name)]; !ok {
			p.fold[strings.ToLower(f.Name)] = i
		}
	}
	return p, nil
}

// Names returns the entry names in archive order. The slice is new.
func (p *Package) Names() []string {
	out := make([]string, len(p.files))
	for i, f := range p.files {
		out[i] = f.Name
	}
	return out
}

// lookup finds name exactly, or case-insensitively when fold is set.
func (p *Package) lookup(name string, fold bool) (*zip.File, bool) {
	if i, ok := p.exact[name]; ok {
		return p.files[i], true
	}
	if fold {
		if i, ok := p.fold[strings.ToLower(name)]; ok {
			return p.files[i], true
		}
	}
	return nil, false
}

// Has reports whether the package has an entry named name exactly.
func (p *Package) Has(name string) bool {
	_, ok := p.exact[name]
	return ok
}

// Read returns the content of the entry named name, inflated, at most
// maxBytes bytes ([DefaultMaxEntryBytes] when maxBytes is zero or less).
// fold looks the name up case-insensitively when no entry has it exactly,
// as OOXML part names compare (ECMA-376 Part 2, 6.2.2.3); ODF names are
// case-sensitive. It fails with [ErrMalformed] when the entry is missing,
// uses a compression method other than STORED or DEFLATED, or fails its
// CRC-32, and with [ErrInputTooLarge] past maxBytes. It returns ctx's error
// when ctx ends first.
func (p *Package) Read(ctx context.Context, name string, fold bool, maxBytes int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, ok := p.lookup(name, fold)
	if !ok {
		return nil, fmt.Errorf("%w: no package entry %q", ErrMalformed, name)
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxEntryBytes
	}
	maxBytes = InflateLimit(f.CompressedSize64, maxBytes)
	if f.UncompressedSize64 > uint64(maxBytes) {
		return nil, fmt.Errorf("%w: entry %q of %d bytes past the cap of %d", ErrInputTooLarge, name, f.UncompressedSize64, maxBytes)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("%w: entry %q: %w", ErrMalformed, name, err)
	}
	defer rc.Close() //nolint:errcheck // a reader over memory has nothing to release
	// The declared size is not trusted: the read stops one byte past the
	// cap whatever the header says.
	out, err := io.ReadAll(io.LimitReader(rc, maxBytes+1))
	if int64(len(out)) > maxBytes {
		return nil, fmt.Errorf("%w: entry %q inflates past the cap of %d", ErrInputTooLarge, name, maxBytes)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: entry %q: %w", ErrMalformed, name, err)
	}
	return out, nil
}

// ReadStored returns the bytes of the entry named name as the archive
// stores them, which an encrypted ODF entry is (ODF 1.3 Part 2, 3.4.1: it
// is flagged STORED and holds its deflated, encrypted content). The slice
// is new. It fails with [ErrMalformed] when the entry is missing or not
// STORED, and with [ErrInputTooLarge] past [DefaultMaxEntryBytes].
func (p *Package) ReadStored(name string) ([]byte, error) {
	f, ok := p.lookup(name, false)
	if !ok {
		return nil, fmt.Errorf("%w: no package entry %q", ErrMalformed, name)
	}
	if f.Method != zip.Store {
		return nil, fmt.Errorf("%w: entry %q is compressed, an encrypted entry is STORED", ErrMalformed, name)
	}
	if f.CompressedSize64 > DefaultMaxEntryBytes {
		return nil, fmt.Errorf("%w: entry %q of %d bytes past the cap of %d", ErrInputTooLarge, name, f.CompressedSize64, int64(DefaultMaxEntryBytes))
	}
	r, err := f.OpenRaw()
	if err != nil {
		return nil, fmt.Errorf("%w: entry %q: %w", ErrMalformed, name, err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("%w: entry %q: %w", ErrMalformed, name, err)
	}
	return out, nil
}
