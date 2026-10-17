package remediation

import (
	"bytes"
	"cmp"
	"compress/zlib"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
	"github.com/gotenberg/litho/pkg/internal/typography/encodings"
)

// maxListedCodes bounds how many codes a refusal names; the rest are counted.
const maxListedCodes = 8

// matchFontEmbed matches the violation every part of ISO 19005 and ISO 14289
// states against a font whose program is not embedded: ISO 19005-1, 6.3.4 t1,
// ISO 19005-2 and 19005-3, 6.2.11.4.1 t1, ISO 19005-4, 6.2.10.4.1 t1, ISO
// 14289-1, 7.21.4.1 t1 and ISO 14289-2, 8.4.5.5.1 t1.
func matchFontEmbed(v compliance.Violation) bool {
	switch v.Rule {
	case compliance.Rule_19005_1_6_3_4_t01, compliance.Rule_19005_2_6_2_11_4_1_t01,
		compliance.Rule_19005_3_6_2_11_4_1_t01, compliance.Rule_19005_4_6_2_10_4_1_t01,
		compliance.Rule_14289_1_7_21_4_1_t01, compliance.Rule_14289_2_8_4_5_5_1_t01:
		return true
	}
	return false
}

// fixFontEmbed embeds, for each font the document shows without its program,
// the program [Options.Fonts] supplies, and records a [FontRefusal] for every
// such font it leaves unembedded. litho ships no font program, so without a
// provider the fixer does nothing and the embedding rules stay in the
// residual.
//
// A supplied program replaces the one a viewer substituted, so it is embedded
// only where it draws what the document showed: a glyph for every code the
// document shows ([ErrFontCoverage]), an advance within the glyph width rules'
// tolerance of every width the font declares ([ErrFontWidths]), so no text
// moves, and a font no rule of the profile fails that did not fail before
// ([ErrFontRejected]), judged as validation judges it
// ([compliance.JudgeFontShowing]). The program is then cut down to the glyphs
// the document shows ([fontEmbedder.subsetQueued]), which takes a fresh subset tag
// (ISO 32000-1, 9.6.4), or embedded whole where subsetting is barred or the
// subset fails a rule the whole program passes. A subset tag the file wrote
// goes, and so do a /CharSet and a /CIDSet, which describe a program the file
// never carried.
//
// The fonts are the ones a stream the pages draw shows text with, read
// through the allocator ([collectFontUses]), and a font no stream shows is
// left as it is: no rule asks for the program of a font nothing draws. A font
// dictionary written inline in a resource dictionary, which ISO 32000-1, 7.3.7
// permits, is embedded as the others are and written at a new object number,
// which its resource dictionary names in its place ([inlineFontSite.replace]).
// A reader resolves the name to the same dictionary either way, and the one
// object keeps the font a single font wherever the resource dictionary is
// shared. One whose resource dictionary no object number reaches, the
// resources of a Type 3 font written inline, is refused ([ErrFontUnwritable]),
// and so is a font of a subtype that takes no program ([ErrFontKind]).
//
// Every font this pass leaves unembedded while it shows text unembedded is
// listed with a [FontRefusal], one written inline with object number 0.
//
// # Corpus evidence
//
//   - All corpora: 10 files show text with an unembedded font written inline
//     in a resource dictionary, which the fixer used to leave unembedded and
//     unreported. Remediated at PDF/A-2b with a provider over Arial, Times
//     New Roman and Courier New, default and aggressive, pdfjs
//     asciihexdecode.pdf, freetexts.pdf, issue16633.pdf, issue17848.pdf,
//     issue6069.pdf, issue6106.pdf and issue6108.pdf lose 6.2.11.4.1 t1 on
//     litho and veraPDF 1.30.2, issue6069.pdf to issue6108.pdf 6.2.11.2 t4 to
//     t6 besides, and five outputs pass both tools. No rule gains a finding.
//   - litho fontcopies_2b.pdf and pdfjs issue11555.pdf: the 15 inline copies
//     of Helvetica and LithoKorea and the inline KozMinPro6N-Regular the
//     provider cannot serve are now listed as refused, with object number 0.
//   - litho fontembed_2b.pdf: three fonts written inline, in a page's /Font
//     dictionary, a form's /Resources and a /Resources two pages share, are
//     embedded with synthetic programs, on litho and veraPDF alike.
//   - govdocs and litho: measured with a provider over the Liberation 2.1.5
//     and URW base35 families, on the 856 files that hold an unembedded
//     font, at PDF/A-2b and PDF/UA-1, default and aggressive.
//   - govdocs: 785 files. The findings of ISO 19005-2, 6.2.11.4.1 t1 fall
//     from 4559 to 728 at PDF/A-2b default (721 aggressive), those of ISO
//     14289-1, 7.21.4.1 t1 from 4560 to 728 at PDF/UA-1, and 416 PDF/A-2b
//     and 287 PDF/UA-1 outputs are left with no finding at all. No rule gains
//     a finding in any file, and no embedding is rolled back.
//   - litho: 71 files. 6.2.11.4.1 t1 falls from 128 to 61 at PDF/A-2b, and
//     37 more outputs are left with no finding. No rule gains a finding.
//   - govdocs and litho: 70 of the files, remediated with and without the
//     provider and judged by veraPDF 1.30.2: the embedding findings fall
//     from 341 to 53 at PDF/A-2b and at PDF/UA-1, the 53 litho reports, and
//     no rule gains a finding. pdf.js places each of the 193275 text items
//     of the 140 outputs where it placed them without the provider.
//   - govdocs: of the 730 refusals at PDF/A-2b, 452 name a font the
//     families do not cover, 184 a width the program does not share (a
//     space the producer declared narrower, a soft hyphen declared 0), 78 a
//     program the font cannot take (Symbol over a CFF program, Arial Narrow,
//     an Identity ordering), 15 a missing glyph, and 1 a symbolic TrueType
//     font the program's cmap fails ISO 19005-2, 6.2.11.6 t4 for.
func fixFontEmbed(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, _ []compliance.Violation, profile string) ([]string, error) {
	if opts.Fonts == nil {
		return nil, nil
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		doc.Logger.Debug("remediation: font-embed: no catalog", "error", err)
		return nil, nil
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		doc.Logger.Debug("remediation: font-embed: no page tree", "error", err)
		return nil, nil
	}
	entries := pageTreeEntries(ctx, alloc, doc, pages)
	uses := collectFontUses(ctx, alloc, doc, pages, entries)
	e := &fontEmbedder{
		alloc:      alloc,
		doc:        doc,
		provider:   opts.Fonts,
		wholeFonts: opts.WholeFonts,
		profile:    profile,
		programs:   make(map[programShare]ast.PdfIndirectRef),
		written:    make(map[int]programKey),
		fdMapping:  make(map[int]string),
	}
	targets := make([]fontTarget, 0, len(uses.codes)+len(uses.inline))
	for _, objNum := range slices.Sorted(maps.Keys(uses.codes)) {
		targets = append(targets, fontTarget{objNum: objNum, codes: uses.codes[objNum], composite: uses.composite[objNum]})
	}
	for _, use := range uses.inline {
		targets = append(targets, fontTarget{site: &use.site, walked: use.font, codes: use.codes, composite: use.composite})
	}
	var outcomes []fontOutcome
	for i, t := range targets {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("embedding fonts: %w", err)
		}
		err := e.embed(ctx, i, t)
		var refusal *fontRefusedError
		switch {
		case errors.As(err, &refusal):
			outcomes = append(outcomes, fontOutcome{order: i, baseFont: refusal.baseFont, objNum: t.objNum, refused: refusal.err})
		case err != nil:
			return nil, err
		}
	}
	written, err := e.finish(ctx, uses)
	if err != nil {
		return nil, err
	}
	outcomes = append(outcomes, written...)
	slices.SortStableFunc(outcomes, func(a, b fontOutcome) int { return cmp.Compare(a.order, b.order) })
	var fixes []string
	for _, o := range outcomes {
		if o.refused != nil {
			if opts.fonts != nil {
				opts.fonts.refused = append(opts.fonts.refused, FontRefusal{BaseFont: o.baseFont, Object: o.objNum, Err: o.refused})
			}
			continue
		}
		fixes = append(fixes, o.fix)
		if opts.fonts != nil {
			opts.fonts.embedded = append(opts.fonts.embedded, FontEmbed{BaseFont: o.baseFont, Object: o.objNum})
		}
	}
	return fixes, nil
}

