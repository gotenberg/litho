package odf

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/offcrypto"
)

// The caps of one package.
const (
	// MaxEntries bounds the entries of a package's zip archive: 10,000.
	// Anti-DoS guard, not spec-mandated: a document with hundreds of
	// pictures holds a few hundred entries, and every entry costs a
	// central directory record read up front.
	MaxEntries = 10_000
	// MaxEntryBytes bounds the uncompressed size of one entry read: 128
	// MiB. Anti-DoS guard, not spec-mandated: the content.xml of a
	// thousand-page document stays under 20 MiB, and a deflate stream
	// inflates up to 1032 times its size, so the cap stops a zip bomb.
	MaxEntryBytes = 128 << 20
	// MaxTotalBytes bounds the uncompressed bytes all the reads of one
	// package take together: 512 MiB. Anti-DoS guard, not spec-mandated:
	// it bounds the work a package of many entries each under
	// [MaxEntryBytes] can make a converter do.
	MaxTotalBytes = 512 << 20
	// MaxDecryptedEntries bounds the encrypted entries one package
	// decrypts: 256. Anti-DoS guard, not spec-mandated: each decryption
	// derives its key anew at the cost the manifest names (PBKDF2 up to
	// offcrypto's iteration cap), and a document holds its four XML
	// documents, a thumbnail and its pictures, a few dozen entries.
	MaxDecryptedEntries = 256
)

// encryptedPackage is the entry of LibreOffice's whole-package encryption
// (ODF 1.4 extended mode, LibreOffice 24.2 and later): one encrypted
// entry holding the zip archive of the document, beside the mimetype and
// a manifest that describes it alone.
const encryptedPackage = "encrypted-package"

// Options configure [Open]. The zero value opens a document stored in the
// clear.
type Options struct {
	// Password opens the encrypted entries of a package (ODF 1.3, Part 2,
	// 3.4). Empty, an encrypted entry fails with office.ErrEncrypted; a
	// wrong password fails alike. A flat document is never encrypted and
	// ignores it.
	Password string
}

// The media types of the ODF documents (ODF 1.3, Part 2, 3.3 and Part 3,
// Appendix C).
const (
	MimeText                 = "application/vnd.oasis.opendocument.text"
	MimeTextTemplate         = "application/vnd.oasis.opendocument.text-template"
	MimeSpreadsheet          = "application/vnd.oasis.opendocument.spreadsheet"
	MimePresentation         = "application/vnd.oasis.opendocument.presentation"
	MimeTextMaster           = "application/vnd.oasis.opendocument.text-master"
	MimeTextWeb              = "application/vnd.oasis.opendocument.text-web"
	MimeSpreadsheetTemplate  = "application/vnd.oasis.opendocument.spreadsheet-template"
	MimePresentationTemplate = "application/vnd.oasis.opendocument.presentation-template"
)

// ManifestEntry is one manifest:file-entry of META-INF/manifest.xml (ODF
// 1.3, Part 2, 4.3).
type ManifestEntry struct {
	// FullPath is the entry's path in the package, "/" for the package
	// itself.
	FullPath string
	// MediaType is its media type, "" when the manifest gives none.
	MediaType string
	// Version is the ODF version of a document entry.
	Version string
	// Size is the uncompressed size of an encrypted entry, 0 when absent.
	Size int64
	// Encryption holds the entry's manifest:encryption-data, nil for an
	// entry stored in the clear.
	Encryption *EncryptionData
}

