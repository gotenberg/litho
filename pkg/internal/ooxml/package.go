package ooxml

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// MaxEntries caps the zip entries of a package. Anti-DoS guard, not
// spec-mandated: ECMA-376 Part 2 sets no limit, and a document with
// thousands of pictures stays under it.
const MaxEntries = 10_000

// MaxPartBytes caps the uncompressed size of one part. Anti-DoS guard,
// not spec-mandated: the largest parts of real documents, a long
// document.xml or a photograph, are a few tens of megabytes.
const MaxPartBytes = 128 << 20

// MaxPackageBytes caps the uncompressed bytes read from a package in all,
// so a zip bomb of many small entries cannot exhaust memory. Anti-DoS
// guard, not spec-mandated: four parts at [MaxPartBytes].
const MaxPackageBytes = 4 * MaxPartBytes

// cfbSignature starts a compound file (MS-CFB, 2.2): an encrypted OOXML
// package (MS-OFFCRYPTO, 2.3.4) or a binary Office document.
var cfbSignature = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}

// The relationship types the converters follow (ECMA-376 Part 1, 15.2;
// Part 2, 8.3 for the core properties).
const (
	RelOfficeDocument = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"
	RelCoreProperties = "http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties"
	RelStyles         = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles"
	RelNumbering      = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering"
	RelSettings       = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/settings"
	RelFontTable      = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/fontTable"
	RelTheme          = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme"
	RelHeader         = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/header"
	RelFooter         = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer"
	RelFootnotes      = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/footnotes"
	RelEndnotes       = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/endnotes"
	RelImage          = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/image"
	RelHyperlink      = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink"
)

// strictRelPrefix and transitionalRelPrefix are the relationship type
// prefixes of ISO/IEC 29500 Strict and Transitional (ISO/IEC 29500-1,
// Annex A).
const (
	strictRelPrefix       = "http://purl.oclc.org/ooxml/officeDocument/relationships/"
	transitionalRelPrefix = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
)

// Package is an opened OPC package (ECMA-376 Part 2). See the package
// documentation for its caps and concurrency contract.
type Package struct {
	// entries maps a part name, lower-cased without its leading slash
	// (part names compare case-insensitively, ECMA-376 Part 2, 9.1.1.1.2),
	// to its zip entry.
	entries   map[string]*zip.File
	defaults  map[string]string
	overrides map[string]string
	rels      map[string][]Relationship
	// read counts the uncompressed bytes read, against
	// [MaxPackageBytes]; elements counts the XML elements parsed, against
	// [MaxElements].
	read     int64
	elements int
}

// Relationship is one relationship of a package or a part (ECMA-376
// Part 2, 9.3).
type Relationship struct {
	ID   string
	Type string
	// Target is the target part's name, absolute and without its leading
	// slash, or the URI as written for an external target.
	Target   string
	External bool
}

// Open opens the package data holds. It fails with office.ErrEncrypted
// for a compound file, an encrypted package that needs a password first,
// with office.ErrMalformed for anything else that is not a zip archive or
// holds no content types, and with office.ErrInputTooLarge past
// [MaxEntries].
func Open(data []byte) (*Package, error) {
	if bytes.HasPrefix(data, cfbSignature) {
		return nil, fmt.Errorf("%w: a compound file, not a zip package", office.ErrEncrypted)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: not a zip package: %w", office.ErrMalformed, err)
	}
	if len(zr.File) > MaxEntries {
		return nil, fmt.Errorf("%w: %d zip entries, more than %d", office.ErrInputTooLarge, len(zr.File), MaxEntries)
	}
	p := &Package{
		entries:   make(map[string]*zip.File, len(zr.File)),
		defaults:  map[string]string{},
		overrides: map[string]string{},
		rels:      map[string][]Relationship{},
	}
	for _, f := range zr.File {
		name := partKey(f.Name)
		if _, dup := p.entries[name]; dup || name == "" || strings.HasSuffix(name, "/") {
			// The first of two entries of one name wins, as zip readers do;
			// ECMA-376 Part 2, 9.1.1.1.2 forbids equivalent part names.
			continue
		}
		p.entries[name] = f
	}
	if err := p.readContentTypes(); err != nil {
		return nil, err
	}
	return p, nil
}

// partKey normalises a part name or zip entry name into a map key.
func partKey(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	return strings.ToLower(strings.TrimPrefix(name, "/"))
}

// entry returns the zip entry of a part name, trying the name as written
// then percent-decoded (part names are URIs, ECMA-376 Part 2, 9.1.1).
func (p *Package) entry(name string) *zip.File {
	if f := p.entries[partKey(name)]; f != nil {
		return f
	}
	if u, err := url.PathUnescape(name); err == nil {
		return p.entries[partKey(u)]
	}
	return nil
}

// Has reports whether the package holds the part name.
func (p *Package) Has(name string) bool { return p.entry(name) != nil }

// Read returns the bytes of the part name. It fails with [ErrNoPart] for a
// part the package does not hold, with office.ErrInputTooLarge past
// [MaxPartBytes] or the package's [MaxPackageBytes], and with
// office.ErrMalformed for an entry that does not inflate.
func (p *Package) Read(name string) ([]byte, error) {
	f := p.entry(name)
	if f == nil {
		return nil, fmt.Errorf("%w: %s", ErrNoPart, name)
	}
	partCap := office.InflateLimit(f.CompressedSize64, MaxPartBytes)
	if f.UncompressedSize64 > uint64(partCap) {
		return nil, fmt.Errorf("%w: part %s is larger than %d bytes", office.ErrInputTooLarge, name, partCap)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("%w: opening part %s: %w", office.ErrMalformed, name, err)
	}
	defer rc.Close() //nolint:errcheck // a read-only zip entry has nothing to flush
	limit := min(partCap, MaxPackageBytes-p.read)
	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	p.read += int64(len(data))
	if err != nil {
		return nil, fmt.Errorf("%w: reading part %s: %w", office.ErrMalformed, name, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: part %s exceeds the package's byte budget", office.ErrInputTooLarge, name)
	}
	return data, nil
}

// ContentType returns the content type of the part name: its Override,
// else the Default of its extension, "" for none (ECMA-376 Part 2, 10.1.2).
func (p *Package) ContentType(name string) string {
	key := "/" + partKey(name)
	if ct, ok := p.overrides[key]; ok {
		return ct
	}
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(key), "."))
	return p.defaults[ext]
}

