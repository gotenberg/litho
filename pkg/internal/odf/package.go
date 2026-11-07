package odf

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
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
)

// The media types of the ODF documents (ODF 1.3, Part 2, 3.3 and Part 3,
// Appendix C).
const (
	MimeText                = "application/vnd.oasis.opendocument.text"
	MimeTextTemplate        = "application/vnd.oasis.opendocument.text-template"
	MimeSpreadsheet         = "application/vnd.oasis.opendocument.spreadsheet"
	MimePresentation        = "application/vnd.oasis.opendocument.presentation"
	MimeTextMaster          = "application/vnd.oasis.opendocument.text-master"
	MimeTextWeb             = "application/vnd.oasis.opendocument.text-web"
	MimeSpreadsheetTemplate = "application/vnd.oasis.opendocument.spreadsheet-template"
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
// (ODF 1.3, Part 2, 4.8 to 4.12), with the values decoded from base64:
// what a decryptor needs to open the entry with a password.
type EncryptionData struct {
	// ChecksumType names the digest of the start of the entry's
	// decrypted bytes, as written: a URI or LibreOffice's
	// "SHA1/1K" and "urn:oasis:names:tc:opendocument:xmlns:manifest:1.0#sha256-1k".
	ChecksumType string
	Checksum     []byte
	// AlgorithmName names the cipher (4.9), as written; IV is its
	// initialisation vector.
	AlgorithmName string
	IV            []byte
	// StartKeyName names the digest of the password that starts the key
	// derivation (4.11), as written, and StartKeySize its size in bytes, 0
	// when absent.
	StartKeyName string
	StartKeySize int
	// KeyDerivationName names the key derivation (4.10), as written:
	// PBKDF2 or Argon2id.
	KeyDerivationName string
	Salt              []byte
	IterationCount    int
	// KeySize is the derived key's size in bytes, 0 when absent (16 then,
	// as ODF 1.3 specifies for Blowfish).
	KeySize int
	// ArgonIterations, ArgonMemory (in KiB) and ArgonLanes are the
	// Argon2id parameters LibreOffice 24.2 and later write (loext:
	// argon2-iterations, argon2-memory and argon2-lanes), 0 when absent.
	ArgonIterations, ArgonMemory, ArgonLanes int
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

	files map[string]*zip.File
	enc   map[string]*EncryptionData
	mu    sync.Mutex
	read  int64
}

// zipMagic starts a zip archive's first local file header.
var zipMagic = []byte("PK\x03\x04")

// Open reads the ODF document data: a zip package (told by its first
// bytes), else a flat XML document. A package needs content.xml; a flat
// document an office:document root. It fails with office.ErrMalformed
// for anything else, office.ErrEncrypted for a package whose content or
// styles are encrypted, office.ErrInputTooLarge past the caps, and ctx's
// error when ctx ends first.
func Open(ctx context.Context, data []byte) (*Package, error) {
	if bytes.HasPrefix(data, zipMagic) {
		return openZip(ctx, data)
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

// openZip reads a zip package (ODF 1.3, Part 2, 3).
func openZip(ctx context.Context, data []byte) (*Package, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: reading the zip archive: %w", office.ErrMalformed, err)
	}
	if len(zr.File) > MaxEntries {
		return nil, fmt.Errorf("%w: more than %d package entries", office.ErrInputTooLarge, MaxEntries)
	}
	p := &Package{files: make(map[string]*zip.File, len(zr.File)), enc: map[string]*EncryptionData{}}
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
		raw, err := p.ReadFile(d.name)
		if errors.Is(err, errNoEntry) {
			if d.need {
				return nil, fmt.Errorf("%w: the package has no %s", office.ErrMalformed, d.name)
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		if *d.dst, err = ParseXML(ctx, raw); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", d.name, err)
		}
	}
	return p, nil
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

// ReadFile returns the uncompressed bytes of the entry name. It fails
// with an error [IsNotExist] reports for an entry the package does not
// hold, office.ErrEncrypted for an encrypted one, and
// office.ErrInputTooLarge past [MaxEntryBytes] or [MaxTotalBytes].
func (p *Package) ReadFile(name string) ([]byte, error) {
	name = cleanName(name)
	if p.enc[name] != nil {
		return nil, fmt.Errorf("%w: the package entry %s is encrypted", office.ErrEncrypted, name)
	}
	return p.readRaw(name)
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
	room := min(int64(MaxEntryBytes), MaxTotalBytes-p.read)
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
			e.Encryption = parseEncryption(ed)
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
// Part 2, 4.8 to 4.12). A value that does not decode is left empty: the
// decryptor then refuses the entry.
func parseEncryption(ed *Element) *EncryptionData {
	b64 := func(s string) []byte {
		v, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
		if err != nil {
			return nil
		}
		return v
	}
	num := func(e *Element, space, local string) int {
		v, err := strconv.Atoi(strings.TrimSpace(e.A(space, local)))
		if err != nil || v < 0 {
			return 0
		}
		return v
	}
	d := &EncryptionData{
		ChecksumType: ed.A(NSManifest, "checksum-type"),
		Checksum:     b64(ed.A(NSManifest, "checksum")),
	}
	if a := ed.Child(NSManifest, "algorithm"); a != nil {
		d.AlgorithmName = a.A(NSManifest, "algorithm-name")
		d.IV = b64(a.A(NSManifest, "initialisation-vector"))
	}
	if s := ed.Child(NSManifest, "start-key-generation"); s != nil {
		d.StartKeyName = s.A(NSManifest, "start-key-generation-name")
		d.StartKeySize = num(s, NSManifest, "key-size")
	}
	if k := ed.Child(NSManifest, "key-derivation"); k != nil {
		d.KeyDerivationName = k.A(NSManifest, "key-derivation-name")
		d.Salt = b64(k.A(NSManifest, "salt"))
		d.IterationCount = num(k, NSManifest, "iteration-count")
		d.KeySize = num(k, NSManifest, "key-size")
		d.ArgonIterations = num(k, NSLOExt, "argon2-iterations")
		d.ArgonMemory = num(k, NSLOExt, "argon2-memory")
		d.ArgonLanes = num(k, NSLOExt, "argon2-lanes")
	}
	return d
}

// Section returns the child office:name of the document root of part:
// office:styles, office:automatic-styles, office:master-styles,
// office:font-face-decls, office:body or office:meta. nil when absent.
func Section(part *Element, name string) *Element {
	return part.Child(NSOffice, name)
}
