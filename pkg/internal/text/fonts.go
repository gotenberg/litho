package text

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype/tables"
	"github.com/go-text/typesetting/shaping"

	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
	"github.com/gotenberg/litho/pkg/internal/typography/varfont"
)

// maxFontFileBytes bounds a file holding one font program, and the tables of
// one face of a collection, which [Face.Typeset] extracts into a program of
// its own. Anti-DoS guard, not spec-mandated: the largest font programs in
// use, pan-CJK families, weigh 20 to 30 MiB. It matches the bound pkg/pdf
// applies to the font files it indexes.
const maxFontFileBytes = 64 << 20

// maxFontCollectionBytes bounds the size of a TrueType or OpenType
// Collection [NewFonts] indexes. Anti-DoS guard, not spec-mandated: a
// collection holds several programs, each bounded by [maxFontFileBytes]; the
// pan-CJK collections operating systems ship weigh about 100 MiB. It matches
// the bound pkg/pdf applies to the collections it indexes.
const maxFontCollectionBytes = 256 << 20

// maxFontIndexBytes bounds the bytes one [NewFonts] call reads across all
// files: their table directories and the tables a footprint needs. Files
// past the budget are skipped with a Debug log. Anti-DoS guard, not
// spec-mandated: a desktop's whole font set, CJK included, needs well under
// 64 MiB of them.
const maxFontIndexBytes int64 = 256 << 20

// maxFontWalkEntries bounds the directory entries, font files or not, one
// [NewFonts] call visits across its directories. Anti-DoS guard, not
// spec-mandated: font directories hold a few thousand entries, and the
// bound keeps a directory that holds, or links to, a large tree from turning
// the index into a walk of the file system. It matches pkg/pdf's walk bound.
const maxFontWalkEntries = 1 << 18

// maxIndexFaces bounds the faces one [Fonts] indexes; faces past it are
// skipped with a Debug log. Anti-DoS guard, not spec-mandated: a
// distribution's full Noto set, CJK collections included, holds a few
// thousand faces, and every face costs its footprint, about a kilobyte.
const maxIndexFaces = 1 << 14

// maxLoadedFaces bounds the parsed fonts a [Fonts] keeps loaded; the least
// recently used one leaves first. Memory guard, not spec-mandated: a
// document uses a handful of faces (a body face, its bold and italic, a
// monospace face, a fallback or two), so a few dozen cover a mix of
// documents laid out concurrently, and a face that left is parsed again on
// its next use.
const maxLoadedFaces = 32

// maxLoadedFaceBytes bounds the table bytes of the fonts a [Fonts] keeps
// loaded, which their parsed form roughly weighs, at
// [limits.MaxLoadedFontBytes]; the least recently used
// one leaves first, but the font just loaded always stays. Memory guard, not
// spec-mandated: a Latin face weighs well under 1 MiB and a pan-CJK face
// about 20 MiB, so six CJK faces fit beside the Latin ones.
const maxLoadedFaceBytes int64 = limits.MaxLoadedFontBytes

// maxTypesetBytes bounds the programs [Face.Typeset] keeps for the faces
// of a [Fonts]; the least recently used one leaves first, but the program
// just read always stays. Memory guard, not spec-mandated: a Latin program
// weighs well under 1 MiB and a CJK one about 16 MiB, so a document's faces
// fit many times over. A program that left is read again on its next use,
// and a document that drew with it keeps it until it is written.
const maxTypesetBytes int64 = 128 << 20

// maxFallbackLoads bounds the faces one per-character fallback loads to
// check that their cmap really maps the character. Anti-DoS guard, not
// spec-mandated: the footprint already says which faces cover it, and a face
// whose cmap claims characters it maps to no glyph must not make a layout
// parse every indexed face.
const maxFallbackLoads = 8

// fontExtensions are the file extensions [NewFonts] reads. Type 1 programs
// (.pfb, .pfa) are left out: go-text parses sfnt programs only.
var fontExtensions = []string{".ttf", ".otf", ".ttc", ".otc"}

