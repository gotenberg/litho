package pdf

import (
	"cmp"
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/remediation"
)

// FontProvider supplies the programs of the fonts a document shows without
// embedding them, which every PDF/A and PDF/UA profile requires embedded (ISO
// 19005-2, 6.2.11.4.1 and ISO 14289-1, 7.21.4.1 among them). litho ships no
// font program, so a remediation embeds only what a provider supplies, and
// the licence to embed it is the caller's. [WithFontProvider] registers one.
//
// FontProgram returns a TrueType or OpenType program, or a bare CFF one, for
// the font req describes, or nil to supply none. A returned error aborts the
// remediation. A provider shared by concurrent remediations must be safe for
// concurrent use.
type FontProvider interface {
	FontProgram(ctx context.Context, req FontRequest) ([]byte, error)
}

// FontProviderFunc adapts a function to a [FontProvider].
type FontProviderFunc func(ctx context.Context, req FontRequest) ([]byte, error)

// FontProgram calls f.
func (f FontProviderFunc) FontProgram(ctx context.Context, req FontRequest) ([]byte, error) {
	return f(ctx, req)
}

// FontRequest describes a font a document shows without embedding its
// program. Values are safe to read concurrently.
type FontRequest struct {
	// BaseFont is the font's /BaseFont as the file writes it, a subset tag
	// included; a composite font's is its descendant CIDFont's.
	BaseFont string
	// Subtype is the font's /Subtype: Type1, MMType1 or TrueType for a
	// simple font, the descendant's CIDFontType0 or CIDFontType2 for a
	// composite one.
	Subtype string
	// Flags is the font descriptor's /Flags (ISO 32000-1, 9.8.2), 0 when the
	// font has no descriptor. Bit 3 (value 4) marks a symbolic font, bit 7
	// (64) an italic one.
	Flags int
	// Encoding names the font's encoding: the predefined simple encoding its
	// /Encoding or /BaseEncoding names, or the CMap a composite font's
	// /Encoding names. It is empty when the font names none.
	Encoding string
	// Ordering is a composite font's character collection, Registry and
	// Ordering joined by a hyphen (Adobe-Japan1), and empty for a simple
	// font.
	Ordering string
	// Codes lists what the document shows with the font, in code order.
	Codes []FontCode
}

// FontCode is one code a document shows with a font. Values are safe to read
// concurrently.
type FontCode struct {
	// Code is a simple font's character code, or the CID a composite font's
	// code selects.
	Code uint32
	// Glyph is the glyph name a simple font's encoding gives the code, empty
	// when it gives none or the font is composite.
	Glyph string
	// Unicode is the character the glyph stands for, 0 when unknown.
	Unicode rune
	// Width is the width the font declares for the code, in thousandths of
	// an em. The supplied program's advance must agree with it within one
	// unit, or the font is refused.
	Width float64
}

// FontRefusal records a font a remediation left unembedded although a
// [FontProvider] was configured. Values are safe to read concurrently.
type FontRefusal struct {
	// BaseFont is the font's /BaseFont.
	BaseFont string
	// Object is the object number of the font dictionary in the file the
	// pass that last judged it read: the document for the first pass, the
	// output of the pass before for a later one. An output written as an
	// incremental update keeps the document's numbers, and one rewritten
	// whole numbers its objects anew. It is 0 for a font dictionary written
	// inline in a resource dictionary.
	Object int
	// Err says why, wrapping [ErrFontNotSupplied],
	// [ErrFontProgramInvalid], [ErrFontEmbeddingRestricted],
	// [ErrFontKind], [ErrFontCoverage], [ErrFontWidths],
	// [ErrFontRejected], [ErrFontUnwritable] or [ErrFontRolledBack].
	Err error
}

// String describes the refusal.
func (r FontRefusal) String() string {
	return fmt.Sprintf("font %s (object %d): %v", r.BaseFont, r.Object, r.Err)
}