// readContentTypes reads [Content_Types].xml (ECMA-376 Part 2, 10.1.2).
func (p *Package) readContentTypes() error {
	if !p.Has("[Content_Types].xml") {
		return fmt.Errorf("%w: no [Content_Types].xml", office.ErrMalformed)
	}
	root, err := p.ParseXML(context.Background(), "[Content_Types].xml", nil)
	if err != nil {
		return err
	}
	for _, c := range root.Children {
		switch c.Local {
		case "Default":
			if ext, ok := c.Attr("Extension"); ok {
				p.defaults[strings.ToLower(ext)] = attrOr(c, "ContentType")
			}
		case "Override":
			if name, ok := c.Attr("PartName"); ok {
				p.overrides["/"+partKey(name)] = attrOr(c, "ContentType")
			}
		}
	}
	return nil
}

// Relationships returns the relationships of the part source, or of the
// package for source "" (ECMA-376 Part 2, 9.3.2: the part's relationships
// part sits in a _rels folder beside it), in document order; none when
// the relationships part is missing. Internal targets are resolved
// against source.
func (p *Package) Relationships(source string) ([]Relationship, error) {
	key := partKey(source)
	if rels, ok := p.rels[key]; ok {
		return rels, nil
	}
	dir, file := path.Split(key)
	relsName := dir + "_rels/" + file + ".rels"
	if !p.Has(relsName) {
		p.rels[key] = nil
		return nil, nil
	}
	root, err := p.ParseXML(context.Background(), relsName, nil)
	if err != nil {
		return nil, err
	}
	var rels []Relationship
	for _, c := range root.Children {
		if c.Local != "Relationship" {
			continue
		}
		r := Relationship{ID: attrOr(c, "Id"), Type: normalizeRelType(attrOr(c, "Type")), Target: attrOr(c, "Target")}
		if strings.EqualFold(attrOr(c, "TargetMode"), "External") {
			r.External = true
		} else {
			r.Target = ResolveTarget(source, r.Target)
		}
		rels = append(rels, r)
	}
	p.rels[key] = rels
	return rels, nil
}

// normalizeRelType maps a Strict relationship type to its Transitional
// name.
func normalizeRelType(t string) string {
	if rest, ok := strings.CutPrefix(t, strictRelPrefix); ok {
		return transitionalRelPrefix + rest
	}
	if t == "http://purl.oclc.org/ooxml/officeDocument/relationships/metadata/core-properties" {
		return RelCoreProperties
	}
	return t
}

// ResolveTarget resolves the internal relationship target against the
// part source (ECMA-376 Part 2, 9.3.3.1: a relative reference resolves
// against the source part's URI), returning a part name without its
// leading slash.
func ResolveTarget(source, target string) string {
	target = strings.ReplaceAll(target, `\`, "/")
	if i := strings.IndexAny(target, "#?"); i >= 0 {
		target = target[:i]
	}
	var joined string
	if strings.HasPrefix(target, "/") {
		joined = target
	} else {
		joined = path.Join("/"+path.Dir(strings.TrimPrefix(source, "/")), target)
	}
	return strings.TrimPrefix(path.Clean(joined), "/")
}

// RelationshipByID returns the relationship of source with the id, false
// when there is none.
func (p *Package) RelationshipByID(source, id string) (Relationship, bool) {
	rels, err := p.Relationships(source)
	if err != nil {
		return Relationship{}, false
	}
	for _, r := range rels {
		if r.ID == id {
			return r, true
		}
	}
	return Relationship{}, false
}

// RelatedPart returns the target of source's first internal relationship
// of type relType that the package holds, "" for none.
func (p *Package) RelatedPart(source, relType string) string {
	rels, err := p.Relationships(source)
	if err != nil {
		return ""
	}
	for _, r := range rels {
		if r.Type == relType && !r.External && p.Has(r.Target) {
			return r.Target
		}
	}
	return ""
}

// MainPart returns the package's main part, the target of its
// officeDocument relationship (ECMA-376 Part 1, 11.3.10), and fails with
// office.ErrMalformed when there is none.
func (p *Package) MainPart() (string, error) {
	rels, err := p.Relationships("")
	if err != nil {
		return "", err
	}
	for _, r := range rels {
		if r.Type == RelOfficeDocument && !r.External {
			if !p.Has(r.Target) {
				return "", fmt.Errorf("%w: main part %s missing", office.ErrMalformed, r.Target)
			}
			return r.Target, nil
		}
	}
	return "", fmt.Errorf("%w: no officeDocument relationship", office.ErrMalformed)
}

// Parts returns the names of the parts, sorted.
func (p *Package) Parts() []string {
	out := make([]string, 0, len(p.entries))
	for _, f := range p.entries {
		out = append(out, strings.TrimPrefix(strings.ReplaceAll(f.Name, `\`, "/"), "/"))
	}
	sort.Strings(out)
	return out
}

// IsMissing reports an error of [Package.Read] or [Package.ParseXML] for a
// part the package does not hold.
func IsMissing(err error) bool { return errors.Is(err, ErrNoPart) }