// Fonts is an index of font faces that matches a [Style] to a face and lays
// out text with them.
//
// The index keeps a small footprint per face: its names, aspect, vertical
// metrics and character coverage. A face's font program is parsed from its
// file on first use by [Fonts.Layout] and kept in a bounded
// least-recently-used cache, so the memory a Fonts holds grows with the
// faces documents use, not with the faces installed.
//
// Build one with [NewFonts]; the zero value is not usable. A Fonts is safe
// for concurrent use once [NewFonts] returns: the footprints are read-only,
// and the cache of parsed fonts sits behind a mutex that no file read holds.
type Fonts struct {
	logger   *slog.Logger
	open     openFunc
	faces    []*Face
	byFamily map[string][]*Face

	// base is the index an overlay [Fonts.WithFaces] returned shares, nil
	// for an index of its own; set holds the overlay's document faces.
	// The cache fields below are unused on an overlay: its faces load
	// through base.
	base *Fonts
	set  *FaceSet
	// inst holds the instances of the variable faces of the conversion an
	// overlay [Fonts.ForConversion] or [Fonts.WithFaces] returned serves,
	// nil for an index of its own.
	inst *instanceStore

	// maxLoaded and maxLoadedBytes are [maxLoadedFaces] and
	// [maxLoadedFaceBytes]; tests lower them.
	maxLoaded      int
	maxLoadedBytes int64

	// maxTypeset is [maxTypesetBytes]; tests lower it.
	maxTypeset int64

	// mu guards the font, loadErr, prev and next fields of every face and
	// the cache list and totals below, and the typesetFace, err, cprev and
	// cnext fields of every face and the typesetFace list.
	mu          sync.Mutex
	lruHead     *Face
	lruTail     *Face
	loaded      int
	loadedBytes int64

	typesetHead, typesetTail *Face
	typesetBytes             int64

	// kits holds the [shapingKit] of finished layout calls for later ones;
	// an overlay uses its base's.
	kits sync.Pool
}

// maxKitFaces bounds the go-text faces a [shapingKit] keeps between layout
// calls; a kit past it is emptied before it is handed on. Memory guard, not
// spec-mandated: a go-text face holds a cache entry a glyph, about 1 MiB
// for a CJK face, and a document draws with a handful of faces, so a kit
// keeps them all while it is not one of the rare documents falling back
// across dozens.
const maxKitFaces = 32

// shapingKit is the HarfBuzz shaper of a layout call and the go-text faces
// it shapes with. A go-text face caches glyph lookups and extents, one
// entry a glyph, and the shaper caches a HarfBuzz font and its shaping
// plans per parsed font, so building them for every call costs more than
// the shaping they serve: a finished call hands its kit on to a later one.
// The kit goes whole, since the shaper's fonts wrap the kit's own faces,
// and to one call at a time, since neither is safe for concurrent use.
// Their caches only memoise, so a kit shapes as a new one does.
type shapingKit struct {
	shaper shaping.HarfbuzzShaper
	faces  map[*Face]*font.Face
}

// takeKit returns a kit for one layout call: one a finished call handed
// back, or a new one.
func (f *Fonts) takeKit() *shapingKit {
	pool := &f.kits
	if f.base != nil {
		pool = &f.base.kits
	}
	if k, ok := pool.Get().(*shapingKit); ok {
		return k
	}
	return &shapingKit{faces: make(map[*Face]*font.Face)}
}

// releaseKit hands k back for a later call. The call that held it is
// over: nothing it returned holds a go-text face. The instances of
// variable faces the call drew leave the kit, which a later conversion may
// take: an instance belongs to its conversion.
func (f *Fonts) releaseKit(k *shapingKit) {
	if len(k.faces) > maxKitFaces {
		k.shaper = shaping.HarfbuzzShaper{}
		clear(k.faces)
	}
	for fc := range k.faces {
		if fc.base != nil {
			delete(k.faces, fc)
		}
	}
	pool := &f.kits
	if f.base != nil {
		pool = &f.base.kits
	}
	pool.Put(k)
}

