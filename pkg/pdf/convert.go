package pdf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/plaintext"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// Format names an input format [Convert] reads.
type Format string

// The formats [Convert] reads.
const (
	// FormatText is plain text in UTF-8. See [Convert] for how it is laid
	// out.
	FormatText Format = "text"
)

// ConvertOption tunes [Convert].
type ConvertOption func(*convertOptions)

// convertOptions collects what the [ConvertOption] values set.
type convertOptions struct {
	logger                               *slog.Logger
	fontDirs                             []string
	width, height                        float64
	marginTop, marginRight, marginBottom float64
	marginLeft                           float64
	families                             []string
	fontSize                             float64
	limits                               Limits
}

// WithConvertLogger injects a structured logger for the diagnostics of a
// [Convert] call: font files skipped while indexing, a font program embedded
// whole, a recovered panic. A nil logger discards them, the default.
func WithConvertLogger(logger *slog.Logger) ConvertOption {
	return func(o *convertOptions) {
		o.logger = logger
	}
}

// WithConvertFontDirs sets the directories [Convert] reads fonts from,
// searched recursively for .ttf, .otf, .ttc and .otc files, in the order
// given: the order decides between faces that match equally. Without it, or
// with no directory, Convert reads the operating system's font directories,
// the ones [WithSystemFonts] reads, and its output then depends on the fonts
// installed where litho runs.
//
// The faces of a list of directories are indexed once per process and kept
// for later calls with the same list, so a font added to a directory after
// its first conversion is not seen. A few lists are kept at a time; a call
// with a new list past them drops the oldest. Indexing the system directories
// of a desktop takes seconds, and the index holds every font file it read in
// memory, up to 1 GiB of them, with their parsed tables beside: a desktop of
// several hundred fonts costs gigabytes. A directory of the few families a
// service needs costs a fraction of that.
func WithConvertFontDirs(dirs ...string) ConvertOption {
	return func(o *convertOptions) {
		o.fontDirs = slices.Clone(dirs)
	}
}

// WithPaperSize sets the page size in points, width then height. The default
// is A4, 595.276 by 841.890 points. Each side must be finite, positive and at
// most 14,400 points (ISO 32000-1, Annex C.2), or [Convert] fails with
// [ErrInvalidConvertOptions]. Both zero keep the default.
func WithPaperSize(width, height float64) ConvertOption {
	return func(o *convertOptions) {
		o.width, o.height = width, height
	}
}

// WithMargins sets the page margins in points. The default is 56.693 points,
// 2 cm, on every side; all four zero keep it. A margin must be finite and
// zero or more, and the margins must leave room on the page, or [Convert]
// fails with [ErrInvalidConvertOptions].
func WithMargins(top, right, bottom, left float64) ConvertOption {
	return func(o *convertOptions) {
		o.marginTop, o.marginRight, o.marginBottom, o.marginLeft = top, right, bottom, left
	}
}

// WithFontFamilies sets the font families asked for, in preference order,
// matched without regard to case or spaces. The generic families serif,
// sans-serif, monospace, cursive, fantasy and system-ui expand to common
// families of their kind. The default is Liberation Serif, the family a word
// processor opens a text file in, then serif. A character the chosen face
// lacks is drawn from another face that covers it.
func WithFontFamilies(families ...string) ConvertOption {
	return func(o *convertOptions) {
		o.families = slices.Clone(families)
	}
}

// WithFontSize sets the font size in points, 12 by default. It must be
// finite, positive and at most 1000, or [Convert] fails with
// [ErrInvalidConvertOptions]. Lines sit 1.15 times the size apart.
func WithFontSize(size float64) ConvertOption {
	return func(o *convertOptions) {
		o.fontSize = size
	}
}

// WithConvertLimits sets the resource budgets of a [Convert] call. It reads
// [Limits.MaxConvertInputBytes] and [Limits.MaxConvertPages]; a zero field
// keeps its default, and a field out of range makes Convert fail with
// [ErrInvalidLimits] before it reads the source.
func WithConvertLimits(l Limits) ConvertOption {
	return func(o *convertOptions) {
		o.limits = l
	}
}

// ConvertReport is what [Convert] reports beside the PDF.
type ConvertReport struct {
	// Pages is the number of pages written.
	Pages int
	// Warnings lists what the conversion could not render as the source
	// meant it, in the order met.
	Warnings []ConvertWarning
}

