package html

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typography/woff"
)

// CodeFontFaceSkipped flags an @font-face rule none of whose sources
// loaded, with the reason of each; its family then resolves to the next
// family of each list that names it.
const CodeFontFaceSkipped = "font-face-skipped"

// maxFontFaces caps the @font-face rules one conversion loads: 256. Anti-DoS
// guard, not spec-mandated: every face costs a parse and its tables in
// memory, and a page loads a handful of families of a few faces each; the
// unicode-range subsets a web font service splits a family into stay well
// below it. Rules past it are skipped with a warning.
const maxFontFaces = 256

// maxFontFaceBytes caps the program of one face, after WOFF decoding:
// [woff.MaxSfntBytes], 64 MiB, the bound litho's font index applies to one
// program. The file itself is bounded by the resource cap.
const maxFontFaceBytes = woff.MaxSfntBytes

// maxTotalFontBytes caps the font programs one conversion loads, after
// WOFF decoding: [limits.MaxLoadedFontBytes], the bound the font index
// applies to the faces it keeps loaded. Anti-DoS guard, not spec-mandated:
// a file the resource caps admit can decompress to a much larger program,
// and the faces stay in memory for the whole conversion. Faces past it are
// skipped with a warning. Each program is also charged to the
// conversion's memory budget while the conversion lives.
const maxTotalFontBytes = limits.MaxLoadedFontBytes

// Reasons a font source is not used. They end up in warning messages.
var (
	errFontFormat     = errors.New("format() names no format litho reads")
	errFontTech       = errors.New("tech() names a technology litho does not support")
	errFontTooLarge   = fmt.Errorf("the decoded font is larger than %d bytes", maxFontFaceBytes)
	errFontBudget     = fmt.Errorf("the conversion already loaded %d bytes of fonts", maxTotalFontBytes)
	errTooManyFaces   = fmt.Errorf("the document declares more than %d font faces", maxFontFaces)
	errNoFontSource   = errors.New("no source")
	errUnknownProgram = errors.New("not a TrueType, OpenType, collection, WOFF or WOFF2 file")
)

// fontFormats are the format() hints litho reads (CSS Fonts 4, 4.3.1,
// Table 1): TrueType, OpenType with glyf or CFF outlines, collections, WOFF
// and WOFF2. embedded-opentype and svg are refused, as are the
// -variations formats of the older syntax, whose axes litho does not
// apply.
var fontFormats = []string{"collection", "opentype", "truetype", "woff", "woff2"}

// fontTechs are the tech() keywords litho supports (CSS Fonts 4, 4.3.1):
// OpenType layout features, which the shaper applies. Colour fonts,
// variations, palettes, Graphite and AAT layout are not.
var fontTechs = []string{"features-opentype"}

// fontLoader loads the faces of one conversion's @font-face rules.
type fontLoader struct {
	res    *resources
	fonts  *text.Fonts
	logger *slog.Logger
	// programs caches the decoded program, or the error, of each URL.
	programs map[string]programResult
	total    int64
	warnings []compose.Warning
}

// programResult is the decoded program of one URL or why it failed.
type programResult struct {
	program []byte
	err     error
}