// Face is one indexed font face: its footprint, and the font program to
// embed.
//
// A Face is safe for concurrent use. It belongs to the [Fonts] that indexed
// it and stays valid as long as that index.
type Face struct {
	src   *fontSource
	index int
	// dir is the offset of the face's table directory in its file.
	dir int64
	// order is the face's position in the index.
	order int

	family     string
	postScript string
	// fullName is the full font name (name ID 4), which a CSS local()
	// source names.
	fullName string
	// keys are the [familyKey] forms of the typographic family and the
	// family the name table records.
	keys   []string
	weight int
	slant  uint8
	width  int
	// ascent, descent and lineGap are in font units, descent positive below
	// the baseline.
	ascent, descent, lineGap float64
	// xHeight and capHeight are the OS/2 x-height and cap height in
	// font units, 0 when the face records none.
	xHeight, capHeight float64
	// typoAscent and typoDescent are the OS/2 typographic ascender and
	// descender in font units, descent positive below the baseline, 0
	// when the face has no OS/2 table.
	typoAscent, typoDescent float64
	upem                    float64
	// revision is the head table's fontRevision, a 16.16 fixed-point
	// number, which ranks faces of last resort (see [compareLastResort]).
	revision uint32
	coverage []runeRange
	// size is the sum of the face's table lengths.
	size int64

	// font, loadErr, prev and next are guarded by the index's mutex.
	font       *font.Font
	loadErr    error
	prev, next *Face

	// idx is the index the face belongs to, which bounds its typesetFace
	// program; nil for a document face, which keeps its program once read.
	idx *Fonts
	// once guards typesetFace and err for a document face; the index's mutex
	// guards them, cprev and cnext for an indexed one.
	once         sync.Once
	typesetFace  *typeset.Face
	err          error
	cprev, cnext *Face

	// pinned is the parsed font of a document face, loaded once by
	// [LoadFace] or [Fonts.LocalFace] and never evicted; doc holds its
	// @font-face descriptors. Both are nil for an indexed face.
	pinned *font.Font
	doc    *FaceDescriptor
	// features are the shaper's form of the features of doc, nil for
	// none.
	features []shaping.FontFeature

	// vf is the design space of a document face whose font is variable,
	// nil otherwise; vfErr says why a variable font's tables do not read.
	vf    *varfont.Font
	vfErr error
	// instMu guards instances, the faces of vf's instances by their
	// normalized coordinates, and instErr, why the font does not
	// instance.
	instMu    sync.Mutex
	instances map[string]*Face
	instErr   error
	// base is the face an instance face is an instance of, nil for any
	// other face; coords are the instance's normalized coordinates, which
	// the shaper is handed, and advances its glyph advances.
	base     *Face
	coords   []tables.Coord
	advances truetype.HMetrics

	// axes is the design space of a face of the index whose font is
	// variable, nil otherwise, read from its 'fvar' table alone;
	// namedInstances are the named instances of the face of its file. A
	// face standing for one named instance has its user coordinates in
	// named and the face of its file, through which it loads and embeds,
	// in varBase; both are nil for the face of the file itself.
	axes           *varfont.Axes
	namedInstances []namedInstance
	named          []float64
	varBase        *Face
}

// Metrics are the vertical metrics of a face at a size, in points.
type Metrics struct {
	// Ascent is the distance from the baseline up to the top of the line
	// box, positive.
	Ascent float64
	// Descent is the distance from the baseline down to the bottom of the
	// line box, positive.
	Descent float64
	// LineGap is the extra space the font asks for between lines.
	LineGap float64
	// XHeight is the height of a lowercase x above the baseline, from the
	// OS/2 table, half the size when the face records none.
	XHeight float64
	// CapHeight is the height of a capital letter above the baseline,
	// from the OS/2 table, 0 when the face records none.
	CapHeight float64
	// EmAscent and EmDescent split the em box, the font size, at the
	// baseline in the proportions of the OS/2 typographic ascender and
	// descender, else of Ascent and Descent: the box a ruby annotation
	// and an emphasis mark sit against, as Chromium's normalized typo
	// ascent and descent place them (CSS Ruby 1, 3.3; CSS Text Decoration
	// 3, 3.5).
	EmAscent, EmDescent float64
}

// NewFonts indexes the font faces in the files under dirs.
//
// Directories are walked in the order given, each in lexical order, and a
// file reached twice is read once. Symbolic links to directories are not
// followed. Files ending in .ttf, .otf, .ttc and .otc are read, every face
// of a collection included. Indexing reads only a face's table directory
// and its name, OS/2, head, hhea, maxp, cmap and loca tables, never its
// outlines or layout tables, and keeps no file bytes.
//
// A file that fails to read, a file larger than the per-file bounds, and a
// directory that cannot be read are skipped with a Debug log, and so is a
// face litho cannot use: one without glyf, CFF or CFF2 outlines, a colour
// bitmap face such as Noto Color Emoji, or one holding a construct go-text
// cannot parse safely. The order of the faces decides ties in
// [Fonts.Match], so the index is deterministic for a given set of files and
// directories.
//
// The files must stay in place while the index is used: [Fonts.Layout] and
// [Face.Typeset] read them again. NewFonts fails only when ctx ends. With
// no usable face it returns an index with no faces, on which [Fonts.Match]
// returns nil and [Fonts.Layout] returns [ErrNoFonts].
//
// A nil logger discards the logs. NewFonts writes no file and keeps no cache
// on disk.
func NewFonts(ctx context.Context, logger *slog.Logger, dirs []string) (*Fonts, error) {
	f := newIndex(logger, openOSFile)
	paths, err := walkFontDirs(ctx, f.logger, dirs)
	if err != nil {
		return nil, fmt.Errorf("walking font directories: %w", err)
	}
	budget := maxFontIndexBytes
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("indexing font files: %w", err)
		}
		f.indexFile(p, &budget)
	}
	return f, nil
}

