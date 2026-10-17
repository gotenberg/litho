package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"path"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/remediation"
	"github.com/gotenberg/litho/pkg/internal/typography/cff"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// maxFontFileBytes bounds the size of a font program [WithFontFiles] and
// [WithSystemFonts] read: a file holding one program, or one face of a
// collection. Anti-DoS guard, not spec-mandated: the largest font programs in
// use, pan-CJK families, weigh 20 to 30 MiB, and a program past this bound in
// a font directory is no font a document embeds.
const maxFontFileBytes = 64 << 20

// maxFontCollectionBytes bounds the size of a TrueType or OpenType Collection
// the index reads faces from. Anti-DoS guard, not spec-mandated: a collection
// holds several programs, each bounded by [maxFontFileBytes], and only its
// header tables are read, so it may be larger; the largest collections an
// operating system ships, colour emoji in bitmaps, approach 200 MiB and hold
// no outlines a document embeds.
const maxFontCollectionBytes = 256 << 20

// maxFontIndexFiles bounds the font files one index reads. Anti-DoS guard,
// not spec-mandated: an operating system ships a few hundred font files, and
// a Linux distribution with every Noto package installed a few thousand, so a
// directory holding more is no font directory.
const maxFontIndexFiles = 16384

// maxFontWalkEntries bounds the directory entries, font files or not, the
// walk of one index visits. Anti-DoS guard, not spec-mandated: it keeps a
// font directory that holds, or links to, a large tree from turning the first
// font request into a walk of the file system; font directories hold a few
// thousand entries.
const maxFontWalkEntries = 1 << 18

// maxFontIndexBytes bounds the bytes one index reads: the header tables of
// every face, and the whole of each bare CFF program. Anti-DoS guard, not
// spec-mandated: a face's header tables weigh a few kilobytes, so a thousand
// faces read under 16 MiB.
const maxFontIndexBytes = 64 << 20

// fontFiles is the [FontProvider] of [WithFontFiles] and [WithSystemFonts].
// Its index is built once, under mu, and read-only after, so concurrent
// remediations share it.
type fontFiles struct {
	// list returns the font files to index, in the order a tie between
	// matches is broken, as keys open takes.
	list func(ctx context.Context, logger *slog.Logger) ([]string, error)
	open func(name string) (fs.File, error)
	// lenient skips a file it cannot read, with a Debug log, where a strict
	// index aborts the remediation. The operating system's font directories
	// hold files a process may not read, which are no reason to fail.
	lenient bool

	mu      sync.Mutex
	indexed bool
	files   []fontFile
	err     error
}

// fontFile is one indexed program.
type fontFile struct {
	path string
	// face is the offset of the face's table directory in a collection, 0
	// for a file holding one program.
	face   int64
	psName string
	family string
	bold   bool
	italic bool
	// weight is the OS/2 usWeightClass, 0 when the program states none.
	weight int
	// cffOutlines reports a program with CFF outlines, which a Type 1 font
	// takes without becoming a TrueType font.
	cffOutlines bool
}

// newFontFiles returns the provider of [WithFontFiles] over fsys.
func newFontFiles(fsys fs.FS) *fontFiles {
	return &fontFiles{
		list: func(ctx context.Context, logger *slog.Logger) ([]string, error) {
			return walkFontFS(ctx, logger, fsys)
		},
		open: fsys.Open,
	}
}

// FontProgram returns the program of the best indexed match for req.
func (f *fontFiles) FontProgram(ctx context.Context, req FontRequest) ([]byte, error) {
	return f.program(ctx, nil, req)
}

// program returns the program of the best indexed match for req, logging to
// logger, which may be nil, what it skips.
func (f *fontFiles) program(ctx context.Context, logger *slog.Logger, req FontRequest) ([]byte, error) {
	logger = orDiscard(logger)
	files, err := f.index(ctx, logger)
	if err != nil {
		return nil, err
	}
	best := matchFontFile(files, req)
	if best == nil {
		return nil, nil
	}
	return f.load(logger, best)
}

// candidates returns a reader for the program of every indexed match for req,
// best first ([matchFontFiles]), logging to logger what it skips. Each reads
// its program when the remediation reaches it.
func (f *fontFiles) candidates(ctx context.Context, logger *slog.Logger, req FontRequest) ([]remediation.FontCandidate, error) {
	logger = orDiscard(logger)
	files, err := f.index(ctx, logger)
	if err != nil {
		return nil, err
	}
	ranked := matchFontFiles(files, req)
	out := make([]remediation.FontCandidate, len(ranked))
	for i, file := range ranked {
		out[i] = func(context.Context) ([]byte, error) { return f.load(logger, file) }
	}
	return out, nil
}

