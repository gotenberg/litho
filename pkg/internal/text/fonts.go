package text

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/fontscan"
	"github.com/go-text/typesetting/language"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// maxFontFileBytes bounds the bytes [NewFonts] reads from a file holding one
// font program, and the size of one face extracted from a collection.
// Anti-DoS guard, not spec-mandated: the largest font programs in use,
// pan-CJK families, weigh 20 to 30 MiB. It matches the bound pkg/pdf applies
// to the font files it indexes.
const maxFontFileBytes = 64 << 20

// maxFontCollectionBytes bounds the bytes [NewFonts] reads from a TrueType or
// OpenType Collection. Anti-DoS guard, not spec-mandated: a collection holds
// several programs, each bounded by [maxFontFileBytes]; the pan-CJK
// collections operating systems ship weigh about 100 MiB. It matches the
// bound pkg/pdf applies to the collections it indexes.
const maxFontCollectionBytes = 256 << 20

// maxFontIndexBytes bounds the bytes one [NewFonts] call reads across all
// files. Files past the budget are skipped with a Debug log. Anti-DoS guard,
// not spec-mandated: the whole font set of a desktop distribution, CJK
// included, stays under 1 GiB, and an index holds every face it reads in
// memory.
const maxFontIndexBytes int64 = 1 << 30

// maxFontWalkEntries bounds the directory entries, font files or not, one
// [NewFonts] call visits across its directories. Anti-DoS guard, not
// spec-mandated: font directories hold a few thousand entries, and the
// bound keeps a directory that holds, or links to, a large tree from turning
// the index into a walk of the file system. It matches pkg/pdf's walk bound.
const maxFontWalkEntries = 1 << 18

// fontExtensions are the file extensions [NewFonts] reads. Type 1 programs
// (.pfb, .pfa) are left out: go-text parses sfnt programs only.
var fontExtensions = []string{".ttf", ".otf", ".ttc", ".otc"}

// Fonts is an index of font faces that matches a [Style] to a face and lays
// out text with them.
//
// Build one with [NewFonts]; the zero value is not usable. A Fonts is safe
// for concurrent use once [NewFonts] returns: its faces are read-only, and
// the go-text resolver it queries, which keeps per-query state, sits behind
// a mutex, so concurrent face resolutions serialize while shaping and line
// breaking run in parallel.
type Fonts struct {
	logger *slog.Logger
	faces  []*Face
	byFont map[*font.Font]*Face

	// mu guards fm and the query and script last set on it: go-text's
	// FontMap mutates its candidate cache on every query.
	mu       sync.Mutex
	fm       *fontscan.FontMap
	query    fontscan.Query
	hasQuery bool
	script   language.Script
}

// Face is one indexed font face: the sfnt program to embed and the metrics a
// layout needs.
//
// A Face is safe for concurrent use. It belongs to the [Fonts] that indexed
// it and stays valid as long as that index.
type Face struct {
	path   string
	index  int
	family string
	font   *font.Font
	// ascent, descent and lineGap are in font units, descent positive below
	// the baseline.
	ascent, descent, lineGap float64
	upem                     float64
	// data is the whole file. dir is the offset of the face's table
	// directory in a collection, -1 for a file holding one program.
	data []byte
	dir  int64

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
}

// NewFonts indexes the font faces in the files under dirs.
//
// Directories are walked in the order given, each in lexical order, and a
// file reached twice is read once. Symbolic links to directories are not
// followed. Files ending in .ttf, .otf, .ttc and .otc are read; a file that
// fails to read or parse, or that is larger than the per-file bound, is
// skipped with a Debug log, and so is a directory that cannot be read. The
// order of the faces decides ties in [Fonts.Match], so the index is
// deterministic for a given set of files and directories.
//
// NewFonts fails only when ctx ends. With no usable face it returns an
// index with no faces, on which [Fonts.Match] returns nil and [Fonts.Layout]
// returns [ErrNoFonts].
//
// A nil logger discards the logs. NewFonts writes no file: it never uses
// go-text's system font cache.
func NewFonts(ctx context.Context, logger *slog.Logger, dirs []string) (*Fonts, error) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	f := &Fonts{
		logger: logger,
		byFont: make(map[*font.Font]*Face),
		fm:     fontscan.NewFontMap(fontscanLogger{logger}),
	}
	paths, err := walkFontDirs(ctx, logger, dirs)
	if err != nil {
		return nil, fmt.Errorf("walking font directories: %w", err)
	}
	budget := maxFontIndexBytes
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("indexing font files: %w", err)
		}
		f.load(p, &budget)
	}
	return f, nil
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