// fontOutcome is what [fixFontEmbed] did with one of its targets, under the
// order it met the target in: the fix description of a font it embedded, or
// why it refused the font. objNum is the font's object number in the document
// the run read, 0 for a font written inline.
type fontOutcome struct {
	order    int
	baseFont string
	objNum   int
	fix      string
	refused  error
}

// fontTarget is a font [fixFontEmbed] embeds: the font object objNum, or the
// font dictionary written inline at site, as the walk read it in walked, with
// what the pages show with it ([fontUses]).
type fontTarget struct {
	objNum    int
	site      *inlineFontSite
	walked    ast.PdfDict
	codes     map[byte]bool
	composite []ast.PdfString
}

// fontRefusedError carries a refusal out of [fontEmbedder.embed].
type fontRefusedError struct {
	baseFont string
	err      error
}

func (r *fontRefusedError) Error() string { return r.baseFont + ": " + r.err.Error() }

func (r *fontRefusedError) Unwrap() error { return r.err }

// programKey identifies an embedded program stream, so fonts that receive the
// same program share it.
type programKey struct {
	sum    [sha256.Size]byte
	format outlineFormat
}

// programShare identifies a program stream the run writes: a program, read
// under one mapping of codes to glyphs ([fontEmbedder.codeMapping]). Fonts
// share a stream only when both agree.
type programShare struct {
	program programKey
	mapping string
}

// fontEmbedder carries one run of [fixFontEmbed].
type fontEmbedder struct {
	alloc    *writer.ObjAllocator
	doc      *document.Document
	provider FontProvider
	// wholeFonts embeds every program whole ([Options.WholeFonts]).
	wholeFonts bool
	profile    string
	programs   map[programShare]ast.PdfIndirectRef
	// written holds, by descriptor object number, the program this run wrote
	// into the descriptor. A descriptor held directly has no entry.
	written map[int]programKey
	// fdMapping holds, by descriptor object number, the mapping of the first
	// font the run planned over the descriptor: fonts sharing a descriptor
	// share the program it holds, whatever their mappings.
	fdMapping map[int]string
	// fdUsers and drFonts are [fontEmbedder.descriptorUsers] and
	// [fontEmbedder.formFonts], read once per run.
	fdUsers map[int]int
	drFonts map[int]bool
	// queued holds the fonts embed planned, in the order the run met them.
	queued []*queuedFont
}

// embedPlan is a font as it stands once embedded: the font dictionary holding
// its descriptor, and the descriptor its program, directly, as the judgement
// reads them, and what writing it takes.
type embedPlan struct {
	font ast.PdfDict
	// descriptor is the descriptor the font (a composite font's descendant)
	// receives, holding the program at programKey.
	descriptor ast.PdfDict
	// descendant is a composite font's new descendant CIDFont, which holds
	// descriptor; nil for a simple font.
	descendant ast.PdfDict
	program    *suppliedProgram
	programKey ast.PdfName
	// cidToGID is a composite font's /CIDToGIDMap stream data.
	cidToGID []byte
	// cidSet is the /CIDSet stream data a composite font's subset carries,
	// nil when it carries none.
	cidSet []byte
	// gids are the glyphs of program the shown codes draw.
	gids []uint16
	// toUnicode is the /ToUnicode CMap stream data a simple font receives,
	// nil when it keeps its own or receives none.
	toUnicode []byte
	// rebuild plans the same font over another program.
	rebuild func(ctx context.Context, prog *suppliedProgram) (*embedPlan, error)
	// subsetGlyphs is the glyph count of a subset program, 0 for a whole one,
	// and whole the reason a program is embedded whole.
	subsetGlyphs int
	whole        string
}

// embed plans the embedding of the font t names when it is shown and
// unembedded, and queues the plan for [fontEmbedder.finish], which writes it,
// under order. It returns a *fontRefusedError for a font it leaves
// unembedded, or the provider's error: finish reports what it writes.
func (e *fontEmbedder) embed(ctx context.Context, order int, t fontTarget) error {
	var font ast.PdfDict
	if t.site != nil {
		if font = t.site.font(ctx, e.alloc, e.doc); font == nil {
			font = t.walked
		}
	} else {
		font = resolveFromAllocOrDoc(ctx, e.alloc, e.doc, ast.PdfIndirectRef{ObjectNum: t.objNum})
	}
	if font == nil {
		return nil
	}
	subtype := nameOf(ctx, e.doc, font["Subtype"])
	var shown []ast.PdfString
	switch subtype {
	case "Type0":
		shown = t.composite
	case "Type3":
		// ISO 32000-1, 9.6.5: a Type 3 font draws its glyphs with content
		// streams and has no program to embed.
		return nil
	default:
		if len(t.codes) > 0 {
			shown = []ast.PdfString{ast.PdfString(slices.Sorted(maps.Keys(t.codes)))}
		}
	}
	if len(shown) == 0 {
		return nil
	}
	embedRule := compliance.FontEmbeddingRule(e.profile)
	before := compliance.JudgeFontShowing(ctx, e.doc, font, shown, e.profile)
	if !hasViolationOf(before, embedRule) {
		return nil
	}
	baseFont := nameOf(ctx, e.doc, font["BaseFont"])
	refused := func(err error) error { return &fontRefusedError{baseFont: baseFont, err: err} }
	switch subtype {
	case "Type1", "MMType1", "TrueType", "Type0":
	default:
		// ISO 32000-1, 9.5 and Table 110: a resource dictionary's font is a
		// Type0, Type1, MMType1, Type3 or TrueType font, and a descendant
		// CIDFont named there directly, or a font of no known subtype, has no
		// program validation reads as its own.
		return refused(fmt.Errorf("%w: a font of subtype %q", ErrFontKind, subtype))
	}
	if t.site != nil && !t.site.writable(ctx, e.alloc, e.doc) {
		return refused(fmt.Errorf("%w: a font dictionary written inline where no object number reaches its resource dictionary", ErrFontUnwritable))
	}

	// A descriptor this run wrote a program into is one an earlier font
	// shares, and the font takes the same program there. Any other program
	// key is one the file wrote and validation does not read as a program: a
	// viewer may still draw it, so it is not replaced.
	fdNum := e.descriptorNumber(ctx, font, subtype)
	if _, ours := e.written[fdNum]; !ours && e.holdsProgram(ctx, font, subtype) {
		return refused(fmt.Errorf("%w: the font carries a program validation cannot read, which litho does not replace", ErrFontKind))
	}

	var req FontRequest
	var build func(ctx context.Context, prog *suppliedProgram) (*embedPlan, error)
	var err error
	if subtype == "Type0" {
		var c *compositeFont
		if c, req, err = e.prepareComposite(ctx, font, shown); err == nil {
			build = func(_ context.Context, prog *suppliedProgram) (*embedPlan, error) { return e.buildComposite(c, prog) }
		}
	} else {
		var s *simpleFont
		if s, req, err = e.prepareSimple(ctx, font, subtype, t.codes); err == nil {
			build = func(ctx context.Context, prog *suppliedProgram) (*embedPlan, error) {
				return e.buildSimple(ctx, s, prog)
			}
		}
	}
	var plan *embedPlan
	if err == nil {
		plan, err = e.planCandidates(ctx, req, build, before, shown)
	}
	if err != nil {
		if isFontRefusal(err) {
			return refused(err)
		}
		return err
	}
	key := programKey{sum: sha256.Sum256(plan.program.data), format: plan.program.format}
	if prev, ours := e.written[fdNum]; ours && prev != key {
		return refused(fmt.Errorf("%w: the font shares its descriptor with a font given another program", ErrFontKind))
	}
	mapping := e.codeMapping(ctx, plan)
	share := programShare{program: key, mapping: mapping}
	if fdNum != 0 {
		e.written[fdNum] = key
		if mapping, seen := e.fdMapping[fdNum]; seen {
			share.mapping = mapping
		} else {
			e.fdMapping[fdNum] = share.mapping
		}
	}
	e.queued = append(e.queued, &queuedFont{
		order: order, objNum: t.objNum, site: t.site, font: font, baseFont: baseFont, fdNum: fdNum,
		before: before, shown: shown, plan: plan, share: share, mapping: mapping,
	})
	return nil
}