// load reads the program of file, or returns nil for a file it skips: one
// past [maxFontFileBytes], one no reader accepts, and, for a lenient index,
// one it cannot read.
func (f *fontFiles) load(logger *slog.Logger, file *fontFile) ([]byte, error) {
	data, err := f.read(file)
	switch {
	case err == nil:
		return data, nil
	case errors.Is(err, errFontFileTooLarge), isFontFormatError(err):
		logger.Debug("font files: skipping a program", slog.String("file", file.path), slog.Any("err", err))
		return nil, nil
	case f.lenient:
		logger.Debug("font files: skipping an unreadable file", slog.String("file", file.path), slog.Any("err", err))
		return nil, nil
	}
	return nil, fmt.Errorf("reading font file %s: %w", file.path, err)
}

// read returns the program file names: the whole file, or the face of a
// collection as a program of its own.
func (f *fontFiles) read(file *fontFile) ([]byte, error) {
	fh, err := f.open(file.path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	if file.face == 0 {
		data, err := io.ReadAll(io.LimitReader(fh, maxFontFileBytes+1))
		if err != nil {
			return nil, err
		}
		if len(data) > maxFontFileBytes {
			return nil, errFontFileTooLarge
		}
		return data, nil
	}
	r, size, _, err := readerAt(fh, maxFontCollectionBytes)
	if err != nil {
		return nil, err
	}
	data, err := truetype.ExtractFace(r, size, file.face, maxFontFileBytes)
	if err != nil {
		return nil, fmt.Errorf("extracting the face at %d: %w", file.face, err)
	}
	return data, nil
}

// index returns the index, building it at the first call. A walk the caller's
// ctx stopped is not kept, so the next remediation walks again.
func (f *fontFiles) index(ctx context.Context, logger *slog.Logger) ([]fontFile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.indexed {
		files, err := f.build(ctx, logger)
		if err != nil && ctx.Err() != nil {
			return nil, err
		}
		f.files, f.err, f.indexed = files, err, true
	}
	return f.files, f.err
}

// build lists the font files and reads the names and styles of their faces,
// in list order, within [maxFontIndexFiles] and [maxFontIndexBytes].
func (f *fontFiles) build(ctx context.Context, logger *slog.Logger) ([]fontFile, error) {
	paths, err := f.list(ctx, logger)
	if err != nil {
		return nil, fmt.Errorf("indexing font files: %w", err)
	}
	if len(paths) > maxFontIndexFiles {
		logger.Debug("font files: indexing the first files only", slog.Int("files", len(paths)), slog.Int("max", maxFontIndexFiles))
		paths = paths[:maxFontIndexFiles]
	}
	budget := int64(maxFontIndexBytes)
	var files []fontFile
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("indexing font files: %w", err)
		}
		faces, err := f.describe(p, &budget)
		switch {
		case err == nil:
			files = append(files, faces...)
		case errors.Is(err, errFontIndexBudget):
			logger.Debug("font files: index budget spent, ignoring the files left", slog.String("file", p), slog.Int("max_bytes", maxFontIndexBytes))
			return files, nil
		case errors.Is(err, errFontFileTooLarge), isFontFormatError(err):
			logger.Debug("font files: skipping a file", slog.String("file", p), slog.Any("err", err))
		case f.lenient:
			logger.Debug("font files: skipping an unreadable file", slog.String("file", p), slog.Any("err", err))
		default:
			return nil, fmt.Errorf("indexing font files: reading %s: %w", p, err)
		}
	}
	return files, nil
}

// describe reads the names and styles of the faces the file at p holds,
// charging what it reads to budget.
func (f *fontFiles) describe(p string, budget *int64) ([]fontFile, error) {
	fh, err := f.open(p)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	if strings.HasSuffix(strings.ToLower(p), ".cff") {
		return describeCFF(p, fh, budget)
	}
	r, size, whole, err := readerAt(fh, maxFontCollectionBytes)
	if err != nil {
		return nil, err
	}
	if whole {
		// The file was read whole, which the budget pays for at once.
		if size > *budget {
			return nil, errFontIndexBudget
		}
		*budget -= size
	} else {
		r = &budgetReaderAt{r: r, left: budget}
	}
	dirs, err := truetype.Faces(r, size)
	if err != nil {
		return nil, err
	}
	if len(dirs) == 1 && dirs[0] == 0 && size > maxFontFileBytes {
		return nil, errFontFileTooLarge
	}
	var faces []fontFile
	for _, dir := range dirs {
		info, err := truetype.ReadInfoAt(r, size, dir)
		if errors.Is(err, errFontIndexBudget) {
			return nil, err
		}
		if err != nil {
			if isFontFormatError(err) && len(dirs) > 1 {
				// One broken face leaves the collection's others usable.
				continue
			}
			return nil, err
		}
		if info.Outline == truetype.OutlineNone {
			continue
		}
		sub := strings.ToLower(info.Subfamily)
		faces = append(faces, fontFile{
			path:        p,
			face:        dir,
			psName:      normalizeFontName(info.PostScriptName),
			family:      withWidth(normalizeFontName(info.Family), parseFontStyle(sub).width),
			bold:        strings.Contains(sub, "bold") || info.MacStyle&1 != 0,
			italic:      strings.Contains(sub, "italic") || strings.Contains(sub, "oblique") || info.MacStyle&2 != 0,
			weight:      int(info.WeightClass),
			cffOutlines: info.Outline == truetype.OutlineCFF,
		})
	}
	return faces, nil
}