// newIndex returns an empty index whose files open with open.
func newIndex(logger *slog.Logger, open openFunc) *Fonts {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Fonts{
		logger:         logger,
		open:           open,
		byFamily:       make(map[string][]*Face),
		maxLoaded:      maxLoadedFaces,
		maxLoadedBytes: maxLoadedFaceBytes,
		maxTypeset:     maxTypesetBytes,
	}
}

// walkFontDirs lists the font files under dirs, each directory in lexical
// order, without duplicates, within [maxFontWalkEntries].
func walkFontDirs(ctx context.Context, logger *slog.Logger, dirs []string) ([]string, error) {
	var paths []string
	seen := make(map[string]bool)
	entries := 0
	for _, dir := range dirs {
		root := filepath.Clean(dir)
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				logger.Debug("font files: skipping an unreadable entry", slog.String("path", p), slog.Any("error", err))
				if d == nil || d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			entries++
			if entries > maxFontWalkEntries {
				logger.Debug("font files: walk bound reached, ignoring the entries left", slog.Int("max", maxFontWalkEntries))
				return fs.SkipAll
			}
			if d.IsDir() || !isFontFileName(p) || seen[p] {
				return nil
			}
			seen[p] = true
			paths = append(paths, p)
			return nil
		})
		if err != nil {
			return nil, err
		}
		if entries > maxFontWalkEntries {
			break
		}
	}
	return paths, nil
}

// isFontFileName reports whether name carries one of [fontExtensions].
func isFontFileName(name string) bool {
	return slices.Contains(fontExtensions, strings.ToLower(filepath.Ext(name)))
}

// indexFile adds the faces of the file at path to the index, charging the
// bytes it reads to budget. A file that cannot be read is skipped with a
// Debug log.
func (f *Fonts) indexFile(path string, budget *int64) {
	file, size, err := f.open(path)
	if err != nil {
		f.logger.Debug("font files: skipping a file", slog.String("path", path), slog.Any("error", err))
		return
	}
	defer file.Close() // read-only file: Close reports nothing a read did not
	if size > maxFontCollectionBytes {
		f.logger.Debug("font files: skipping a file past the size bound", slog.String("path", path), slog.Int("max", maxFontCollectionBytes))
		return
	}
	limit := min(maxFootprintFileBytes, *budget)
	r := &boundedReader{r: file, left: limit}
	src := &fontSource{path: path, open: f.open}
	faces, err := readFootprints(f.logger, src, r, size)
	*budget -= limit - r.left
	if err != nil {
		f.logger.Debug("font files: skipping a file", slog.String("path", path), slog.Any("error", err))
		return
	}
	for _, fc := range faces {
		for _, nf := range append([]*Face{fc}, fc.namedFaces()...) {
			if len(f.faces) >= maxIndexFaces {
				f.logger.Debug("font files: face bound reached, ignoring the faces left", slog.Int("max", maxIndexFaces))
				return
			}
			f.add(nf)
		}
	}
}

// add appends fc to the index and to its families.
func (f *Fonts) add(fc *Face) {
	fc.order = len(f.faces)
	fc.idx = f
	f.faces = append(f.faces, fc)
	for _, k := range fc.keys {
		f.byFamily[k] = append(f.byFamily[k], fc)
	}
}

// Len returns the number of indexed faces.
func (f *Fonts) Len() int {
	return len(f.faces)
}