// isFontRefusal reports whether err wraps one of the refusal sentinels.
func isFontRefusal(err error) bool {
	for _, s := range []error{ErrFontNotSupplied, ErrFontProgramInvalid, ErrFontEmbeddingRestricted, ErrFontKind, ErrFontCoverage, ErrFontWidths, ErrFontRejected, ErrFontUnwritable} {
		if errors.Is(err, s) {
			return true
		}
	}
	return false
}

// descriptorHolder returns the dictionary holding the font's descriptor: the
// font, or a composite font's descendant CIDFont.
func (e *fontEmbedder) descriptorHolder(ctx context.Context, font ast.PdfDict, subtype string) ast.PdfDict {
	if subtype == "Type0" {
		return e.descendant(ctx, font)
	}
	return font
}

// descriptorNumber returns the object number of the font's descriptor, 0 when
// it has none or holds it directly.
func (e *fontEmbedder) descriptorNumber(ctx context.Context, font ast.PdfDict, subtype string) int {
	if ref, ok := e.descriptorHolder(ctx, font, subtype)["FontDescriptor"].(ast.PdfIndirectRef); ok {
		return ref.ObjectNum
	}
	return 0
}

// holdsProgram reports whether the font's descriptor, read through the
// allocator, has a program key.
func (e *fontEmbedder) holdsProgram(ctx context.Context, font ast.PdfDict, subtype string) bool {
	fd := liveDictValue(ctx, e.alloc, e.doc, e.descriptorHolder(ctx, font, subtype)["FontDescriptor"])
	for _, key := range []ast.PdfName{"FontFile", "FontFile2", "FontFile3"} {
		if _, ok := fd[key]; ok {
			return true
		}
	}
	return false
}

// descendant returns a composite font's descendant CIDFont, read through the
// allocator.
func (e *fontEmbedder) descendant(ctx context.Context, font ast.PdfDict) ast.PdfDict {
	arr := liveArray(ctx, e.alloc, e.doc, font["DescendantFonts"])
	if len(arr) == 0 {
		return nil
	}
	return liveDictValue(ctx, e.alloc, e.doc, arr[0])
}

// maxFontCandidates bounds the programs one font is planned over. Cost guard,
// not spec-mandated: each candidate is read whole, up to the stream budget,
// then planned and judged, and a provider may offer any number. The
// metric-compatible families a font may take hold at most seven names, each
// with one face of the font's style, so eight candidates try every program
// that may serve a font even on a system that installs them all.
const maxFontCandidates = 8

// planCandidates plans the font over the first program of the candidates the
// provider offers for req that serves it: build plans a program, which
// [fontEmbedder.judge] then judges against before. A program refused for the
// font gives way to the next, so a face of a metric-compatible family serves
// a font whose own face draws a glyph wider or narrower than the font
// declares. A candidate repeating a program already tried is passed over.
// When none serves, the refusal is the first candidate's, the best match's,
// and counts the programs refused; it is [ErrFontNotSupplied] when the provider
// offered no program at all.
//
// # Corpus evidence
//
//   - govdocs: remediated at PDF/A-2b with WithSystemFonts on macOS, 8 fonts
//     of 7 files refused over the first program are embedded over a later
//     one: 4 Courier fonts Apple's Courier drew with a degree sign of 400
//     where they declare 600, now over Courier New, and 2 Times New Roman and
//     2 Arial fonts declaring a periodcentered of 250 and 278, which
//     Microsoft's programs draw at 333, now over Times and Helvetica. veraPDF
//     1.30.2 reports 19 failures fewer on the 7 outputs (6.2.11.4.1 t1 and
//     6.2.11.2 t4 to t6) and none more, pdf.js places their 35782 text items
//     where it placed them before, and the 1084 other outputs are
//     byte-identical.
//   - govdocs: the 512 refusals left are 310 fonts no program serves, of
//     families the system does not ship (Century, Melior, Book Antiqua, WP
//     symbol fonts) or in styles it has no face of (Geneva,Bold,
//     Symbol,Italic, Futura-Book); 174 whose widths no face shares (a
//     producer's metrics off by a few units across the font, mu and
//     plusminus at Adobe's 500 and 564); 15 a code no face has a glyph for (a
//     control code, a .notdef space, an fi Symbol does not draw); and 13 of an
//     Identity ordering.
func (e *fontEmbedder) planCandidates(ctx context.Context, req FontRequest, build func(ctx context.Context, prog *suppliedProgram) (*embedPlan, error), before []compliance.Violation, shown []ast.PdfString) (*embedPlan, error) {
	candidates, err := e.provider(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("font provider for %s: %w", req.BaseFont, err)
	}
	var first error
	tried := 0
	seen := make(map[[sha256.Size]byte]bool)
	for _, candidate := range candidates {
		if tried == maxFontCandidates {
			e.doc.Logger.Debug("remediation: font-embed: candidate bound reached", "font", req.BaseFont, "max", maxFontCandidates)
			break
		}
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("embedding font %s: %w", req.BaseFont, err)
		}
		data, err := candidate(ctx)
		if err != nil {
			return nil, fmt.Errorf("font provider for %s: %w", req.BaseFont, err)
		}
		if len(data) == 0 {
			continue
		}
		sum := sha256.Sum256(data)
		if seen[sum] {
			continue
		}
		seen[sum] = true
		tried++
		plan, err := e.planProgram(ctx, data, build, before, shown)
		if err == nil {
			return plan, nil
		}
		if !isFontRefusal(err) {
			return nil, err
		}
		if first == nil {
			first = err
		}
	}
	switch {
	case first == nil:
		return nil, ErrFontNotSupplied
	case tried > 1:
		return nil, fmt.Errorf("%w (%d programs refused)", first, tried)
	}
	return nil, first
}

