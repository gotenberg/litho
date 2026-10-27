package pdf

import (
	"cmp"
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/remediation"
)

// FontProvider supplies the programs of the fonts a document shows without
// embedding them, which every PDF/A, PDF/UA and WTPDF profile requires
// embedded (ISO 19005-2, 6.2.11.4.1 and ISO 14289-1, 7.21.4.1 among them). litho ships no
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
//     outlines; a Type 1 font takes a CFF program, or TrueType outlines,
//     which make it a TrueType font; a composite font of
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
// A TrueType program receives the cmap a PDF reader looks its glyphs up
// through (ISO 32000-1, 9.6.6.4): a (3,1) subtable beside the Unicode one of a
// program made for the Macintosh, for a font that is not symbolic, and for a
// symbolic font a single subtable, which for a symbolic Type 1 font maps each
// code to the glyph of the name its encoding gives the code. A font embedded
// as a symbolic TrueType font, which a reader reads no text from but a
// /ToUnicode CMap, receives one stating the text its glyph names gave its
// codes, unless it carries its own.
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
// of its family and style, read from the name's style suffix ("Arial,Bold",
// "Arial-BoldItalicMT", "Helvetica-Narrow", "Futura-Light"): bold and italic,
// italic also when the font descriptor's /Flags marks the font italic, the
// weight a weight word states (Light, Medium, Semibold, Black), and the width
// a width word states (Condensed, Narrow), the face of regular or bold weight
// first. A face of another weight or width is another design, and does not
// stand for the font. Failing both, it takes a program of a metric-compatible family:
// Helvetica, Arial, Arimo, Liberation Sans and Nimbus Sans stand for one
// another, as do Times, Times New Roman, Tinos, Liberation Serif and Nimbus
// Roman, and Courier, Courier New, Cousine, Liberation Mono and Nimbus Mono
// PS; Symbol takes Standard Symbols PS and ZapfDingbats D050000L. The width
// check keeps a family that is not metric-compatible after all from moving
// text. A program refused for the font gives way to the next file that
// matches it, in that order, up to eight programs, so Courier New serves a
// Courier whose own face draws a glyph of another width.
// [RemediationReport.Fixed] names the program each font received.
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

// matchFontFile returns the file that best serves req, or nil. A face of the
// font's family stands for it only in the font's style: bold and italic as
// its name states them, italic also when its descriptor's /Flags sets the
// Italic flag, the weight a weight word of its name states, and the width a
// width word states. The face whose PostScript name is the font's serves it
// unless the Italic flag asks for a slant the face lacks. Between faces that
// match alike, the one closest to the weight comes first, so a Light or Black
// face of the family does not stand for its Regular or Bold one, and then the
// first indexed.
//
// # Corpus evidence
//
//   - govdocs: at PDF/A-2b with the macOS system fonts, 8 files show
//     Helvetica-Narrow, which took Helvetica's Regular face and was refused
//     for its widths, and now takes Arial Narrow and embeds; pdf.js reads
//     their text at the same positions, and veraPDF reports one 6.2.11.4.1
//     failure fewer per file and nothing new.
//   - govdocs: 16 fonts whose weight word the family has no face of
//     (Helvetica-Black, Times-ExtraBold, Futura-Light, Futura-Book,
//     Arial-BlackItalic) took the Bold or Regular face, every one refused
//     for its widths, and now match none.
//   - govdocs: the 294 fonts no program served before those changes name
//     families the system does not ship (Melior, New Century Schoolbook, Century Gothic, WP symbol
//     fonts) or styles it has no face of (Geneva,Bold, Symbol,Italic); none is
//     a name the matching misreads.
func matchFontFile(files []fontFile, req FontRequest) *fontFile {
	if ranked := matchFontFiles(files, req); len(ranked) > 0 {
		return ranked[0]
	}
	return nil
}