// WithFontProvider embeds the programs p supplies for the fonts a document
// shows without embedding them. A remediation asks p once per such font and
// embeds the program it returns only where it serves the font as the document
// shows it:
//
//   - it holds a glyph for every code the document shows with the font;
//   - each of those glyphs advances as far as the font declares, within the
//     one unit the glyph width rules allow, so no text moves;
//   - its OS/2 fsType permits embedding its outlines;
//   - its outlines suit the font: a TrueType font takes TrueType (glyf)
//     outlines; a Type 1 font takes a CFF program, or TrueType outlines when
//     it is not symbolic, which makes it a TrueType font; a composite font of
//     an Adobe character collection (Japan1, GB1, CNS1, Korea1) takes
//     TrueType outlines through a /CIDToGIDMap built from the collection's
//     Unicode values;
//   - the embedded font fails no rule of the profile the unembedded one
//     passed.
//
// litho embeds a subset of the program, holding the glyphs the document shows
// with the font and those they are drawn from, under a subset tag, with the
// /CharSet or /CIDSet PDF/A-1 asks of a subset. The program stays whole when
// [WithWholeFontPrograms] asks for it, when its OS/2 fsType forbids subsetting, when its font descriptor serves another
// font the document shows, when the interactive form's default resources name
// the font, and when validation rejects the subset where it accepts the whole
// program; a whole program is written once and shared by every font given it.
// [RemediationReport.Fixed] states which each font received.
//
// Every font it leaves unembedded is listed with the reason in
// [RemediationReport.FontRefusals], and stays in the residual. Embedding
// changes the glyphs that render, so a regression the integrity probe detects
// rolls it back with the other content-modifying fixes. Without a provider,
// no font is embedded and no refusal is listed.
func WithFontProvider(p FontProvider) RemediateOption {
	return func(o *remediateOpts) {
		o.fonts = p
	}
}

// WithFontFiles embeds font programs read from the font files of fsys
// ([WithFontProvider]): every .ttf, .otf, .ttc, .otc and .cff file under its
// root, each face of a TrueType or OpenType Collection (.ttc, .otc) standing
// as a program of its own. The first font a remediation asks for indexes the
// files once, reading only the header tables that name each face, and the
// program a font takes is read when it is chosen. A font takes the program
// whose PostScript name is its /BaseFont with any subset tag dropped, else one
// of its family and style, bold and italic read from the name's style suffix
// ("Arial,Bold", "Arial-BoldItalicMT"), the face of regular or bold weight
// first. Failing both, it takes a program of a metric-compatible family:
// Helvetica, Arial, Arimo, Liberation Sans and Nimbus Sans stand for one
// another, as do Times, Times New Roman, Tinos, Liberation Serif and Nimbus
// Roman, and Courier, Courier New, Cousine, Liberation Mono and Nimbus Mono
// PS; Symbol takes Standard Symbols PS and ZapfDingbats D050000L. The width
// check keeps a family that is not metric-compatible after all from moving
// text, and [RemediationReport.Fixed] names the program each font received.
//
// A program of more than 64 MiB, a collection of more than 256 MiB, or a file
// no TrueType, OpenType or CFF reader accepts, is skipped. The index reads at
// most 16384 files and 64 MiB of header tables, and ignores the files past
// either bound. An error walking or reading fsys aborts the remediation that
// asked first. The option may be shared by concurrent remediations, which
// share its index.
func WithFontFiles(fsys fs.FS) RemediateOption {
	return WithFontProvider(newFontFiles(fsys))
}

// WithWholeFontPrograms embeds every program [WithFontProvider],
// [WithFontFiles] or [WithSystemFonts] supplies whole, under the font's own
// name, instead of the subset litho embeds by default. A subset holds only the
// glyphs the document shows, so text a later edit adds in characters the document does not show
// has no glyph to draw; a whole program keeps every glyph, at the cost of a
// larger file. Each font is still embedded only where the whole program
// serves it as the document shows it, and [RemediationReport.Fixed] states
// that the caller asked for the whole program. Without a provider it does
// nothing.
func WithWholeFontPrograms() RemediateOption {
	return func(o *remediateOpts) {
		o.wholeFonts = true
	}
}

// metricAliases are families drawn with the same advance widths, which
// [WithFontFiles] lets stand for one another.
var metricAliases = [][]string{
	{"helvetica", "arial", "arimo", "liberationsans", "nimbussans", "nimbussansl"},
	{"helveticanarrow", "arialnarrow", "liberationsansnarrow", "nimbussansnarrow"},
	{"times", "timesroman", "timesnewroman", "tinos", "liberationserif", "nimbusroman", "nimbusromanno9l"},
	{"courier", "couriernew", "cousine", "liberationmono", "nimbusmonops", "nimbusmonol"},
	{"symbol", "standardsymbolsps"},
	{"zapfdingbats", "d050000l"},
}

// metricFamilies returns the normalized families of family's
// [metricAliases] group, family alone when it belongs to none.
func metricFamilies(family string) []string {
	for _, group := range metricAliases {
		normalized := make([]string, len(group))
		for i, name := range group {
			normalized[i] = normalizeFontName(name)
		}
		if slices.Contains(normalized, family) {
			return normalized
		}
	}
	return []string{family}
}