// load reads and registers the faces of the file at path, charging its size
// to budget. A file that cannot be read is skipped with a Debug log.
func (f *Fonts) load(path string, budget *int64) {
	data, err := readFontFile(path, budget)
	if err != nil {
		f.logger.Debug("font files: skipping a file", slog.String("path", path), slog.Any("error", err))
		return
	}
	f.loadData(path, data)
}

// loadData registers the faces of data, the bytes of the file at path. Data
// that holds no usable face is skipped with a Debug log.
func (f *Fonts) loadData(path string, data []byte) {
	if truetype.IsCollection(data) {
		f.loadCollection(path, data)
		return
	}
	if len(data) > maxFontFileBytes {
		f.logger.Debug("font files: skipping a program past the size bound", slog.String("path", path), slog.Int("max", maxFontFileBytes))
		return
	}
	if err := checkGoTextSafe(data, 0); err != nil {
		f.logger.Debug("font files: skipping a file", slog.String("path", path), slog.Any("error", err))
		return
	}
	face, err := font.ParseTTF(bytes.NewReader(data))
	if err != nil {
		f.logger.Debug("font files: skipping an unparsable file", slog.String("path", path), slog.Any("error", err))
		return
	}
	f.register(path, 0, -1, data, face)
}

// loadCollection registers every face of the collection data read from path.
// The face count and table directory offsets come from litho's own bounded
// reader, so go-text never sees a collection litho would refuse.
func (f *Fonts) loadCollection(path string, data []byte) {
	r := bytes.NewReader(data)
	dirs, err := truetype.Faces(r, r.Size())
	if err != nil {
		f.logger.Debug("font files: skipping an unreadable collection", slog.String("path", path), slog.Any("error", err))
		return
	}
	for i, dir := range dirs {
		if err := checkGoTextSafe(data, dir); err != nil {
			f.logger.Debug("font files: skipping a collection", slog.String("path", path), slog.Int("face", i), slog.Any("error", err))
			return
		}
	}
	faces, err := font.ParseTTC(bytes.NewReader(data))
	if err != nil || len(faces) != len(dirs) {
		f.logger.Debug("font files: skipping an unparsable collection", slog.String("path", path), slog.Any("error", err))
		return
	}
	for i, face := range faces {
		// The program is extracted again, and kept, on the first
		// [Face.Compose]; extracting it here only proves that call succeeds,
		// so a face litho cannot embed never reaches a layout.
		if _, err := truetype.ExtractFace(r, r.Size(), dirs[i], maxFontFileBytes); err != nil {
			f.logger.Debug("font files: skipping a collection face", slog.String("path", path), slog.Int("face", i), slog.Any("error", err))
			continue
		}
		f.register(path, i, dirs[i], data, face)
	}
}

// register adds one parsed face to the index and to the go-text resolver.
func (f *Fonts) register(path string, index int, dir int64, data []byte, face *font.Face) {
	if index > int(^uint16(0)) {
		return
	}
	desc := face.Describe()
	upem := float64(face.Upem())
	if upem <= 0 {
		upem = 1000
	}
	fc := &Face{
		path:   path,
		index:  index,
		family: desc.Family,
		font:   face.Font,
		upem:   upem,
		data:   data,
		dir:    dir,
	}
	if ext, ok := face.FontHExtents(); ok {
		fc.ascent = float64(ext.Ascender)
		fc.descent = -float64(ext.Descender)
		fc.lineGap = float64(ext.LineGap)
	} else {
		fc.ascent, fc.descent = 0.8*upem, 0.2*upem
	}
	f.faces = append(f.faces, fc)
	f.byFont[face.Font] = fc
	f.fm.AddFace(face, font.FontID{File: path, Index: uint16(index)}, desc) //nolint:gosec // index fits uint16: checked against its maximum above
}