// Match returns the face that best matches style, ignoring its size,
// colour and spacing. It reads no font file.
//
// Families resolve as fontconfig resolves them. Each family of style is
// tried in order, by its family or typographic family name (name IDs 1 and
// 16, compared without regard to case or spaces), followed by its
// metric-compatible aliases (Arial by Liberation Sans, Arimo and Albany;
// Times New Roman by Liberation Serif and Tinos; Courier New by Liberation
// Mono and Cousine; Calibri by Carlito; Cambria by Caladea; and the reverse
// directions). A generic family stands for its preference list (see
// [Style.Families]); for a Chinese, Japanese or Korean [Style.Lang] the Noto
// CJK family of that language comes first. Then the sans-serif list is
// tried, and at last the first indexed face. [BrowserFamilies] turns a list
// into the one that resolves as Chromium does.
//
// Among the faces of a family, Match keeps the one closest in aspect, in the
// order of CSS Fonts Level 4, 5.2 (Matching font styles): a normal width
// first, then the slant (italic, then oblique, then normal for an italic
// request), then the nearest weight, with the preference for 400 and 500 (a
// 500 request takes a 400 face before a 700 one). Bold italic in a family
// of regular, bold and italic faces takes the italic face. A
// candidate must cover U+0020 SPACE.
//
// Match returns nil only when the index holds no face.
func (f *Fonts) Match(style Style) *Face {
	if len(f.faces) == 0 {
		return nil
	}
	if refs := f.familyRefs(style); refs != nil {
		return f.matchRefs(style, refs)
	}
	req := requestOf(style)
	for _, key := range matchFamilies(style) {
		if fc := f.bestInFamily(key, req, ' '); fc != nil {
			return fc
		}
	}
	return f.faces[0]
}

// Covers reports whether a face of family covers r by its character map,
// a face of one of its metric-compatible aliases too when aliases is set
// (see [Fonts.Match]). It reads no font file.
func (f *Fonts) Covers(family string, aliases bool, r rune) bool {
	key := familyKey(family)
	keys := []string{key}
	if aliases {
		keys = append(keys, metricAliases[key]...)
	}
	for _, k := range keys {
		for _, fc := range f.byFamily[k] {
			if fc.covers(r) {
				return true
			}
		}
	}
	return false
}

// Family returns the face's typographic family name (name ID 16), or its
// family name (name ID 1) when it records none.
func (fc *Face) Family() string {
	return fc.family
}

// DeclaredFamily returns the family a face answers to in a document: the
// family of its @font-face rule for a document face, [Face.Family] for an
// indexed one.
func (fc *Face) DeclaredFamily() string {
	if fc.doc != nil {
		return fc.doc.Family
	}
	return fc.family
}

// Key returns the face's identity: its file path, "#", and its index in the
// file, 0 for a file holding one program.
func (fc *Face) Key() string {
	return fc.src.path + "#" + strconv.Itoa(fc.index)
}

// Typeset returns the face as a [typeset.Face]: one sfnt program and
// [Face.Key] as its dedup key.
//
// The first call reads the program from the face's file, extracting a face
// of a collection into a program of its own, and the [typeset.Face] keeps
// it: the backend embeds it. The index keeps the programs of its faces in a
// cache bounded by bytes, least recently used out first, so calls return
// the same pointer while the program stays cached and a new one, equal but
// for its address, after it left; the backend embeds faces by [Face.Key],
// so runs drawn with either share one font. A document face keeps its
// program for its lifetime. The caller must not modify the program.
// [NewFonts] indexes only faces it can extract, so the error, returned
// again on every call, means the file changed or vanished since.
//
// ctx's error stops a call before it reads the file, and is never kept:
// the face reads again on the next call. The read itself is one read of
// at most the index's per-file bound.
func (fc *Face) Typeset(ctx context.Context) (*typeset.Face, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if fc.idx == nil {
		fc.once.Do(func() {
			fc.typesetFace, fc.err = fc.readTypeset()
		})
		return fc.typesetFace, fc.err
	}
	if fc.varBase != nil {
		return fc.varBase.Typeset(ctx)
	}
	return fc.idx.typesetOf(fc)
}

// readTypeset reads the face's program as a [typeset.Face].
func (fc *Face) readTypeset() (*typeset.Face, error) {
	program, err := fc.readProgram()
	if err != nil {
		return nil, fmt.Errorf("reading the program of face %s: %w", fc.Key(), err)
	}
	return &typeset.Face{Program: program, Key: fc.Key()}, nil
}