// ConvertWarning is one construct of the source [Convert] did not render
// faithfully.
type ConvertWarning struct {
	// Code is a stable identifier in kebab-case. "missing-glyph" flags a
	// character no font covers, drawn as a placeholder glyph, reported once
	// per character. "line-split" flags a source line of more than 65,536
	// characters, laid out in pieces that each start a new line.
	Code string
	// Message explains the construct and what was done instead.
	Message string
	// Page is the 1-based page the construct lands on, 0 when it has none.
	Page int
}

// Convert reads src in the given format, lays it out and writes the PDF to
// dst.
//
// # Plain text
//
// [FormatText] reads UTF-8. A leading byte order mark is dropped, an invalid
// byte reads as U+FFFD, and CR LF and a lone CR end a line as LF does.
//
//   - Each source line is a paragraph, wrapped at the width between the
//     margins at the line break opportunities of UAX #14, a word wider than
//     the line at a character boundary. Spaces are kept. There is no
//     hyphenation and no justification.
//   - Paragraph direction follows the first strong character (UAX #9), so
//     right-to-left text shapes and orders right to left; within a line, a
//     run of the opposite direction is reversed as a block, and deeper
//     embedding levels are not reordered. Every line starts at the left
//     margin.
//   - A tab moves to the next multiple of 8 characters from the start of the
//     line. An empty line takes one line height. A form feed starts a new
//     page; form feeds no text follows start none.
//   - Lines sit 1.15 times the font size apart and flow onto the next page
//     when the page is full, a paragraph included: there is no widow or
//     orphan control.
//
// The output is a tagged PDF 1.7 file (ISO 32000-1, 14.8): a Document
// element with one P element for each source line that draws text. Its
// language is left unset. The fonts are embedded, each subset to the glyphs
// drawn. Nothing is written for PDF/A or PDF/UA conformance; remediate the
// output for that.
//
// # Errors
//
// Convert returns an error wrapping [ErrUnsupportedFormat] for a format it
// does not read, [ErrInvalidLimits] or [ErrInvalidConvertOptions] for an
// option out of range, [ErrConvertInputTooLarge] past
// [Limits.MaxConvertInputBytes], [ErrConvertTooManyPages] past
// [Limits.MaxConvertPages], [ErrConvertNoFonts] when the font directories
// hold no usable font, [ErrInternalPanic] for a recovered panic, and ctx's
// error when ctx ends first. On an error other than a write error on dst,
// nothing reaches dst: the whole file is built before it is written.
//
// # Ownership and concurrency
//
// Convert reads src to its end and writes dst; it closes neither. It is safe
// to call concurrently: calls share only the read-only font index.
//
// Identical source bytes, options and font files write identical bytes.
func Convert(ctx context.Context, src io.Reader, format Format, dst io.Writer, opts ...ConvertOption) (_ *ConvertReport, err error) {
	var o convertOptions
	for _, opt := range opts {
		opt(&o)
	}
	defer recoverToError(o.logger, "Convert", &err)

	if format != FormatText {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedFormat, format)
	}
	if src == nil {
		return nil, fmt.Errorf("converting: %w", ErrNilSource)
	}
	if dst == nil {
		return nil, fmt.Errorf("%w: nil destination", ErrInvalidConvertOptions)
	}
	if err := o.limits.Validate(); err != nil {
		return nil, err
	}
	budget := o.limits.internal().WithDefaults()

	dirs := o.fontDirs
	if len(dirs) == 0 {
		dirs = defaultConvertFontDirs()
	}
	fonts, err := convertFonts(ctx, o.logger, dirs)
	if err != nil {
		return nil, fmt.Errorf("indexing fonts: %w", err)
	}

	doc, err := plaintext.Convert(ctx, src, fonts, plaintext.Options{
		PageWidth:     o.width,
		PageHeight:    o.height,
		MarginTop:     o.marginTop,
		MarginRight:   o.marginRight,
		MarginBottom:  o.marginBottom,
		MarginLeft:    o.marginLeft,
		FontFamilies:  o.families,
		FontSize:      o.fontSize,
		MaxInputBytes: budget.MaxConvertInputBytes,
		MaxPages:      budget.MaxConvertPages,
	})
	if err != nil {
		return nil, fmt.Errorf("converting text: %w", err)
	}
	if err := compose.Write(ctx, doc, dst, compose.Options{Logger: o.logger}); err != nil {
		return nil, fmt.Errorf("writing the PDF: %w", err)
	}

	report := &ConvertReport{Pages: len(doc.Pages)}
	for _, w := range doc.Warnings {
		report.Warnings = append(report.Warnings, ConvertWarning{Code: w.Code, Message: w.Message, Page: w.Page})
	}
	return report, nil
}