// matchFontFiles returns every file that serves req as [matchFontFile] judges
// it, best first: by rank, then by distance from the weight, then in index
// order. A program the remediation refuses for the font gives way to the next
// one, so a face of a metric-compatible family stands for the font where the
// face of its own name draws a glyph wider or narrower than the font
// declares.
func matchFontFiles(files []fontFile, req FontRequest) []*fontFile {
	base := req.BaseFont
	if tag, rest, ok := strings.Cut(base, "+"); ok && len(tag) == 6 {
		base = rest
	}
	family, style := splitFontName(base)
	// ISO 32000-1, 9.8.2, Table 123: bit 7 of /Flags marks a font whose
	// glyphs slant. A producer often leaves it clear on an italic font its
	// name calls italic, so the flag adds italic and never removes it.
	flagItalic := req.Flags&fontFlagItalic != 0
	if flagItalic {
		style.italic = true
	}
	ps := normalizeFontName(base)
	families := metricFamilies(family)
	target, tolerance := style.targetWeight()

	type match struct {
		file       *fontFile
		rank, dist int
	}
	matches := make([]match, 0, len(files))
	for i := range files {
		file := &files[i]
		dist := weightDistance(file.weight, target)
		sameStyle := file.bold == style.bold && file.italic == style.italic && dist <= tolerance
		var rank int
		switch {
		case file.psName == ps && (file.italic || !flagItalic):
			rank = 3
		case sameStyle && file.family == family:
			rank = 2
		case sameStyle && slices.Contains(families, file.family):
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
		matches = append(matches, match{file: file, rank: rank, dist: dist})
	}
	// A stable sort keeps index order between files that match alike.
	slices.SortStableFunc(matches, func(a, b match) int {
		if c := cmp.Compare(b.rank, a.rank); c != 0 {
			return c
		}
		return cmp.Compare(a.dist, b.dist)
	})
	ranked := make([]*fontFile, len(matches))
	for i, m := range matches {
		ranked[i] = m.file
	}
	return ranked
}

// fontFlagItalic is the Italic flag of a font descriptor's /Flags (ISO
// 32000-1, 9.8.2, Table 123, bit 7).
const fontFlagItalic = 1 << 6

// OS/2 usWeightClass values of the Normal and Bold weights (OpenType
// specification, OS/2 table, usWeightClass).
const (
	regularWeightClass    = 400
	boldWeightClassTarget = 700
)

// namedWeightTolerance is how far a face's weight class may be from the one a
// weight word of the font's name states (Light, Medium, Semibold, Black). The
// OpenType usWeightClass names its weights at multiples of 100, so a face
// within half a step is a face of that weight, and a Medium or Book face does
// not stand for a font its name calls Light or Regular. A matching heuristic,
// not spec-mandated.
const namedWeightTolerance = 50

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

// fontStyle is the style a font name states.
type fontStyle struct {
	bold, italic bool
	// weight is the usWeightClass a weight word of the name states, 0 when
	// it states none but Bold.
	weight int
	// width is the width word the name states, empty for a font of normal
	// width.
	width string
}

// targetWeight returns the weight class a face of the style has, and how far
// from it a face may be. A name stating no weight word but Bold bounds the
// distance not at all: it states the weight of the family's Regular or Bold
// face, which a face's subfamily and macStyle state where its usWeightClass
// may not, and the distance only ranks the faces of that style.
func (s fontStyle) targetWeight() (target, tolerance int) {
	switch {
	case s.weight != 0:
		return s.weight, namedWeightTolerance
	case s.bold:
		return boldWeightClassTarget, math.MaxInt
	}
	return regularWeightClass, math.MaxInt
}

// fontWeightWords maps the weight words of font names to their OpenType
// usWeightClass (OpenType specification, OS/2 table, usWeightClass), longest
// first where one holds another. Bold, Regular and their synonyms are absent:
// a name stating them alone states the weight a family's Bold or Regular
// face has, whatever its class.
var fontWeightWords = []struct {
	word   string
	weight int
}{
	{"extralight", 200},
	{"ultralight", 200},
	{"hairline", 100},
	{"thin", 100},
	{"semibold", 600},
	{"demibold", 600},
	{"extrabold", 800},
	{"ultrabold", 800},
	{"light", 300},
	{"book", 400},
	{"medium", 500},
	{"demi", 600},
	{"black", 900},
	{"heavy", 900},
}

// fontWidthWords are the width words of font names. A face of another width
// is another design, drawn with other advances.
var fontWidthWords = []string{"condensed", "narrow", "compressed", "extended", "expanded"}

// parseFontStyle reads the style words of a name's style suffix, or of a
// face's subfamily.
func parseFontStyle(style string) fontStyle {
	style = strings.ToLower(style)
	var s fontStyle
	s.bold = strings.Contains(style, "bold") || strings.Contains(style, "black") || strings.Contains(style, "heavy")
	s.italic = strings.Contains(style, "italic") || strings.Contains(style, "oblique")
	for _, w := range fontWeightWords {
		if strings.Contains(style, w.word) {
			s.weight = w.weight
			// A Semibold face and every heavier one is a bold face of its
			// family, a Medium one is not.
			s.bold = w.weight >= 600
			break
		}
	}
	for _, w := range fontWidthWords {
		if strings.Contains(style, w) {
			s.width = w
			break
		}
	}
	return s
}

// withWidth returns family named for the width word, which a name may state
// in its family ("ArialNarrow") or in its style ("Helvetica-Narrow"), so both
// name one family.
func withWidth(family, width string) string {
	if width == "" || strings.HasSuffix(family, width) {
		return family
	}
	return family + width
}

// splitFontName splits a PostScript or /BaseFont name into its normalized
// family, named for its width, and its style: "Arial,BoldItalic",
// "Arial-BoldItalicMT" and "TimesNewRomanPS-BoldMT" split at the comma or the
// last hyphen, and "Helvetica-Narrow" names the family helveticanarrow.
func splitFontName(name string) (string, fontStyle) {
	fam, style := name, ""
	if before, after, ok := strings.Cut(name, ","); ok {
		fam, style = before, after
	} else if i := strings.LastIndexByte(name, '-'); i > 0 {
		fam, style = name[:i], name[i+1:]
	}
	s := parseFontStyle(style)
	return withWidth(normalizeFontName(fam), s.width), s
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
// turn, and the programs they offer are the font's candidates, in provider
// order, the first error aborting. A [FontProvider] offers the one program it
// returns, and the providers of [WithFontFiles] and [WithSystemFonts] every
// face that serves the font, best first, read when the engine reaches it and
// logging to logger what they skip. The engine embeds the first candidate it
// accepts, so a later provider serves a font only when no program an earlier
// one offers does.
func fontProvider(logger *slog.Logger, providers []FontProvider) remediation.FontProvider {
	if len(providers) == 0 {
		return nil
	}
	return func(ctx context.Context, req remediation.FontRequest) ([]remediation.FontCandidate, error) {
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
		var candidates []remediation.FontCandidate
		for _, p := range providers {
			if files, ok := p.(*fontFiles); ok {
				offered, err := files.candidates(ctx, logger, out)
				if err != nil {
					return nil, err
				}
				candidates = append(candidates, offered...)
				continue
			}
			program, err := p.FontProgram(ctx, out)
			if err != nil {
				return nil, err
			}
			if program != nil {
				candidates = append(candidates, func(context.Context) ([]byte, error) { return program, nil })
			}
		}
		return candidates, nil
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
