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

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// maxFontFileBytes bounds a file holding one font program, and the tables of
// one face of a collection, which [Face.Compose] extracts into a program of
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
// loaded, which their parsed form roughly weighs; the least recently used
// one leaves first, but the font just loaded always stays. Memory guard, not
// spec-mandated: a Latin face weighs well under 1 MiB and a pan-CJK face
// about 20 MiB, so six CJK faces fit beside the Latin ones.
const maxLoadedFaceBytes int64 = 128 << 20

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

	// maxLoaded and maxLoadedBytes are [maxLoadedFaces] and
	// [maxLoadedFaceBytes]; tests lower them.
	maxLoaded      int
	maxLoadedBytes int64

	// mu guards the font, loadErr, prev and next fields of every face and
	// the cache list and totals below.
	mu          sync.Mutex
	lruHead     *Face
	lruTail     *Face
	loaded      int
	loadedBytes int64
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
	// keys are the [familyKey] forms of the typographic family and the
	// family the name table records.
	keys   []string
	weight int
	slant  uint8
	width  int
	// ascent, descent and lineGap are in font units, descent positive below
	// the baseline.
	ascent, descent, lineGap float64
	// xHeight is the OS/2 x-height in font units, 0 when the face records
	// none.
	xHeight  float64
	upem     float64
	coverage []runeRange
	// size is the sum of the face's table lengths.
	size int64

	// font, loadErr, prev and next are guarded by the index's mutex.
	font       *font.Font
	loadErr    error
	prev, next *Face

	once     sync.Once
	composed *compose.Face
	err      error
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
// [Face.Compose] read them again. NewFonts fails only when ctx ends. With
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
	defer file.Close() //nolint:errcheck // read-only file: Close reports nothing a read did not
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
		if len(f.faces) >= maxIndexFaces {
			f.logger.Debug("font files: face bound reached, ignoring the faces left", slog.Int("max", maxIndexFaces))
			return
		}
		f.add(fc)
	}
}

// add appends fc to the index and to its families.
func (f *Fonts) add(fc *Face) {
	fc.order = len(f.faces)
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
// tried, and at last the first indexed face.
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
	req := requestOf(style)
	for _, key := range matchFamilies(style) {
		if fc := f.bestInFamily(key, req, ' '); fc != nil {
			return fc
		}
	}
	return f.faces[0]
}

// Family returns the face's typographic family name (name ID 16), or its
// family name (name ID 1) when it records none.
func (fc *Face) Family() string {
	return fc.family
}

// Key returns the face's identity: its file path, "#", and its index in the
// file, 0 for a file holding one program.
func (fc *Face) Key() string {
	return fc.src.path + "#" + strconv.Itoa(fc.index)
}

// Compose returns the face as a [compose.Face]: one sfnt program and
// [Face.Key] as its dedup key.
//
// The first call reads the program from the face's file, extracting a face
// of a collection into a program of its own, and the [compose.Face] keeps
// it: the backend embeds it. Every call returns the same pointer, so runs
// drawn with one face share one [compose.Face]. The caller must not modify
// the program. [NewFonts] indexes only faces it can extract, so the error,
// returned again on every call, means the file changed or vanished since.
func (fc *Face) Compose() (*compose.Face, error) {
	fc.once.Do(func() {
		program, err := fc.readProgram()
		if err != nil {
			fc.err = fmt.Errorf("reading the program of face %s: %w", fc.Key(), err)
			return
		}
		fc.composed = &compose.Face{Program: program, Key: fc.Key()}
	})
	return fc.composed, fc.err
}

// readProgram reads the face's program from its file.
func (fc *Face) readProgram() ([]byte, error) {
	file, size, err := fc.src.open(fc.src.path)
	if err != nil {
		return nil, err
	}
	defer file.Close() //nolint:errcheck // read-only file: Close reports nothing a read did not
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

// Metrics returns the face's vertical metrics at size points: the
// ascender, descender and line gap HarfBuzz uses, from the OS/2 typographic
// metrics when the font sets USE_TYPO_METRICS and from hhea otherwise, and
// the OS/2 x-height. It
// answers from the index and reads no file.
func (fc *Face) Metrics(size float64) Metrics {
	scale := size / fc.upem
	m := Metrics{
		Ascent:  fc.ascent * scale,
		Descent: fc.descent * scale,
		LineGap: fc.lineGap * scale,
		XHeight: fc.xHeight * scale,
	}
	if m.XHeight <= 0 {
		m.XHeight = size / 2
	}
	return m
}