// planProgram reads a supplied program, plans the font over it with build,
// and judges the plan.
func (e *fontEmbedder) planProgram(ctx context.Context, data []byte, build func(ctx context.Context, prog *suppliedProgram) (*embedPlan, error), before []compliance.Violation, shown []ast.PdfString) (*embedPlan, error) {
	prog, err := readSuppliedProgram(ctx, data, e.doc.Limits().MaxStreamBytes)
	if err != nil {
		return nil, err
	}
	plan, err := build(ctx, prog)
	if err != nil {
		return nil, err
	}
	if err := e.judge(ctx, plan, before, shown); err != nil {
		return nil, err
	}
	return plan, nil
}

// prepareSimple reads what planning a simple font takes, and the request the
// provider answers: a TrueType font takes a TrueType program, and a Type 1
// font a CFF program as /Type1C or a TrueType program, which makes it a
// TrueType font.
func (e *fontEmbedder) prepareSimple(ctx context.Context, font ast.PdfDict, subtype string, shownSet map[byte]bool) (*simpleFont, FontRequest, error) {
	if subtype == "MMType1" {
		// ISO 32000-1, 9.6.2.3 draws a multiple master instance from the
		// master design a /FontFile program holds, which no other program
		// stands in for.
		return nil, FontRequest{}, fmt.Errorf("%w: a multiple master font", ErrFontKind)
	}
	fd := liveDictValue(ctx, e.alloc, e.doc, font["FontDescriptor"])
	baseFont := nameOf(ctx, e.doc, font["BaseFont"])
	flags := int(document.Float(resolved(ctx, e.doc, fd["Flags"])))
	symbolic := flags&fontFlagSymbolic != 0 || isSymbolStandardFont(baseFont)
	names := compliance.UnembeddedFontGlyphNames(ctx, e.doc, font)
	codes := slices.Sorted(maps.Keys(shownSet))

	widths, err := e.declaredWidths(ctx, font, fd, baseFont, names)
	if err != nil {
		return nil, FontRequest{}, err
	}
	req := FontRequest{BaseFont: baseFont, Subtype: subtype, Flags: flags, Encoding: simpleEncodingName(ctx, e.doc, font)}
	for _, code := range codes {
		w := widths.at(code)
		fc := FontCode{Code: uint32(code), Glyph: names[code], Width: w}
		if r, ok := encodings.GlyphToRuneFull(names[code]); ok {
			fc.Unicode = r
		}
		req.Codes = append(req.Codes, fc)
	}
	s := &simpleFont{font: font, subtype: subtype, fd: fd, baseFont: baseFont, symbolic: symbolic, names: names, codes: codes, shownSet: shownSet, widths: widths}
	return s, req, nil
}

// simpleFont is what planning a simple font reads from the file, which
// [fontEmbedder.buildSimple] turns into a plan for a program.
type simpleFont struct {
	font     ast.PdfDict
	subtype  string
	fd       ast.PdfDict
	baseFont string
	symbolic bool
	names    map[byte]string
	codes    []byte
	shownSet map[byte]bool
	widths   declaredWidths
}

// buildSimple plans the embedding of prog for the simple font s, refusing a
// program that lacks a glyph for a shown code or advances one differently
// from the width the font declares.
func (e *fontEmbedder) buildSimple(ctx context.Context, s *simpleFont, prog *suppliedProgram) (*embedPlan, error) {
	font, subtype, baseFont, symbolic, names, codes, shownSet, widths := s.font, s.subtype, s.baseFont, s.symbolic, s.names, s.codes, s.shownSet, s.widths
	cand := maps.Clone(font)
	var glyph func(code byte) (uint16, bool)
	var toUnicode []byte
	switch {
	case subtype == "TrueType" && prog.format != outlinesTrueType:
		return nil, fmt.Errorf("%w: a TrueType font takes glyf outlines, and %s carries CFF ones", ErrFontKind, prog.psName)
	case subtype == "TrueType" && symbolic:
		// ISO 32000-1, 9.6.6.4: a symbolic TrueType font maps its codes
		// through the program's (3,0) or (1,0) cmap subtable.
		prog = e.fitCmap(ctx, prog, true)
		if len(prog.symbol) == 0 {
			return nil, fmt.Errorf("%w: a symbolic TrueType font needs a (3,0) or (1,0) cmap, which %s lacks", ErrFontKind, prog.psName)
		}
		toUnicode = symbolicToUnicode(ctx, e.doc, font, names)
		glyph = func(code byte) (uint16, bool) { gid, ok := prog.symbol[uint16(code)]; return gid, ok }
	case prog.format == outlinesTrueType && symbolic:
		// A symbolic Type 1 font names its glyphs through its own encoding,
		// which a symbolic TrueType font does not carry: ISO 32000-1,
		// 9.6.6.4 has a reader map each code through the program's (3,0) or
		// (1,0) cmap subtable, and ISO 19005-2, 6.2.11.6 bars an /Encoding
		// from the font. The program receives a (3,0) subtable taking each
		// code to the glyph of the name the Type 1 font gave it
		// ([fontEmbedder.symbolCmap]), so every code keeps its glyph.
		if names == nil {
			return nil, fmt.Errorf("%w: the codes of the symbolic font %s name no glyphs the file states", ErrFontKind, baseFont)
		}
		fitted, err := e.symbolCmap(ctx, prog, names)
		if err != nil {
			return nil, err
		}
		prog = fitted
		cand["Subtype"] = ast.PdfName("TrueType")
		delete(cand, "Encoding")
		toUnicode = symbolicToUnicode(ctx, e.doc, font, names)
		glyph = func(code byte) (uint16, bool) { gid, ok := prog.symbol[uint16(code)]; return gid, ok }
	case prog.format == outlinesTrueType:
		prog = e.fitCmap(ctx, prog, false)
		if names == nil || len(prog.unicode) == 0 {
			return nil, fmt.Errorf("%w: the codes of %s cannot reach the Unicode cmap of %s", ErrFontKind, baseFont, prog.psName)
		}
		if subtype == "Type1" {
			// ISO 32000-1, 9.6.6.4: a non-symbolic TrueType font names its
			// codes through MacRomanEncoding or WinAnsiEncoding, with
			// /Differences, and a conforming reader looks the names up in
			// the (3,1) cmap. Every code the Type 1 font named keeps its
			// name.
			cand["Subtype"] = ast.PdfName("TrueType")
			cand["Encoding"] = trueTypeEncoding(ctx, e.doc, font, names, shownSet)
		}
		glyph = func(code byte) (uint16, bool) { return prog.nameToGID(names[code]) }
	default:
		// A Type 1 font over a CFF program (ISO 32000-1, 9.6.2.1 and Table
		// 126).
		if prog.cid {
			return nil, fmt.Errorf("%w: a simple font takes a name-keyed CFF program, and %s is CID-keyed", ErrFontKind, prog.psName)
		}
		if names == nil {
			return nil, fmt.Errorf("%w: the codes of the symbolic font %s name no glyphs the file states", ErrFontKind, baseFont)
		}
		if enc := type1CEncoding(ctx, e.doc, font, names, prog.builtin, shownSet); enc != nil {
			cand["Encoding"] = enc
		}
		glyph = func(code byte) (uint16, bool) {
			name := names[code]
			if name == "" {
				return 0, false
			}
			gid, ok := prog.names[name]
			return gid, ok
		}
	}

	if toUnicode != nil {
		cand["ToUnicode"] = ast.NewStream(ast.PdfDict{"Length": ast.PdfInt(len(toUnicode))}, toUnicode)
	}

	var missing, off []string
	var gids []uint16
	for _, code := range codes {
		gid, ok := glyph(code)
		if !ok || gid == 0 {
			missing = append(missing, describeCode(uint32(code), names[code]))
			continue
		}
		gids = append(gids, gid)
		declared := widths.at(code)
		adv, known := prog.advance(gid)
		if !known || !compliance.WidthsConsistent(declared, adv) {
			off = append(off, fmt.Sprintf("%s declares %g, program %.1f", describeCode(uint32(code), names[code]), declared, adv))
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: %s has no glyph for %s", ErrFontCoverage, prog.psName, listCodes(missing))
	}
	if len(off) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrFontWidths, listCodes(off))
	}
	if widths.synthesized != nil {
		maps.Copy(cand, widths.synthesized)
	}

	descriptor := e.descriptor(s.fd, baseFont, prog, symbolic)
	stripSubsetTag(cand, descriptor)
	key := ast.PdfName("FontFile2")
	if prog.format == outlinesCFF {
		key = "FontFile3"
	}
	descriptor[key] = programStream(prog)
	cand["FontDescriptor"] = descriptor
	return &embedPlan{
		font: cand, descriptor: descriptor, program: prog, programKey: key, gids: gids, toUnicode: toUnicode,
		rebuild: func(ctx context.Context, sub *suppliedProgram) (*embedPlan, error) { return e.buildSimple(ctx, s, sub) },
	}, nil
}