// EncryptionData is the manifest:encryption-data of an encrypted entry
// (ODF 1.3, Part 2, 4.8 to 4.12), each value as the manifest spells it:
// base64 for the binary ones, decimal for the numbers, "" when absent.
// It is what a decryptor needs to open the entry with a password; the
// decryptor decodes and checks the values.
type EncryptionData struct {
	// ChecksumType and Checksum are manifest:checksum-type and
	// manifest:checksum: the digest of the start of the decrypted entry.
	ChecksumType, Checksum string
	// AlgorithmName and InitialisationVector are manifest:algorithm's
	// manifest:algorithm-name and manifest:initialisation-vector (4.9).
	AlgorithmName, InitialisationVector string
	// StartKeyGenerationName and StartKeySize are
	// manifest:start-key-generation's manifest:start-key-generation-name
	// and manifest:key-size (4.11).
	StartKeyGenerationName, StartKeySize string
	// KeyDerivationName, KeySize, IterationCount and Salt are
	// manifest:key-derivation's attributes (4.10).
	KeyDerivationName, KeySize, IterationCount, Salt string
	// Argon2Iterations, Argon2Memory and Argon2Lanes are the Argon2id
	// parameters of manifest:key-derivation: loext:argon2-iterations,
	// argon2-memory and argon2-lanes as LibreOffice 24.2 and later writes
	// them, or their manifest: namespace spelling.
	Argon2Iterations, Argon2Memory, Argon2Lanes string
	// Size is the file entry's manifest:size, the entry's size before
	// encryption.
	Size string
}

// Package is an opened ODF document: a zip package or a flat XML document
// (ODF 1.3, Part 2, 3, and Part 3, 2.1.2). It is safe for concurrent use.
type Package struct {
	// MimeType is the document's media type: the mimetype entry, else the
	// manifest's root entry, else the flat document's office:mimetype.
	MimeType string
	// Flat reports a single XML document: Content, Styles, Meta and
	// Settings are then its one root, office:document.
	Flat bool
	// Manifest lists META-INF/manifest.xml's entries in order; empty for
	// a flat document.
	Manifest []ManifestEntry
	// Content, Styles, Meta and Settings are the roots of content.xml,
	// styles.xml, meta.xml and settings.xml (ODF 1.3, Part 3, 3.1), nil
	// for a document the package does not hold.
	Content, Styles, Meta, Settings *Element

	files    map[string]*zip.File
	enc      map[string]*EncryptionData
	password string
	mu       sync.Mutex
	read     int64
	opened   int
}

// zipMagic starts a zip archive's first local file header.
var zipMagic = []byte("PK\x03\x04")

// Open reads the ODF document data: a zip package (told by its first
// bytes), else a flat XML document. A package needs content.xml; a flat
// document an office:document root.
//
// The encrypted entries of a package (ODF 1.3, Part 2, 3.4: every entry
// whose manifest entry holds a manifest:encryption-data) are decrypted
// with opts.Password through offcrypto when they are read: content.xml,
// styles.xml, meta.xml and settings.xml by Open, the others by
// [Package.ReadFile]. A package LibreOffice encrypted whole, its one
// encrypted-package entry holding the document's zip archive, is
// decrypted and opened in its place.
//
// It fails with office.ErrMalformed for anything else, office.ErrEncrypted
// for a package whose content or styles are encrypted and opts.Password
// is empty or does not open, office.ErrInputTooLarge past the caps, and
// ctx's error when ctx ends first.
func Open(ctx context.Context, data []byte, opts Options) (*Package, error) {
	if bytes.HasPrefix(data, zipMagic) {
		return openZip(ctx, data, opts.Password, true, 0)
	}
	return openFlat(ctx, data)
}

// openFlat parses a flat XML document (ODF 1.3, Part 3, 2.1.2).
func openFlat(ctx context.Context, data []byte) (*Package, error) {
	root, err := ParseXML(ctx, data)
	if err != nil {
		return nil, err
	}
	if !root.Is(NSOffice, "document") {
		return nil, fmt.Errorf("%w: the root element is not office:document", office.ErrMalformed)
	}
	return &Package{
		MimeType: root.A(NSOffice, "mimetype"),
		Flat:     true,
		Content:  root, Styles: root, Meta: root, Settings: root,
	}, nil
}