// loadFontFaces loads the faces of rules, in rule order, through res and
// the local faces of fonts (CSS Fonts 4, 4.3). Each rule's sources are tried
// in order and the first that loads is its face; a rule none of whose
// sources loads is reported with [CodeFontFaceSkipped] and still declares
// its family. fonts may be nil, which loads nothing.
func loadFontFaces(ctx context.Context, rules []css.FontFace, res *resources, fonts *text.Fonts, logger *slog.Logger) (*text.FaceSet, []compose.Warning) {
	if fonts == nil || len(rules) == 0 {
		return nil, nil
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	l := &fontLoader{res: res, fonts: fonts, logger: logger, programs: map[string]programResult{}}
	var faces []*text.Face
	var declared []string
	for i, rule := range rules {
		declared = append(declared, rule.Family)
		if ctx.Err() != nil {
			break
		}
		if i >= maxFontFaces {
			l.skip(rule, []string{errTooManyFaces.Error()})
			continue
		}
		if fc := l.load(ctx, rule); fc != nil {
			faces = append(faces, fc)
		}
	}
	set := text.NewFaceSet(faces)
	for _, fam := range declared {
		set.Declare(fam)
	}
	return set, l.warnings
}

// load returns the face of the first source of rule that loads, nil when
// none does.
func (l *fontLoader) load(ctx context.Context, rule css.FontFace) *text.Face {
	d := descriptorOf(rule)
	var reasons []string
	for _, src := range rule.Sources {
		fc, err := l.loadSource(ctx, src, d)
		if err == nil {
			return fc
		}
		name := "local(" + src.Local + ")"
		if src.Local == "" {
			name = displayURL(src.URL)
			if len(name) > 64 && strings.HasPrefix(strings.ToLower(name), "data:") {
				name = name[:64] + "..."
			}
		}
		reasons = append(reasons, name+": "+displayURL(err.Error()))
	}
	if len(reasons) == 0 {
		reasons = append(reasons, errNoFontSource.Error())
	}
	l.skip(rule, reasons)
	return nil
}

// skip reports rule as not loaded, for reasons.
func (l *fontLoader) skip(rule css.FontFace, reasons []string) {
	msg := fmt.Sprintf("the @font-face rule for %q loaded no face (%s); its family falls back to the next one", rule.Family, strings.Join(reasons, "; "))
	l.logger.Debug("html: skipping an @font-face rule", slog.String("family", rule.Family), slog.String("reasons", strings.Join(reasons, "; ")))
	l.warnings = append(l.warnings, compose.Warning{Code: CodeFontFaceSkipped, Message: msg})
}

// loadSource loads one src entry: a local() face of the index, or the file
// a url() names, skipped when its format() or tech() hints name nothing
// litho reads (CSS Fonts 4, 4.3.1).
func (l *fontLoader) loadSource(ctx context.Context, src css.FontSource, d text.FaceDescriptor) (*text.Face, error) {
	if src.Local != "" {
		return l.fonts.LocalFace(src.Local, d)
	}
	if len(src.Formats) > 0 && !slices.ContainsFunc(src.Formats, func(f string) bool { return slices.Contains(fontFormats, f) }) {
		return nil, errFontFormat
	}
	for _, t := range src.Tech {
		if !slices.Contains(fontTechs, t) {
			return nil, errFontTech
		}
	}
	program, err := l.program(ctx, src.URL)
	if err != nil {
		return nil, err
	}
	return text.LoadFace(program, d)
}

// program returns the sfnt program the URL u holds, WOFF and WOFF2 decoded,
// reading and decoding each URL once and charging its program to the
// conversion's font budget.
func (l *fontLoader) program(ctx context.Context, u string) ([]byte, error) {
	if r, ok := l.programs[u]; ok {
		return r.program, r.err
	}
	program, err := l.decode(ctx, u)
	if err == nil {
		switch {
		case l.total+int64(len(program)) > maxTotalFontBytes:
			program, err = nil, errFontBudget
		default:
			if err = limits.BudgetFrom(ctx).ChargeUnits(int64(len(program)), 1, "font programs"); err != nil {
				program = nil
			} else {
				l.total += int64(len(program))
			}
		}
	}
	l.programs[u] = programResult{program: program, err: err}
	return program, err
}

// decode fetches u and returns its sfnt program.
func (l *fontLoader) decode(ctx context.Context, u string) ([]byte, error) {
	data, err := l.res.fetch(ctx, u)
	if err != nil {
		return nil, err
	}
	if woff.Sniff(data) != woff.Other {
		program, err := woff.Decode(data)
		if errors.Is(err, woff.ErrTooLarge) {
			return nil, errFontTooLarge
		}
		return program, err
	}
	if !isSfntProgram(data) {
		return nil, errUnknownProgram
	}
	if len(data) > maxFontFaceBytes {
		return nil, errFontTooLarge
	}
	return data, nil
}

// isSfntProgram reports data that starts as an sfnt program or a
// collection does (ISO/IEC 14496-22, 5.1 and 5.2): version 1.0, "true",
// "OTTO" or "ttcf".
func isSfntProgram(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	switch string(data[:4]) {
	case "\x00\x01\x00\x00", "true", "OTTO", "ttcf":
		return true
	}
	return false
}

// descriptorOf returns the text package's form of rule's descriptors.
func descriptorOf(rule css.FontFace) text.FaceDescriptor {
	d := text.FaceDescriptor{
		Family:       rule.Family,
		WeightMin:    rule.WeightMin,
		WeightMax:    rule.WeightMax,
		StretchMin:   rule.StretchMin,
		StretchMax:   rule.StretchMax,
		UnicodeRange: rule.UnicodeRange,
	}
	switch rule.Style {
	case css.FontItalic:
		d.Style = text.FaceItalic
	case css.FontOblique:
		d.Style, d.ObliqueMin, d.ObliqueMax = text.FaceOblique, rule.ObliqueMin, rule.ObliqueMax
	case css.FontNormal:
	}
	return d
}