// symbolicToUnicode returns the /ToUnicode CMap a simple font embedded as a
// symbolic TrueType font receives, nil when it carries one of its own or the
// file names no glyph for its codes. A reader derives the text of a symbolic
// TrueType font's codes from its /ToUnicode CMap (ISO 32000-1, 9.10.2), where
// it read the unembedded font's through the glyph names its encoding or, for
// Symbol and ZapfDingbats, the standard font's built-in encoding gives them
// (names). The CMap states the text of every code names covers, as litho's
// text extraction reads it ([typography.ResolveFont]), so the text stays what
// it was. A code the file names no glyph for gets none: its meaning would be
// invented.
//
// # Corpus evidence
//
//   - govdocs: remediated at PDF/A-2b with WithSystemFonts on macOS, pdf.js
//     read Symbol's bullet as the MacRoman summation sign and alpha as "a"
//     in 11 of 31 outputs sampled, once the Symbol fonts took Apple's
//     programs without a /ToUnicode CMap. With the CMap, 4 read otherwise
//     than unembedded: ZapfDingbats codes pdf.js had read as the letters l,
//     q and z now read as the dingbats they draw, Delta reads U+0394 for
//     U+2206, and three Symbol brace pieces, whose Private Use values no CMap
//     states, read as Latin-1 letters where pdf.js read nothing.
func symbolicToUnicode(ctx context.Context, doc *document.Document, font ast.PdfDict, names map[byte]string) []byte {
	if _, ok := font["ToUnicode"]; ok || names == nil {
		return nil
	}
	resolved, err := typography.ResolveFont(ctx, doc, font)
	if err != nil || resolved == nil {
		doc.Logger.Debug("remediation: font-embed: no text for the symbolic font", "error", err)
		return nil
	}
	mapping := make(map[uint16]rune, len(resolved.Encoding))
	for code, r := range resolved.Encoding {
		if names[code] != "" && names[code] != ".notdef" && isValidUnicodeMapping(r) {
			mapping[uint16(code)] = r
		}
	}
	if len(mapping) == 0 {
		return nil
	}
	return generateToUnicodeCMap(mapping, 1)
}

// adobeOrderings are the character collections whose CIDs litho maps to
// Unicode ([cmap.AdobeCIDToUnicode]).
var adobeOrderings = map[string]bool{"Japan1": true, "GB1": true, "CNS1": true, "Korea1": true}

// prepareComposite reads what planning a composite font takes, and the
// request the provider answers. Its CIDs belong to a
// character collection, and a program another producer made numbers its
// glyphs its own way, so the embedding maps each CID of an Adobe collection
// to the Unicode value the collection gives it and that to the glyph the
// program's Unicode cmap gives it, in a /CIDToGIDMap stream (ISO 32000-1,
// 9.7.4.2, Table 117). That needs TrueType outlines, which a CIDFontType2
// descendant takes: a CIDFontType0 descendant reads a CFF program's glyphs by
// CID, which such a map cannot redirect. An Identity ordering names glyphs of
// a program the file does not carry, so no mapping reaches another program.
func (e *fontEmbedder) prepareComposite(ctx context.Context, font ast.PdfDict, shown []ast.PdfString) (*compositeFont, FontRequest, error) {
	desc := e.descendant(ctx, font)
	if desc == nil {
		return nil, FontRequest{}, fmt.Errorf("%w: a composite font with no descendant CIDFont", ErrFontKind)
	}
	baseFont := nameOf(ctx, e.doc, desc["BaseFont"])
	info := liveDictValue(ctx, e.alloc, e.doc, desc["CIDSystemInfo"])
	registry := stringOf(ctx, e.doc, info["Registry"])
	ordering := stringOf(ctx, e.doc, info["Ordering"])
	if registry != "Adobe" || !adobeOrderings[ordering] {
		return nil, FontRequest{}, fmt.Errorf("%w: the %s-%s character collection maps no CID to a character", ErrFontKind, registry, ordering)
	}
	toUnicode := cmap.AdobeCIDToUnicode(ordering)
	cids := compliance.Type0ShownCIDs(ctx, e.doc, font, shown)
	if len(cids) == 0 {
		return nil, FontRequest{}, fmt.Errorf("%w: the codes the document shows decode to no CID", ErrFontKind)
	}
	fd := liveDictValue(ctx, e.alloc, e.doc, desc["FontDescriptor"])
	req := FontRequest{
		BaseFont: baseFont,
		Subtype:  nameOf(ctx, e.doc, desc["Subtype"]),
		Flags:    int(document.Float(resolved(ctx, e.doc, fd["Flags"]))),
		Encoding: nameOf(ctx, e.doc, font["Encoding"]),
		Ordering: registry + "-" + ordering,
	}
	sorted := slices.Sorted(maps.Keys(cids))
	for _, cid := range sorted {
		req.Codes = append(req.Codes, FontCode{Code: uint32(cid), Unicode: toUnicode[int(cid)], Width: compliance.DeclaredCIDWidth(ctx, e.doc, desc, cid)})
	}
	c := &compositeFont{font: font, desc: desc, fd: fd, baseFont: baseFont, codes: req.Codes, toUnicode: toUnicode}
	return c, req, nil
}

// compositeFont is what planning a composite font reads from the file, which
// [fontEmbedder.buildComposite] turns into a plan for a program.
type compositeFont struct {
	font, desc, fd ast.PdfDict
	baseFont       string
	codes          []FontCode
	toUnicode      map[int]rune
}