// matchFontFile returns the file that best serves req, or nil. Between faces
// that match alike, the one of regular or bold weight comes first, so a Light
// or Black face of the family does not stand for its Regular or Bold one, and
// then the first indexed.
func matchFontFile(files []fontFile, req FontRequest) *fontFile {
	base := req.BaseFont
	if tag, rest, ok := strings.Cut(base, "+"); ok && len(tag) == 6 {
		base = rest
	}
	family, bold, italic := splitFontName(base)
	ps := normalizeFontName(base)
	families := metricFamilies(family)

	target := regularWeightClass
	if bold {
		target = boldWeightClassTarget
	}
	var best *fontFile
	bestRank, bestDist := 0, 0
	for i := range files {
		file := &files[i]
		var rank int
		switch {
		case file.psName == ps:
			rank = 3
		case file.bold == bold && file.italic == italic && file.family == family:
			rank = 2
		case file.bold == bold && file.italic == italic && slices.Contains(families, file.family):
			rank = 1
		default:
			continue
		}
		// A Type 1 font keeps its subtype over a CFF program, and a TrueType
		// font takes glyf outlines alone.
		rank *= 2
		if file.cffOutlines == (req.Subtype == "Type1") {
			rank++
		}
		dist := weightDistance(file.weight, target)
		if rank > bestRank || rank == bestRank && dist < bestDist {
			best, bestRank, bestDist = file, rank, dist
		}
	}
	return best
}

// OS/2 usWeightClass values of the Normal and Bold weights (OpenType
// specification, OS/2 table, usWeightClass).
const (
	regularWeightClass    = 400
	boldWeightClassTarget = 700
)

// weightDistance is how far a face of weight class w is from target. A face
// that states no weight (0) counts as the target's.
func weightDistance(w, target int) int {
	if w == 0 {
		return 0
	}
	if w > target {
		return w - target
	}
	return target - w
}

// splitFontName splits a PostScript or /BaseFont name into its normalized
// family and its bold and italic style: "Arial,BoldItalic",
// "Arial-BoldItalicMT" and "TimesNewRomanPS-BoldMT" split at the comma or the
// last hyphen.
func splitFontName(name string) (family string, bold, italic bool) {
	fam, style := name, ""
	if i := strings.IndexByte(name, ','); i >= 0 {
		fam, style = name[:i], name[i+1:]
	} else if i := strings.LastIndexByte(name, '-'); i > 0 {
		fam, style = name[:i], name[i+1:]
	}
	style = strings.ToLower(style)
	bold = strings.Contains(style, "bold") || strings.Contains(style, "black") || strings.Contains(style, "heavy")
	italic = strings.Contains(style, "italic") || strings.Contains(style, "oblique")
	return normalizeFontName(fam), bold, italic
}

// normalizeFontName lowercases a font name, drops its spaces, hyphens and
// underscores, and a vendor suffix (MT, PS, PSMT, MS) a producer appends.
func normalizeFontName(name string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(name) {
		switch r {
		case ' ', '-', '_':
		default:
			sb.WriteRune(r)
		}
	}
	out := sb.String()
	for _, suffix := range []string{"psmt", "mt", "ps", "ms"} {
		if trimmed, ok := strings.CutSuffix(out, suffix); ok && len(trimmed) >= 3 {
			return trimmed
		}
	}
	return out
}

// fontProvider adapts providers to the remediation engine: each is asked in
// turn, and the first program one returns, or the first error, answers. The
// providers of [WithFontFiles] and [WithSystemFonts] log to logger what they
// skip.
func fontProvider(logger *slog.Logger, providers []FontProvider) remediation.FontProvider {
	if len(providers) == 0 {
		return nil
	}
	return func(ctx context.Context, req remediation.FontRequest) ([]byte, error) {
		out := FontRequest{
			BaseFont: req.BaseFont,
			Subtype:  req.Subtype,
			Flags:    req.Flags,
			Encoding: req.Encoding,
			Ordering: req.Ordering,
			Codes:    make([]FontCode, len(req.Codes)),
		}
		for i, c := range req.Codes {
			out.Codes[i] = FontCode{Code: c.Code, Glyph: c.Glyph, Unicode: c.Unicode, Width: c.Width}
		}
		for _, p := range providers {
			var program []byte
			var err error
			if files, ok := p.(*fontFiles); ok {
				program, err = files.program(ctx, logger, out)
			} else {
				program, err = p.FontProgram(ctx, out)
			}
			if err != nil || program != nil {
				return program, err
			}
		}
		return nil, nil
	}
}

// fontRefusals translates the engine's refusals.
func fontRefusals(in []remediation.FontRefusal) []FontRefusal {
	if len(in) == 0 {
		return nil
	}
	out := make([]FontRefusal, len(in))
	for i, r := range in {
		out[i] = FontRefusal{BaseFont: r.BaseFont, Object: r.Object, Err: r.Err}
	}
	slices.SortStableFunc(out, func(a, b FontRefusal) int { return cmp.Compare(a.Object, b.Object) })
	return out
}
