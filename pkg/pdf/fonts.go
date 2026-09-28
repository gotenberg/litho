package pdf

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/remediation"
	"github.com/gotenberg/litho/pkg/internal/typography/cff"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
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
// embeds the program it returns, whole, only where it serves the font as the
// document shows it:
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
// litho does not subset: each distinct program is written once, whole, and
// shared by every font given it, so the output grows by the size of the
// programs embedded.
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
// ([WithFontProvider]): every .ttf, .otf and .cff file under its root, read
// once, at the first font a remediation asks for. A font takes the program
// whose PostScript name is its /BaseFont with any subset tag dropped, else one
// of its family and style, bold and italic read from the name's style suffix
// ("Arial,Bold", "Arial-BoldItalicMT"). Failing both, it
// takes a program of a metric-compatible family: Helvetica, Arial, Arimo,
// Liberation Sans and Nimbus Sans stand for one another, as do Times, Times
// New Roman, Tinos, Liberation Serif and Nimbus Roman, and Courier, Courier
// New, Cousine, Liberation Mono and Nimbus Mono PS; Symbol takes Standard
// Symbols PS and ZapfDingbats D050000L. The width check keeps a family that
// is not metric-compatible after all from moving text, and
// [RemediationReport.Fixed] names the program each font received.
//
// A file of more than 64 MiB, or one no TrueType, OpenType or CFF reader
// accepts, is skipped. An error walking fsys aborts the remediation that asked
// first. The option may be shared by concurrent remediations.
func WithFontFiles(fsys fs.FS) RemediateOption {
	return WithFontProvider(&fontFiles{fsys: fsys})
}

// maxFontFileBytes bounds the size of a font file [WithFontFiles] reads.
// Anti-DoS guard, not spec-mandated: the largest font programs in use, pan-CJK
// families, weigh 20 to 30 MiB, and a file past this bound in a font
// directory is no font a document embeds.
const maxFontFileBytes = 64 << 20

// fontFiles is the [FontProvider] of [WithFontFiles]. Its index is built once,
// under mu, and read-only after, so concurrent remediations share it.
type fontFiles struct {
	fsys    fs.FS
	mu      sync.Mutex
	indexed bool
	files   []fontFile
	err     error
}

// fontFile is one indexed program.
type fontFile struct {
	path   string
	psName string
	family string
	bold   bool
	italic bool
	// cffOutlines reports a program with CFF outlines, which a Type 1 font
	// takes without becoming a TrueType font.
	cffOutlines bool
}

// FontProgram returns the program of the best indexed match for req.
func (f *fontFiles) FontProgram(ctx context.Context, req FontRequest) ([]byte, error) {
	files, err := f.index(ctx)
	if err != nil {
		return nil, err
	}
	best := matchFontFile(files, req)
	if best == nil {
		return nil, nil
	}
	data, err := readFontFile(f.fsys, best.path)
	if err != nil {
		return nil, fmt.Errorf("reading font file %s: %w", best.path, err)
	}
	return data, nil
}

// index returns the index, building it at the first call. A walk the caller's
// ctx stopped is not kept, so the next remediation walks again.
func (f *fontFiles) index(ctx context.Context) ([]fontFile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.indexed {
		files, err := indexFontFiles(ctx, f.fsys)
		if err != nil && ctx.Err() != nil {
			return nil, err
		}
		f.files, f.err, f.indexed = files, err, true
	}
	return f.files, f.err
}

// indexFontFiles reads the names and styles of the font files under fsys, in
// path order.
func indexFontFiles(ctx context.Context, fsys fs.FS) ([]fontFile, error) {
	var files []fontFile
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch strings.ToLower(path.Ext(p)) {
		case ".ttf", ".otf", ".cff":
		default:
			return nil
		}
		data, err := readFontFile(fsys, p)
		if errors.Is(err, errFontFileTooLarge) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading font file %s: %w", p, err)
		}
		if file, ok := describeFontFile(p, data); ok {
			files = append(files, file)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("indexing font files: %w", err)
	}
	return files, nil
}

// readFontFile reads the file at p, refusing one past [maxFontFileBytes].
func readFontFile(fsys fs.FS, p string) ([]byte, error) {
	file, err := fsys.Open(p)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxFontFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFontFileBytes {
		return nil, errFontFileTooLarge
	}
	return data, nil
}

// describeFontFile reads the names and style of the program data holds.
func describeFontFile(p string, data []byte) (fontFile, bool) {
	if truetype.IsSfnt(data) {
		info, err := truetype.ReadInfo(data)
		if err != nil || info.Outline == truetype.OutlineNone {
			return fontFile{}, false
		}
		sub := strings.ToLower(info.Subfamily)
		return fontFile{
			path:        p,
			psName:      normalizeFontName(info.PostScriptName),
			family:      normalizeFontName(info.Family),
			bold:        strings.Contains(sub, "bold") || info.MacStyle&1 != 0,
			italic:      strings.Contains(sub, "italic") || strings.Contains(sub, "oblique") || info.MacStyle&2 != 0,
			cffOutlines: info.Outline == truetype.OutlineCFF,
		}, true
	}
	info, err := cff.ReadInfo(data)
	if err != nil || info.CID {
		return fontFile{}, false
	}
	family, bold, italic := splitFontName(info.FontName)
	return fontFile{path: p, psName: normalizeFontName(info.FontName), family: family, bold: bold, italic: italic, cffOutlines: true}, true
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

// matchFontFile returns the file that best serves req, or nil.
func matchFontFile(files []fontFile, req FontRequest) *fontFile {
	base := req.BaseFont
	if tag, rest, ok := strings.Cut(base, "+"); ok && len(tag) == 6 {
		base = rest
	}
	family, bold, italic := splitFontName(base)
	ps := normalizeFontName(base)
	families := metricFamilies(family)

	var best *fontFile
	bestRank := 0
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
		if rank > bestRank {
			best, bestRank = file, rank
		}
	}
	return best
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

// fontProvider adapts a [FontProvider] to the remediation engine.
func fontProvider(p FontProvider) remediation.FontProvider {
	if p == nil {
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
		return p.FontProgram(ctx, out)
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