// openZip reads a zip package (ODF 1.3, Part 2, 3). outer allows the
// whole-package encryption: the package it decrypts is opened with outer
// false, so an encrypted package nests once at most. spent is what the
// package's reads start from against [MaxTotalBytes]: the package an
// encrypted one decrypts reads within what its outer package left.
func openZip(ctx context.Context, data []byte, password string, outer bool, spent int64) (*Package, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: reading the zip archive: %w", office.ErrMalformed, err)
	}
	if len(zr.File) > MaxEntries {
		return nil, fmt.Errorf("%w: more than %d package entries", office.ErrInputTooLarge, MaxEntries)
	}
	p := &Package{files: make(map[string]*zip.File, len(zr.File)), enc: map[string]*EncryptionData{}, password: password, read: spent}
	for _, f := range zr.File {
		name := strings.TrimPrefix(path.Clean("/"+f.Name), "/")
		if _, dup := p.files[name]; !dup {
			p.files[name] = f
		}
	}
	if mt, err := p.readRaw("mimetype"); err == nil {
		p.MimeType = strings.TrimSpace(string(mt))
	} else if !errors.Is(err, errNoEntry) {
		return nil, err
	}
	if m, err := p.readRaw("META-INF/manifest.xml"); err == nil {
		if err := p.parseManifest(ctx, m); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, errNoEntry) {
		return nil, err
	}
	if p.enc[encryptedPackage] != nil && !p.Has("content.xml") {
		if !outer {
			return nil, fmt.Errorf("%w: an encrypted package inside an encrypted package", office.ErrMalformed)
		}
		inner, err := p.ReadFile(ctx, encryptedPackage)
		if err != nil {
			return nil, err
		}
		// The decrypted archive is held as long as the package it opens.
		if err := limits.BudgetFrom(ctx).ChargeUnits(int64(len(inner)), 1, "the decrypted package"); err != nil {
			return nil, err
		}
		if !bytes.HasPrefix(inner, zipMagic) {
			return nil, fmt.Errorf("%w: the encrypted package is not a zip archive", office.ErrMalformed)
		}
		q, err := openZip(ctx, inner, password, false, p.read)
		if err != nil {
			return nil, fmt.Errorf("opening the encrypted package: %w", err)
		}
		if q.MimeType == "" {
			q.MimeType = p.MimeType
		}
		return q, nil
	}
	for _, d := range []struct {
		name string
		dst  **Element
		need bool
	}{
		{"content.xml", &p.Content, true},
		{"styles.xml", &p.Styles, false},
		{"meta.xml", &p.Meta, false},
		{"settings.xml", &p.Settings, false},
	} {
		if err := p.parseEntry(ctx, d.name, d.dst, d.need); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// parseEntry reads and parses the XML document of the entry name into
// dst, failing when need is set and the package does not hold it. The
// inflated entry is held while it is parsed: its declared size is charged
// to the conversion's budget before it is inflated, and given back once
// the tree is built.
func (p *Package) parseEntry(ctx context.Context, name string, dst **Element, need bool) error {
	mem := limits.BudgetFrom(ctx)
	var declared int64
	if f := p.files[name]; f != nil {
		declared = int64(min(f.UncompressedSize64, MaxEntryBytes)) //nolint:gosec // fits int64: at most MaxEntryBytes
	}
	if err := mem.Charge(declared, "inflated package parts"); err != nil {
		return fmt.Errorf("reading %s: %w", name, err)
	}
	defer mem.Release(declared)
	raw, err := p.ReadFile(ctx, name)
	if errors.Is(err, errNoEntry) {
		if need {
			return fmt.Errorf("%w: the package has no %s", office.ErrMalformed, name)
		}
		return nil
	}
	if err != nil {
		return err
	}
	if *dst, err = ParseXML(ctx, raw); err != nil {
		return fmt.Errorf("parsing %s: %w", name, err)
	}
	return nil
}

// errNoEntry reports a package entry that does not exist.
var errNoEntry = fmt.Errorf("%w: no such package entry", office.ErrMalformed)

// IsNotExist reports whether err is [Package.ReadFile]'s error for an
// entry the package does not hold.
func IsNotExist(err error) bool { return errors.Is(err, errNoEntry) }

// Has reports whether the package holds the entry name.
func (p *Package) Has(name string) bool {
	_, ok := p.files[cleanName(name)]
	return ok
}

// cleanName returns the package path of an entry reference, relative to
// the package root: "./Pictures/a.png" and "/Pictures/a.png" are
// "Pictures/a.png" (ODF 1.3, Part 2, 3.7, IRIs relative to the package).
func cleanName(name string) string {
	return strings.TrimPrefix(path.Clean("/"+name), "/")
}

// Encryption returns the encryption data of the entry name, nil for an
// entry stored in the clear.
func (p *Package) Encryption(name string) *EncryptionData {
	return p.enc[cleanName(name)]
}

// ReadFile returns the uncompressed bytes of the entry name, decrypted
// with [Options.Password] when the manifest marks it encrypted. It fails
// with an error [IsNotExist] reports for an entry the package does not
// hold; office.ErrEncrypted for an encrypted one when the password is
// empty or does not open it, or past [MaxDecryptedEntries] decryptions;
// office.ErrInputTooLarge past [MaxEntryBytes] or [MaxTotalBytes]; and
// ctx's error when ctx ends first.
func (p *Package) ReadFile(ctx context.Context, name string) ([]byte, error) {
	name = cleanName(name)
	if ed := p.enc[name]; ed != nil {
		return p.readEncrypted(ctx, name, ed)
	}
	return p.readRaw(name)
}

// readEncrypted decrypts the entry name (ODF 1.3, Part 2, 3.4): its
// stored bytes, which the archive holds STORED (3.4.1), go to offcrypto
// with the manifest's parameters.
func (p *Package) readEncrypted(ctx context.Context, name string, ed *EncryptionData) ([]byte, error) {
	if p.password == "" {
		return nil, fmt.Errorf("%w: the package entry %s is encrypted and no password was given", office.ErrEncrypted, name)
	}
	f := p.files[name]
	if f == nil {
		return nil, fmt.Errorf("%w: %s", errNoEntry, name)
	}
	if f.Method != zip.Store {
		return nil, fmt.Errorf("%w: the encrypted package entry %s is compressed, not STORED", office.ErrMalformed, name)
	}
	if f.CompressedSize64 > MaxEntryBytes {
		return nil, fmt.Errorf("%w: the package entry %s is past the caps", office.ErrInputTooLarge, name)
	}
	p.mu.Lock()
	if p.opened >= MaxDecryptedEntries {
		p.mu.Unlock()
		return nil, fmt.Errorf("%w: more than %d encrypted package entries", office.ErrEncrypted, MaxDecryptedEntries)
	}
	p.opened++
	p.mu.Unlock()
	rc, err := f.OpenRaw()
	if err != nil {
		return nil, fmt.Errorf("%w: opening the package entry %s: %w", office.ErrMalformed, name, err)
	}
	stored, err := io.ReadAll(io.LimitReader(rc, MaxEntryBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: reading the package entry %s: %w", office.ErrMalformed, name, err)
	}
	data, err := offcrypto.OpenODFEntry(ctx, stored, ed.params(), p.password)
	if err != nil {
		return nil, fmt.Errorf("decrypting the package entry %s: %w", name, err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if int64(len(data)) > min(int64(MaxEntryBytes), MaxTotalBytes-p.read) {
		return nil, fmt.Errorf("%w: the package entry %s inflates past the caps", office.ErrInputTooLarge, name)
	}
	p.read += int64(len(data))
	return data, nil
}

// readRaw reads the entry name as stored, decompressed, within the caps.
func (p *Package) readRaw(name string) ([]byte, error) {
	f := p.files[name]
	if f == nil {
		return nil, fmt.Errorf("%w: %s", errNoEntry, name)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("%w: opening the package entry %s: %w", office.ErrMalformed, name, err)
	}
	defer func() { _ = rc.Close() }() // a read-only zip entry reports nothing worth handling on close
	p.mu.Lock()
	room := min(office.InflateLimit(f.CompressedSize64, MaxEntryBytes), MaxTotalBytes-p.read)
	p.mu.Unlock()
	data, err := io.ReadAll(io.LimitReader(rc, room+1))
	if err != nil {
		return nil, fmt.Errorf("%w: reading the package entry %s: %w", office.ErrMalformed, name, err)
	}
	if int64(len(data)) > room {
		return nil, fmt.Errorf("%w: the package entry %s inflates past the caps", office.ErrInputTooLarge, name)
	}
	p.mu.Lock()
	p.read += int64(len(data))
	p.mu.Unlock()
	return data, nil
}

// parseManifest reads META-INF/manifest.xml (ODF 1.3, Part 2, 4).
func (p *Package) parseManifest(ctx context.Context, data []byte) error {
	root, err := ParseXML(ctx, data)
	if err != nil {
		return fmt.Errorf("parsing the manifest: %w", err)
	}
	if !root.Is(NSManifest, "manifest") {
		return fmt.Errorf("%w: the manifest's root is not manifest:manifest", office.ErrMalformed)
	}
	for _, fe := range root.All(NSManifest, "file-entry") {
		e := ManifestEntry{
			FullPath:  fe.A(NSManifest, "full-path"),
			MediaType: fe.A(NSManifest, "media-type"),
			Version:   fe.A(NSManifest, "version"),
		}
		if s, err := strconv.ParseInt(fe.A(NSManifest, "size"), 10, 64); err == nil && s > 0 {
			e.Size = s
		}
		if ed := fe.Child(NSManifest, "encryption-data"); ed != nil {
			e.Encryption = parseEncryption(fe, ed)
			if e.FullPath != "/" {
				p.enc[cleanName(e.FullPath)] = e.Encryption
			}
		}
		if e.FullPath == "/" && p.MimeType == "" {
			p.MimeType = e.MediaType
		}
		p.Manifest = append(p.Manifest, e)
	}
	return nil
}

// parseEncryption reads a manifest:encryption-data element (ODF 1.3,
// Part 2, 4.8 to 4.12) of the file entry fe.
func parseEncryption(fe, ed *Element) *EncryptionData {
	argon := func(k *Element, local string) string {
		if v, ok := k.Lookup(NSLOExt, local); ok {
			return v
		}
		return k.A(NSManifest, local)
	}
	d := &EncryptionData{
		ChecksumType: ed.A(NSManifest, "checksum-type"),
		Checksum:     ed.A(NSManifest, "checksum"),
		Size:         fe.A(NSManifest, "size"),
	}
	if a := ed.Child(NSManifest, "algorithm"); a != nil {
		d.AlgorithmName = a.A(NSManifest, "algorithm-name")
		d.InitialisationVector = a.A(NSManifest, "initialisation-vector")
	}
	if s := ed.Child(NSManifest, "start-key-generation"); s != nil {
		d.StartKeyGenerationName = s.A(NSManifest, "start-key-generation-name")
		d.StartKeySize = s.A(NSManifest, "key-size")
	}
	if k := ed.Child(NSManifest, "key-derivation"); k != nil {
		d.KeyDerivationName = k.A(NSManifest, "key-derivation-name")
		d.KeySize = k.A(NSManifest, "key-size")
		d.IterationCount = k.A(NSManifest, "iteration-count")
		d.Salt = k.A(NSManifest, "salt")
		d.Argon2Iterations = argon(k, "argon2-iterations")
		d.Argon2Memory = argon(k, "argon2-memory")
		d.Argon2Lanes = argon(k, "argon2-lanes")
	}
	return d
}

// Section returns the child office:name of the document root of part:
// office:styles, office:automatic-styles, office:master-styles,
// office:font-face-decls, office:body or office:meta. nil when absent.
func Section(part *Element, name string) *Element {
	return part.Child(NSOffice, name)
}

// params returns the parameters offcrypto reads, field for field.
func (d *EncryptionData) params() offcrypto.ODFParams {
	return offcrypto.ODFParams{
		Size:         d.Size,
		ChecksumType: d.ChecksumType, Checksum: d.Checksum,
		AlgorithmName: d.AlgorithmName, InitialisationVector: d.InitialisationVector,
		StartKeyGenerationName: d.StartKeyGenerationName, StartKeySize: d.StartKeySize,
		KeyDerivationName: d.KeyDerivationName, KeySize: d.KeySize, IterationCount: d.IterationCount, Salt: d.Salt,
		Argon2Iterations: d.Argon2Iterations, Argon2Memory: d.Argon2Memory, Argon2Lanes: d.Argon2Lanes,
	}
}