// typesetOf returns the typeset program of fc, a face of f, from the cache
// or read from its file. The file is read without holding f.mu.
func (f *Fonts) typesetOf(fc *Face) (*typeset.Face, error) {
	f.mu.Lock()
	if fc.err != nil || fc.typesetFace != nil {
		if fc.typesetFace != nil {
			f.touchTypesetLocked(fc)
		}
		cf, err := fc.typesetFace, fc.err
		f.mu.Unlock()
		return cf, err
	}
	f.mu.Unlock()

	cf, err := fc.readTypeset()

	f.mu.Lock()
	defer f.mu.Unlock()
	if fc.typesetFace != nil {
		// Another call read it meanwhile; both read the same bytes.
		f.touchTypesetLocked(fc)
		return fc.typesetFace, nil
	}
	if err != nil {
		if fc.err == nil {
			fc.err = err
		}
		return nil, fc.err
	}
	fc.typesetFace = cf
	fc.cprev, fc.cnext = nil, f.typesetHead
	if f.typesetHead != nil {
		f.typesetHead.cprev = fc
	}
	f.typesetHead = fc
	if f.typesetTail == nil {
		f.typesetTail = fc
	}
	f.typesetBytes += int64(len(cf.Program))
	for f.typesetTail != fc && f.typesetBytes > f.maxTypeset {
		old := f.typesetTail
		f.unlinkTypesetLocked(old)
		f.typesetBytes -= int64(len(old.typesetFace.Program))
		old.typesetFace = nil
	}
	return cf, nil
}

// touchTypesetLocked moves fc to the front of the typesetFace list. f.mu is
// held.
func (f *Fonts) touchTypesetLocked(fc *Face) {
	if f.typesetHead == fc {
		return
	}
	f.unlinkTypesetLocked(fc)
	fc.cprev, fc.cnext = nil, f.typesetHead
	if f.typesetHead != nil {
		f.typesetHead.cprev = fc
	}
	f.typesetHead = fc
	if f.typesetTail == nil {
		f.typesetTail = fc
	}
}

// unlinkTypesetLocked removes fc from the typesetFace list. f.mu is held.
func (f *Fonts) unlinkTypesetLocked(fc *Face) {
	if fc.cprev != nil {
		fc.cprev.cnext = fc.cnext
	} else {
		f.typesetHead = fc.cnext
	}
	if fc.cnext != nil {
		fc.cnext.cprev = fc.cprev
	} else {
		f.typesetTail = fc.cprev
	}
	fc.cprev, fc.cnext = nil, nil
}

// readProgram reads the face's program from its file.
func (fc *Face) readProgram() ([]byte, error) {
	file, size, err := fc.src.open(fc.src.path)
	if err != nil {
		return nil, err
	}
	defer file.Close() // read-only file: Close reports nothing a read did not
	if fc.src.collection {
		if size > maxFontCollectionBytes {
			return nil, fmt.Errorf("collection of %d bytes past the %d-byte bound", size, maxFontCollectionBytes)
		}
		return truetype.ExtractFace(file, size, fc.dir, maxFontFileBytes)
	}
	if size > maxFontFileBytes {
		return nil, fmt.Errorf("program of %d bytes past the %d-byte bound", size, maxFontFileBytes)
	}
	program := make([]byte, size)
	if _, err := file.ReadAt(program, 0); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return program, nil
}

// Covers reports whether the face's cmap maps r. It answers from the
// index and reads no file.
func (fc *Face) Covers(r rune) bool { return fc.covers(r) }

// Metrics returns the face's vertical metrics at size points: the
// ascender, descender and line gap HarfBuzz uses, from the OS/2 typographic
// metrics when the font sets USE_TYPO_METRICS and from hhea otherwise, and
// the OS/2 x-height and cap height. It answers from the index and reads no
// file.
func (fc *Face) Metrics(size float64) Metrics {
	size *= fc.doc.sizeAdjust()
	scale := size / fc.upem
	m := Metrics{
		Ascent:    fc.ascent * scale,
		Descent:   fc.descent * scale,
		LineGap:   fc.lineGap * scale,
		XHeight:   fc.xHeight * scale,
		CapHeight: fc.capHeight * scale,
	}
	if m.XHeight <= 0 {
		m.XHeight = size / 2
	}
	asc, desc := fc.typoAscent, fc.typoDescent
	if asc+desc <= 0 || asc < 0 || desc < 0 {
		asc, desc = fc.ascent, fc.descent
	}
	if asc+desc > 0 {
		m.EmAscent = size * asc / (asc + desc)
		m.EmDescent = size - m.EmAscent
	} else {
		m.EmAscent, m.EmDescent = 0.8*size, 0.2*size
	}
	fc.doc.override(&m, size)
	return m
}