// readFontFile reads the file at path within the per-file bounds and the
// index budget.
func readFontFile(path string, budget *int64) ([]byte, error) {
	file, err := os.Open(path) //nolint:gosec // reading the font files the caller's directories hold is the purpose of the index
	if err != nil {
		return nil, err
	}
	defer file.Close() //nolint:errcheck // read-only file: Close reports nothing a read did not
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	size := info.Size()
	if size > maxFontCollectionBytes {
		return nil, fmt.Errorf("file of %d bytes past the %d-byte bound", size, maxFontCollectionBytes)
	}
	if size > *budget {
		return nil, fmt.Errorf("index budget of %d bytes spent", maxFontIndexBytes)
	}
	data, err := io.ReadAll(io.LimitReader(file, size))
	if err != nil {
		return nil, err
	}
	*budget -= int64(len(data))
	return data, nil
}

// Len returns the number of indexed faces.
func (f *Fonts) Len() int {
	return len(f.faces)
}

// Match returns the face that best matches style, ignoring its size,
// colour, spacing and language.
//
// It tries the families of style in order (see [Style.Families] for the
// generic families), then go-text's family substitutions, which map a
// family to its metric-compatible and similar families (Arial to Liberation
// Sans, for example) and end with go-text's sans-serif list, then every
// indexed face in index order. Among the faces of a family it keeps the
// closest weight and style, by the CSS Fonts 4 font matching rules
// (italic, then oblique, then normal). A candidate must cover U+0020 SPACE;
// when none does, Match returns the first indexed face.
//
// Match returns nil only when the index holds no face.
func (f *Fonts) Match(style Style) *Face {
	if len(f.faces) == 0 {
		return nil
	}
	q := queryFor(style)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.resolveLocked(q, language.Unknown, ' ')
}

// resolveLocked returns the face the go-text resolver picks for r under q
// and script, or the first indexed face when it picks none. f.mu is held.
func (f *Fonts) resolveLocked(q fontscan.Query, script language.Script, r rune) *Face {
	if !f.hasQuery || !sameQuery(f.query, q) {
		f.fm.SetQuery(q)
		f.query = q
		f.hasQuery = true
	}
	if script != f.script {
		f.fm.SetScript(script)
		f.script = script
	}
	if face := f.fm.ResolveFace(r); face != nil {
		if fc, ok := f.byFont[face.Font]; ok {
			return fc
		}
	}
	return f.faces[0]
}

// sameQuery reports whether a and b ask for the same families and aspect.
func sameQuery(a, b fontscan.Query) bool {
	return a.Aspect == b.Aspect && slices.Equal(a.Families, b.Families)
}

// Family returns the family name the face's name table records.
func (fc *Face) Family() string {
	return fc.family
}

// Key returns the face's identity: its file path, "#", and its index in the
// file, 0 for a file holding one program.
func (fc *Face) Key() string {
	return fc.path + "#" + strconv.Itoa(fc.index)
}

// Compose returns the face as a [compose.Face]: one sfnt program and
// [Face.Key] as its dedup key. A face of a collection is extracted into a
// program of its own on the first call.
//
// Every call returns the same pointer, so runs drawn with one face share one
// [compose.Face]. Program aliases the index's copy of the file: the caller
// must not modify it. [NewFonts] indexes only collection faces it can
// extract, so the error, returned again on every call, is not expected.
func (fc *Face) Compose() (*compose.Face, error) {
	fc.once.Do(func() {
		program := fc.data
		if fc.dir >= 0 {
			r := bytes.NewReader(fc.data)
			program, fc.err = truetype.ExtractFace(r, r.Size(), fc.dir, maxFontFileBytes)
			if fc.err != nil {
				fc.err = fmt.Errorf("extracting face %s: %w", fc.Key(), fc.err)
				return
			}
		}
		fc.composed = &compose.Face{Program: program, Key: fc.Key()}
	})
	return fc.composed, fc.err
}

// Metrics returns the face's vertical metrics at size points: the
// ascender, descender and line gap HarfBuzz uses, from the OS/2 typographic
// metrics when the font sets USE_TYPO_METRICS and from hhea otherwise.
func (fc *Face) Metrics(size float64) Metrics {
	scale := size / fc.upem
	return Metrics{
		Ascent:  fc.ascent * scale,
		Descent: fc.descent * scale,
		LineGap: fc.lineGap * scale,
	}
}

// fontscanLogger routes go-text's resolver messages to a [slog.Logger] at
// Debug.
type fontscanLogger struct {
	logger *slog.Logger
}

// Printf logs one resolver message at Debug.
func (l fontscanLogger) Printf(format string, args ...any) {
	if !l.logger.Enabled(context.Background(), slog.LevelDebug) {
		return
	}
	l.logger.Debug("font resolver: " + fmt.Sprintf(format, args...))
}