// buildComposite plans the embedding of prog for the composite font c,
// refusing a program that lacks a glyph for a shown CID or advances one
// differently from the width the font declares.
func (e *fontEmbedder) buildComposite(c *compositeFont, prog *suppliedProgram) (*embedPlan, error) {
	font, desc, fd, baseFont, toUnicode := c.font, c.desc, c.fd, c.baseFont, c.toUnicode
	if prog.format != outlinesTrueType {
		return nil, fmt.Errorf("%w: a composite font takes glyf outlines here, and %s carries CFF ones", ErrFontKind, prog.psName)
	}
	if len(prog.unicode) == 0 {
		return nil, fmt.Errorf("%w: %s carries no Unicode cmap", ErrFontKind, prog.psName)
	}
	glyphOf := func(cid uint16) uint16 {
		r := toUnicode[int(cid)]
		if r <= 0 || r > 0xFFFF {
			return 0
		}
		return prog.unicode[uint16(r)]
	}
	var missing, off []string
	var gids []uint16
	for _, fc := range c.codes {
		cid := uint16(fc.Code) //nolint:gosec // G115: the codes are CIDs, uint16 values.
		if cid == 0 {
			// CID 0 is .notdef in every collection, which the .notdef rules
			// judge and no program maps.
			continue
		}
		gid := glyphOf(cid)
		if gid == 0 {
			missing = append(missing, describeCode(fc.Code, ""))
			continue
		}
		gids = append(gids, gid)
		if adv, ok := prog.advance(gid); !ok || !compliance.WidthsConsistent(fc.Width, adv) {
			off = append(off, fmt.Sprintf("CID %d declares %g, program %.1f", fc.Code, fc.Width, adv))
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: %s has no glyph for %s", ErrFontCoverage, prog.psName, listCodes(missing))
	}
	if len(off) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrFontWidths, listCodes(off))
	}

	// The map covers every CID of the collection the program draws, so text
	// an edit adds later reaches its glyphs too. CIDs are uint16, which
	// bounds the stream at 128 KiB.
	maxCID := 0
	for cid := range toUnicode {
		if cid > maxCID && cid <= math.MaxUint16 && glyphOf(uint16(cid)) != 0 { //nolint:gosec // G115: cid is bounded to uint16 by the guard.
			maxCID = cid
		}
	}
	cidToGID := make([]byte, 2*(maxCID+1))
	for cid := 1; cid <= maxCID; cid++ {
		gid := glyphOf(uint16(cid)) //nolint:gosec // G115: cid <= maxCID <= MaxUint16.
		cidToGID[2*cid], cidToGID[2*cid+1] = byte(gid>>8), byte(gid)
	}

	descriptor := e.descriptor(fd, baseFont, prog, false)
	delete(descriptor, "CIDSet")
	newDesc := maps.Clone(desc)
	newDesc["Subtype"] = ast.PdfName("CIDFontType2")
	newDesc["CIDToGIDMap"] = ast.NewStream(ast.PdfDict{}, cidToGID)
	stripSubsetTag(newDesc, descriptor)
	descriptor["FontFile2"] = programStream(prog)
	newDesc["FontDescriptor"] = descriptor
	cand := maps.Clone(font)
	cand["DescendantFonts"] = ast.PdfArray{newDesc}
	return &embedPlan{
		font: cand, descriptor: descriptor, descendant: newDesc, program: prog, programKey: "FontFile2", cidToGID: cidToGID, gids: gids,
		rebuild: func(_ context.Context, sub *suppliedProgram) (*embedPlan, error) { return e.buildComposite(c, sub) },
	}, nil
}

// judge refuses a plan whose font fails a rule the unembedded font, judged in
// before, did not fail, or still reads as unembedded.
func (e *fontEmbedder) judge(ctx context.Context, plan *embedPlan, before []compliance.Violation, shown []ast.PdfString) error {
	after := compliance.JudgeFontShowing(ctx, e.doc, plan.font, shown, e.profile)
	if hasViolationOf(after, compliance.FontEmbeddingRule(e.profile)) {
		return fmt.Errorf("%w: validation still reads %s as unembedded", ErrFontRejected, plan.program.psName)
	}
	failed := make(map[compliance.Rule]bool, len(before))
	for _, v := range before {
		failed[v.Rule] = true
	}
	for _, v := range after {
		if failed[v.Rule] {
			continue
		}
		switch v.Rule {
		case compliance.GlyphPresenceRule(e.profile):
			return fmt.Errorf("%w: %s", ErrFontCoverage, v.Error())
		case compliance.GlyphWidthRule(e.profile):
			return fmt.Errorf("%w: %s", ErrFontWidths, v.Error())
		}
		return fmt.Errorf("%w: %s", ErrFontRejected, v.Error())
	}
	return nil
}

// write queues the planned font: the program stream, shared with every font
// of the run given the same program, under the same mapping when it is a
// subset ([fontEmbedder.subsetQueued]), the descriptor at the number the
// font's descriptor had or a new one, with its /CIDSet stream, and the font,
// and for a composite font its descendant and its /CIDToGIDMap. A simple
// font's /Encoding names encoding when it is set
// ([fontEmbedder.encodingObjects]).
func (e *fontEmbedder) write(ctx context.Context, objNum int, font ast.PdfDict, plan *embedPlan, mapping string, encoding *ast.PdfIndirectRef) {
	if plan.subsetGlyphs == 0 {
		mapping = ""
	}
	descriptor := maps.Clone(plan.descriptor)
	descriptor[plan.programKey] = e.programRef(plan.program, mapping)
	if plan.cidSet != nil {
		// ISO 32000-1, Table 124 makes /CIDSet a stream, which is indirect.
		descriptor["CIDSet"] = e.alloc.Alloc(flateStream(ast.PdfDict{}, plan.cidSet))
	}

	holder := font
	if plan.descendant != nil {
		holder = e.descendant(ctx, font)
	}
	descriptorRef := e.place(holder["FontDescriptor"], descriptor)

	if plan.descendant == nil {
		out := maps.Clone(plan.font)
		out["FontDescriptor"] = descriptorRef
		if encoding != nil {
			out["Encoding"] = *encoding
		}
		if plan.toUnicode != nil {
			out["ToUnicode"] = e.alloc.Alloc(flateStream(ast.PdfDict{}, plan.toUnicode))
		}
		e.alloc.AllocAt(objNum, out)
		return
	}
	desc := maps.Clone(plan.descendant)
	desc["FontDescriptor"] = descriptorRef
	desc["CIDToGIDMap"] = e.alloc.Alloc(flateStream(ast.PdfDict{}, plan.cidToGID))
	out := maps.Clone(plan.font)
	var descEntry ast.PdfObject
	if arr := liveArray(ctx, e.alloc, e.doc, font["DescendantFonts"]); len(arr) > 0 {
		descEntry = arr[0]
	}
	out["DescendantFonts"] = ast.PdfArray{e.place(descEntry, desc)}
	e.alloc.AllocAt(objNum, out)
}