// defaultConvertFontDirs returns the operating system's font directories,
// the ones [WithSystemFonts] reads.
func defaultConvertFontDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		// No home directory leaves the per-user directories out; the
		// system-wide ones still serve.
		home = ""
	}
	return systemFontDirs(runtime.GOOS, os.Getenv, home)
}

// maxConvertFontIndexes bounds the font indexes [convertFonts] keeps, one per
// list of directories. Anti-DoS guard, not spec-mandated: an index holds the
// font files it read, up to 1 GiB, so a caller cycling through directory
// lists must not pile them up. A service converts with one list, the system
// directories or its own, and rarely with more than two.
const maxConvertFontIndexes = 4

// errFontIndexAbandoned stands for an index whose build stopped without a
// result, a panic recovered further up: callers waiting on it build again.
var errFontIndexAbandoned = errors.New("font index build abandoned")

// convertFontIndex is one cached index, ready once its channel closes.
type convertFontIndex struct {
	ready chan struct{}
	fonts *text.Fonts
	err   error
}

// convertFontCache holds the font indexes of [Convert], keyed by directory
// list. Indexing a desktop's font directories takes seconds, which every
// call would otherwise pay again.
//
// The key keeps the order of the list rather than sorting it, because the
// order decides between faces that match equally: a per-user directory
// listed first wins over a system one, and two orders of one set are two
// indexes. Entries leave oldest first past [maxConvertFontIndexes].
var convertFontCache = struct {
	sync.RWMutex
	entries map[string]*convertFontIndex
	// order lists the keys oldest first.
	order []string
}{entries: make(map[string]*convertFontIndex)}

// convertFontKey returns the cache key of dirs: each cleaned, in order.
func convertFontKey(dirs []string) string {
	clean := make([]string, len(dirs))
	for i, d := range dirs {
		clean[i] = filepath.Clean(d)
	}
	return strings.Join(clean, "\x00")
}

// convertFonts returns the font index of dirs, built once and shared by every
// later call with the same list. Concurrent first calls build it once: the
// others wait for it, or for their ctx. A build that fails, which only ctx
// ending does, is not kept.
func convertFonts(ctx context.Context, logger *slog.Logger, dirs []string) (*text.Fonts, error) {
	key := convertFontKey(dirs)
	for {
		e, build := claimConvertFontIndex(key)
		if build {
			return buildConvertFontIndex(ctx, logger, key, dirs, e)
		}
		select {
		case <-e.ready:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if e.err == nil {
			return e.fonts, nil
		}
		// The build failed for its own caller's ctx, or was abandoned, and
		// left the cache; this call's ctx decides whether to try again.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
}

// claimConvertFontIndex returns the entry of key and whether the caller must
// build it, which it must when the entry is new.
func claimConvertFontIndex(key string) (*convertFontIndex, bool) {
	convertFontCache.RLock()
	e := convertFontCache.entries[key]
	convertFontCache.RUnlock()
	if e != nil {
		return e, false
	}
	convertFontCache.Lock()
	defer convertFontCache.Unlock()
	if e := convertFontCache.entries[key]; e != nil {
		return e, false
	}
	e = &convertFontIndex{ready: make(chan struct{})}
	convertFontCache.entries[key] = e
	convertFontCache.order = append(convertFontCache.order, key)
	for len(convertFontCache.order) > maxConvertFontIndexes {
		delete(convertFontCache.entries, convertFontCache.order[0])
		convertFontCache.order = convertFontCache.order[1:]
	}
	return e, true
}

// buildConvertFontIndex indexes dirs into e, the entry of key, and releases
// the calls waiting on it. A failed or abandoned build leaves the cache, so
// the next call builds again.
func buildConvertFontIndex(ctx context.Context, logger *slog.Logger, key string, dirs []string, e *convertFontIndex) (_ *text.Fonts, err error) {
	e.err = errFontIndexAbandoned
	defer func() {
		if e.err != nil {
			dropConvertFontIndex(key, e)
		}
		close(e.ready)
	}()
	fonts, err := text.NewFonts(ctx, logger, dirs)
	if err != nil {
		e.err = err
		return nil, err
	}
	e.fonts, e.err = fonts, nil
	return fonts, nil
}

// dropConvertFontIndex removes e from the cache when it is still the entry of
// key.
func dropConvertFontIndex(key string, e *convertFontIndex) {
	convertFontCache.Lock()
	defer convertFontCache.Unlock()
	if convertFontCache.entries[key] != e {
		return
	}
	delete(convertFontCache.entries, key)
	if i := slices.Index(convertFontCache.order, key); i >= 0 {
		convertFontCache.order = slices.Delete(convertFontCache.order, i, i+1)
	}
}