// describeCFF reads the name and style of the bare CFF program fh holds,
// which it reads whole, as CFF places its Name INDEX after a header of any
// length and its Top DICT after that.
func describeCFF(p string, fh fs.File, budget *int64) ([]fontFile, error) {
	data, err := io.ReadAll(io.LimitReader(fh, maxFontFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFontFileBytes {
		return nil, errFontFileTooLarge
	}
	if int64(len(data)) > *budget {
		return nil, errFontIndexBudget
	}
	*budget -= int64(len(data))
	info, err := cff.ReadInfo(data)
	if err != nil || info.CID {
		return nil, nil //nolint:nilerr // An unreadable or CID-keyed CFF program names no face to index.
	}
	// A bare CFF program states its style in its name alone.
	family, style := splitFontName(info.FontName)
	return []fontFile{{path: p, psName: normalizeFontName(info.FontName), family: family, bold: style.bold, italic: style.italic, weight: style.weight, cffOutlines: true}}, nil
}

// readerAt returns random access to fh and its size: fh itself when it
// supports it, as an [os.File] does, and otherwise its bytes, read whole,
// which whole reports. A file past limit is refused.
func readerAt(fh fs.File, limit int64) (r io.ReaderAt, size int64, whole bool, err error) {
	st, err := fh.Stat()
	if err != nil {
		return nil, 0, false, err
	}
	if st.Size() > limit {
		return nil, 0, false, errFontFileTooLarge
	}
	if r, ok := fh.(io.ReaderAt); ok {
		return r, st.Size(), false, nil
	}
	data, err := io.ReadAll(io.LimitReader(fh, limit+1))
	if err != nil {
		return nil, 0, false, err
	}
	if int64(len(data)) > limit {
		return nil, 0, false, errFontFileTooLarge
	}
	return bytes.NewReader(data), int64(len(data)), true, nil
}

// budgetReaderAt charges every read to a byte budget shared by the files of
// one index, and fails a read past it with [errFontIndexBudget].
type budgetReaderAt struct {
	r    io.ReaderAt
	left *int64
}

// ReadAt reads from the underlying reader when the budget allows.
func (b *budgetReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if int64(len(p)) > *b.left {
		return 0, errFontIndexBudget
	}
	*b.left -= int64(len(p))
	return b.r.ReadAt(p, off)
}

// isFontFormatError reports an error of a file that holds no program the
// readers accept, which the index skips rather than fails on.
func isFontFormatError(err error) bool {
	return errors.Is(err, truetype.ErrInvalidFont) || errors.Is(err, truetype.ErrTableNotFound)
}

// fontExtensions are the file extensions the index reads.
var fontExtensions = []string{".ttf", ".otf", ".ttc", ".otc", ".cff"}

// isFontFileName reports whether name carries one of [fontExtensions].
func isFontFileName(name string) bool {
	ext := strings.ToLower(path.Ext(name))
	for _, e := range fontExtensions {
		if ext == e {
			return true
		}
	}
	return false
}

// walkFontFS lists the font files under the root of fsys, in path order,
// within [maxFontWalkEntries]. [fs.WalkDir] does not descend a symbolic link
// to a directory, so no link loop can hold the walk.
func walkFontFS(ctx context.Context, logger *slog.Logger, fsys fs.FS) ([]string, error) {
	var paths []string
	entries := 0
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		entries++
		if entries > maxFontWalkEntries {
			logger.Debug("font files: walk bound reached, ignoring the entries left", slog.Int("max", maxFontWalkEntries))
			return fs.SkipAll
		}
		if !d.IsDir() && isFontFileName(p) {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return paths, nil
}

// orDiscard returns logger, or a logger that drops every record when it is
// nil.
func orDiscard(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.New(slog.DiscardHandler)
	}
	return logger
}