// place writes dict at the object number entry refers to, or at a new one
// when entry is no reference: ISO 32000-1, Tables 111, 117 and 121 make a
// font descriptor and a descendant CIDFont indirect references.
func (e *fontEmbedder) place(entry ast.PdfObject, dict ast.PdfDict) ast.PdfIndirectRef {
	if ref, ok := entry.(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 {
		e.alloc.AllocAt(ref.ObjectNum, dict)
		return ast.PdfIndirectRef{ObjectNum: ref.ObjectNum, GenNum: ref.GenNum}
	}
	return e.alloc.Alloc(dict)
}

// programRef returns the object holding prog's program stream, written once
// per program, format and mapping of codes to glyphs
// ([fontEmbedder.codeMapping]).
func (e *fontEmbedder) programRef(prog *suppliedProgram, mapping string) ast.PdfIndirectRef {
	key := programShare{program: programKey{sum: sha256.Sum256(prog.data), format: prog.format}, mapping: mapping}
	if ref, ok := e.programs[key]; ok {
		return ref
	}
	dict := ast.PdfDict{}
	if prog.format == outlinesTrueType {
		// ISO 32000-1, Table 127 requires /Length1, the decoded length, of a
		// TrueType program.
		dict["Length1"] = ast.PdfInt(len(prog.data))
	} else {
		dict["Subtype"] = ast.PdfName("Type1C")
	}
	ref := e.alloc.Alloc(flateStream(dict, prog.data))
	e.programs[key] = ref
	return ref
}

// programStream returns prog's program as an unfiltered stream the judgement
// reads directly.
func programStream(prog *suppliedProgram) ast.PdfStream {
	dict := ast.PdfDict{"Length": ast.PdfInt(len(prog.data))}
	if prog.format == outlinesTrueType {
		dict["Length1"] = ast.PdfInt(len(prog.data))
	} else {
		dict["Subtype"] = ast.PdfName("Type1C")
	}
	return ast.NewStream(dict, prog.data)
}

// flateStream returns data compressed as a FlateDecode stream over dict.
func flateStream(dict ast.PdfDict, data []byte) ast.PdfStream {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	// A bytes.Buffer grows without failing, so neither call can.
	_, _ = w.Write(data)
	_ = w.Close()
	out := maps.Clone(dict)
	out["Filter"] = ast.PdfName("FlateDecode")
	out["Length"] = ast.PdfInt(buf.Len())
	return ast.NewStream(out, buf.Bytes())
}

// descriptor returns the font descriptor the embedded font carries: the
// font's own, its program keys and subset descriptions dropped, with every
// key ISO 32000-1, Table 122 requires filled in from the program where the
// font's leaves it out, or one built from the program when the font had none.
func (e *fontEmbedder) descriptor(fd ast.PdfDict, baseFont string, prog *suppliedProgram, symbolic bool) ast.PdfDict {
	out := maps.Clone(fd)
	if out == nil {
		out = ast.PdfDict{}
	}
	for _, key := range []ast.PdfName{"FontFile", "FontFile2", "FontFile3", "CharSet", "CIDSet"} {
		delete(out, key)
	}
	out["Type"] = ast.PdfName("FontDescriptor")
	out["FontName"] = ast.PdfName(baseFont)
	defaults := ast.PdfDict{
		"Flags":       ast.PdfInt(descriptorFlags(prog, symbolic)),
		"FontBBox":    ast.PdfArray{realOf(prog.bbox[0]), realOf(prog.bbox[1]), realOf(prog.bbox[2]), realOf(prog.bbox[3])},
		"ItalicAngle": realOf(prog.italicAngle),
		"Ascent":      realOf(prog.ascent),
		"Descent":     realOf(prog.descent),
		"CapHeight":   realOf(cmpNonZero(prog.capHeight, prog.ascent)),
		"StemV":       ast.PdfInt(stemV(prog)),
	}
	for key, v := range defaults {
		if _, ok := out[key]; !ok {
			out[key] = v
		}
	}
	return out
}

// descriptorFlags returns the /Flags a program implies (ISO 32000-1, Table
// 123): symbolic or not as the font was, fixed pitch and italic as the
// program states.
func descriptorFlags(prog *suppliedProgram, symbolic bool) int {
	flags := fontFlagNonsymbolic
	if symbolic {
		flags = fontFlagSymbolic
	}
	if prog.fixedPitch {
		flags |= fontFlagFixedPitch
	}
	if prog.italicAngle != 0 {
		flags |= fontFlagItalic
	}
	return flags
}

// Stem widths for a descriptor built from a program. ISO 32000-1, Table 122
// requires /StemV, which only a hinting rasterizer reads, and no sfnt or CFF
// table states it, so the value is the one typical of the program's weight.
const (
	regularStemV = 80
	boldStemV    = 140
)

// stemV returns the /StemV of a descriptor built from prog.
func stemV(prog *suppliedProgram) int {
	if prog.bold {
		return boldStemV
	}
	return regularStemV
}

// cmpNonZero returns a unless it is zero, else b.
func cmpNonZero(a, b float64) float64 {
	if a != 0 {
		return a
	}
	return b
}

// realOf returns v rounded to a whole number of glyph space units.
func realOf(v float64) ast.PdfObject {
	return ast.PdfInt(int(math.Round(v)))
}

// stripSubsetTag drops a subset tag from the font's /BaseFont and the
// descriptor's /FontName (ISO 32000-1, 9.6.4): the tag names a subset the file
// never carried, and [fontEmbedder.markSubset] tags the one litho embeds.
func stripSubsetTag(font, descriptor ast.PdfDict) {
	name, ok := font["BaseFont"].(ast.PdfName)
	if !ok {
		return
	}
	if tag, rest, cut := strings.Cut(string(name), "+"); cut && isSubsetTag(tag) && rest != "" {
		font["BaseFont"] = ast.PdfName(rest)
		descriptor["FontName"] = ast.PdfName(rest)
	}
}

// isSubsetTag reports whether tag is six uppercase letters (ISO 32000-1,
// 9.6.4).
func isSubsetTag(tag string) bool {
	if len(tag) != 6 {
		return false
	}
	for _, c := range tag {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}

// isSymbolStandardFont reports whether baseFont names the Symbol or
// ZapfDingbats standard font, which are symbolic whatever a descriptor says
// (ISO 32000-1, 9.6.2.2).
func isSymbolStandardFont(baseFont string) bool {
	name := baseFont
	if _, rest, ok := strings.Cut(name, "+"); ok {
		name = rest
	}
	return name == "Symbol" || name == "ZapfDingbats"
}

// nameToGID returns the glyph a non-symbolic TrueType font draws for a glyph
// name: the one the program's Unicode cmap gives the name's Unicode value, as
// ISO 32000-1, 9.6.6.4 has a reader look it up, else the one the post table
// names so.
func (p *suppliedProgram) nameToGID(name string) (uint16, bool) {
	if name == "" || name == ".notdef" {
		return 0, false
	}
	if r, ok := encodings.GlyphToRuneFull(name); ok && r > 0 && r <= 0xFFFF {
		if gid, ok := p.unicode[uint16(r)]; ok {
			return gid, true
		}
	}
	gid, ok := p.names[name]
	return gid, ok
}

// declaredWidths is what a simple font declares for its codes.
type declaredWidths struct {
	first   int
	widths  []float64
	missing float64
	// synthesized holds the /FirstChar, /LastChar and /Widths entries a
	// standard font with none receives, nil otherwise.
	synthesized ast.PdfDict
}

// at returns the width declared for code: /Widths when it covers the code,
// else the descriptor's /MissingWidth (ISO 32000-1, Table 111).
func (d declaredWidths) at(code byte) float64 {
	if i := int(code) - d.first; i >= 0 && i < len(d.widths) {
		return d.widths[i]
	}
	return d.missing
}

// declaredWidths reads the widths a simple font declares. A standard font may
// declare none (ISO 32000-1, 9.6.2.1 lets its /Widths go before PDF 1.5), and a
// reader then draws it with the widths of its Adobe Font Metrics: those are
// the widths its text is positioned with, so the embedded font declares them.
// Any other font that declares none is refused, since nothing says where its
// text sits.
func (e *fontEmbedder) declaredWidths(ctx context.Context, font, fd ast.PdfDict, baseFont string, names map[byte]string) (declaredWidths, error) {
	missing := document.Float(resolved(ctx, e.doc, fd["MissingWidth"]))
	if _, ok := font["Widths"]; ok {
		arr := liveArray(ctx, e.alloc, e.doc, font["Widths"])
		first := int(document.Float(resolved(ctx, e.doc, font["FirstChar"])))
		d := declaredWidths{first: first, missing: missing}
		for _, w := range arr {
			d.widths = append(d.widths, document.Float(resolved(ctx, e.doc, w)))
		}
		return d, nil
	}
	if !typography.IsStandard14(baseFont) || names == nil {
		return declaredWidths{}, fmt.Errorf("%w: %s declares no /Widths", ErrFontWidths, baseFont)
	}
	first, last := -1, -1
	for code := range 256 {
		if _, ok := typography.Standard14Width(baseFont, names[byte(code)]); ok {
			if first < 0 {
				first = code
			}
			last = code
		}
	}
	if first < 0 {
		return declaredWidths{}, fmt.Errorf("%w: %s names no glyph its metrics give", ErrFontWidths, baseFont)
	}
	d := declaredWidths{first: first, missing: missing}
	arr := make(ast.PdfArray, 0, last-first+1)
	for code := first; code <= last; code++ {
		w, _ := typography.Standard14Width(baseFont, names[byte(code)]) //nolint:gosec // G115: code is bounded to 0..255 by the loop.
		d.widths = append(d.widths, w)
		arr = append(arr, ast.PdfInt(int(w)))
	}
	d.synthesized = ast.PdfDict{"FirstChar": ast.PdfInt(first), "LastChar": ast.PdfInt(last), "Widths": arr}
	return d, nil
}

// simpleEncodingName returns the predefined encoding a simple font's
// /Encoding or its /BaseEncoding names, or "".
func simpleEncodingName(ctx context.Context, doc *document.Document, font ast.PdfDict) string {
	if name := nameOf(ctx, doc, font["Encoding"]); name != "" {
		return name
	}
	enc, err := document.ResolveDict(ctx, doc, font["Encoding"])
	if err != nil || enc == nil {
		return ""
	}
	return nameOf(ctx, doc, enc["BaseEncoding"])
}

// trueTypeEncoding returns the /Encoding a Type 1 font that becomes a
// non-symbolic TrueType font takes: the font's own when it names
// WinAnsiEncoding or MacRomanEncoding, the two ISO 19005-2, 6.2.11.6 admits,
// else WinAnsiEncoding, with /Differences naming each code the document shows
// whose name differs from it, so every shown code keeps its glyph name. A code
// no stream shows takes the name WinAnsiEncoding gives it: widening
// /Differences to it would fail ISO 19005-1, 6.3.7 t1, which admits no
// /Differences on a TrueType font, for a glyph nothing draws.
func trueTypeEncoding(ctx context.Context, doc *document.Document, font ast.PdfDict, names map[byte]string, shown map[byte]bool) ast.PdfObject {
	if name := nameOf(ctx, doc, font["Encoding"]); name == "WinAnsiEncoding" || name == "MacRomanEncoding" {
		return ast.PdfName(name)
	}
	diffs := differences(names, compliance.PredefinedEncodingNames("WinAnsiEncoding"), shown)
	if len(diffs) == 0 {
		return ast.PdfName("WinAnsiEncoding")
	}
	return ast.PdfDict{"Type": ast.PdfName("Encoding"), "BaseEncoding": ast.PdfName("WinAnsiEncoding"), "Differences": diffs}
}

// type1CEncoding returns the /Encoding a Type 1 font over a CFF program takes
// so every code the document shows keeps the glyph name it had, or nil to keep
// the font's own. A predefined encoding the font names stays. Otherwise the
// embedded program's built-in encoding becomes the implicit base (ISO 32000-1,
// Table 114), so every shown code whose name differs from it goes into
// /Differences.
func type1CEncoding(ctx context.Context, doc *document.Document, font ast.PdfDict, names, builtin map[byte]string, shown map[byte]bool) ast.PdfObject {
	if name := nameOf(ctx, doc, font["Encoding"]); compliance.PredefinedEncodingNames(name) != nil {
		return nil
	}
	diffs := differences(names, builtin, shown)
	if len(diffs) == 0 {
		return nil
	}
	return ast.PdfDict{"Type": ast.PdfName("Encoding"), "Differences": diffs}
}

// differences returns a /Differences array naming every shown code whose name
// in names differs from its name in base (ISO 32000-1, 9.6.6.1), in code
// order.
func differences(names, base map[byte]string, shown map[byte]bool) ast.PdfArray {
	var out ast.PdfArray
	next := -1
	for code := range 256 {
		name := names[byte(code)]                                         //nolint:gosec // G115: code is bounded to 0..255 by the loop.
		if !shown[byte(code)] || name == "" || name == base[byte(code)] { //nolint:gosec // G115: code is bounded to 0..255 by the loop.
			continue
		}
		if code != next {
			out = append(out, ast.PdfInt(code))
		}
		out = append(out, ast.PdfName(name))
		next = code + 1
	}
	return out
}

// describeCode names a code for a refusal.
func describeCode(code uint32, glyph string) string {
	if glyph == "" {
		return fmt.Sprintf("code %d", code)
	}
	return fmt.Sprintf("code %d (%s)", code, glyph)
}

// listCodes joins the first [maxListedCodes] entries and counts the rest.
func listCodes(items []string) string {
	if len(items) <= maxListedCodes {
		return strings.Join(items, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(items[:maxListedCodes], ", "), len(items)-maxListedCodes)
}

// hasViolationOf reports whether vs holds a violation of rule.
func hasViolationOf(vs []compliance.Violation, rule compliance.Rule) bool {
	if rule.Specification == "" {
		return false
	}
	for _, v := range vs {
		if v.Rule == rule {
			return true
		}
	}
	return false
}

// nameOf returns obj resolved as a name, "" when it is none.
func nameOf(ctx context.Context, doc *document.Document, obj ast.PdfObject) string {
	name, err := document.ResolveName(ctx, doc, obj)
	if err != nil {
		return ""
	}
	return string(name)
}

// stringOf returns obj resolved as a string, "" when it is none.
func stringOf(ctx context.Context, doc *document.Document, obj ast.PdfObject) string {
	s, err := document.ResolveString(ctx, doc, obj)
	if err != nil {
		return ""
	}
	return string(s)
}

// resolved returns obj with a reference followed, nil when it cannot be.
func resolved(ctx context.Context, doc *document.Document, obj ast.PdfObject) ast.PdfObject {
	r, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return nil
	}
	return r
}
