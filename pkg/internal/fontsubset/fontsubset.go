package fontsubset

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"maps"
	"reflect"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/typography/cff"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// Config configures [Run].
type Config struct {
	// Decoder decodes the content streams, CMaps and font programs the pass
	// reads. A nil Decoder decodes with the built-in filters under the
	// default budgets.
	Decoder *filter.Decoder
	// Limits bounds the pass: MaxStreamBytes bounds what one decode holds,
	// MaxPageContentBytes the content of one page, and MaxFontProgCacheBytes
	// how many subset program bytes the pass keeps in memory (see [Run]).
	Limits limits.Limits
	// Logger receives, at Debug, why each program is left whole. Nil logs
	// nothing.
	Logger *slog.Logger
}

// Stats reports what [Run] changed.
type Stats struct {
	// Programs is the number of font programs subset.
	Programs int
	// SavedBytes is how many bytes the subset programs are smaller by, as
	// serialized.
	SavedBytes int64
}

// Run subsets, in place, the embedded TrueType and CFF font programs of the
// object graph an allocator holds, keeping the glyphs the document can show:
// every glyph a text-showing operator of a content stream the document draws
// can select through each font drawing from the program, the .notdef glyph,
// and the glyphs a kept glyph draws (TrueType composite components, CFF seac
// accents). Every other glyph keeps its glyph ID and loses its outline
// ([truetype.SubsetInPlace], [cff.SubsetInPlace]), so no reference into the
// program changes: /Widths, /W, /FontBBox, /ToUnicode, /CIDToGIDMap, /CIDSet
// and /CharSet describe the subset as they described the whole program. The
// font's name gains no subset tag (ISO 32000-1, 9.6.4): the program still
// holds every glyph ID, and the tag would make ISO 19005-1, 6.3.5 ask a
// /CharSet or /CIDSet of a font that never needed one.
//
// The walk interprets every content stream the page tree reaches: each page's
// content, its annotations' appearances and the icons of its widgets, and,
// for every resource dictionary those streams draw with, its forms, tiling
// patterns, Type 3 glyph procedures and soft-mask groups, whether a stream
// invokes them or not. A program is subset only when the walk can prove it
// read every use of it: every object holding the program, its descriptor,
// its fonts, their /Font dictionaries and their resource dictionaries is one
// the walk reached, through the entries ISO 32000-1 gives them. It is left
// whole, with a Debug log saying why, when anything else holds one of them;
// when a font is named by a default appearance string, which a reader filling
// a field types with (12.7.3.3); when a widget's appearance draws with it
// under /NeedAppearances; when a stream drawing with it does not decode or
// parse; when the program is OpenType, Type 1, or a format litho cannot read
// whole; and when its subset is not smaller as serialized. The whole pass
// subsets nothing in a document holding a signature, whose signed bytes a
// rewrite already changes but whose fonts a signer chose, in one with an XFA
// form, whose text a reader draws outside every content stream (12.7.8), and
// when a stream that is not a page's shows text with a font it inherits from
// whatever draws it.
//
// A program is replaced by its subset compressed with FlateDecode, its
// dictionary keeping every entry but /Filter, /DecodeParms and /DL, and
// /Length1 giving the subset's length where the program carried one. Every
// subset is decoded again and read back before it replaces the program: its
// glyph count, cmap subtables, glyph names, charset and built-in encoding
// must be the program's.
//
// Subset bytes are kept in memory up to [limits.Limits.MaxFontProgCacheBytes]
// in total. Past it, a program's subset is redone from its source when the
// stream is serialized. Programs are visited in object-number order and every
// step depends on the bytes alone, so the output does too.
//
// The pass must run before encryption, which replaces every stream's data
// with ciphertext.
//
// # Corpus evidence
//
//   - All corpora: 1,278 of 5,280 files have a program subset, 2,128
//     TrueType, 1,484 name-keyed CFF and 154 CID-keyed CFF. Each output
//     extracts the same text as the plain rewrite, reports the same PDF/A-1b
//     and PDF/A-2b rules, renders the same in poppler on its first 50 pages,
//     passes qpdf --check as the plain rewrite does, and comes out the same
//     on a second run.
//   - All corpora: the rewrite is 52.5% smaller on facturx (56.8 to 27.0
//     MB), 7.3% on pdfjs, 2.7% on pdfcpu and verapdf, 1.8% on litho and 1.4%
//     on govdocs; pdf20 has nothing to subset. Most programs left whole are
//     already subsets, whose in-place subset is not smaller.
//   - 43 plain and subset pairs from facturx, litho, pdfjs, govdocs and
//     verapdf, judged by veraPDF 1.30.2 at their own flavour: the same rules
//     fail as often.
//   - pdfjs Brotli-Prototype-FileA.pdf: the one rule that moves. Three
//     programs stored under BrotliDecode, which ISO 19005-2, 6.1.7.2 does not
//     list, come out under FlateDecode, so 6.1.7.2 fires three times fewer.
//   - pdfjs issue14990.pdf: page 33 renders differently on every poppler run
//     of the plain rewrite too, so its render difference is poppler's.
func Run(ctx context.Context, alloc *writer.ObjAllocator, trailer ast.PdfDict, cfg Config) (Stats, error) {
	var stats Stats
	lim := cfg.Limits.WithDefaults()
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	pending := alloc.Pending()
	g, err := newGraph(ctx, pending, trailer)
	if err != nil {
		return stats, err
	}
	switch {
	case g.incomplete != "":
		logger.Debug("font subsetting: no font subset", slog.String("reason", g.incomplete))
		return stats, nil
	case g.signed:
		logger.Debug("font subsetting: no font subset", slog.String("reason", "the document holds a signature"))
		return stats, nil
	}
	w, err := newWalk(ctx, g, cfg.Decoder, lim)
	if err != nil {
		return stats, err
	}
	if w.aborted != "" {
		logger.Debug("font subsetting: no font subset", slog.String("reason", w.aborted))
		return stats, nil
	}

	groups, err := programGroups(ctx, g)
	if err != nil {
		return stats, err
	}
	var retained int64
	for _, group := range groups {
		if err := ctx.Err(); err != nil {
			return stats, fmt.Errorf("subsetting fonts: %w", err)
		}
		plan, reason, err := w.planProgram(ctx, group)
		if err != nil {
			return stats, fmt.Errorf("subsetting font program %d: %w", group[0], err)
		}
		if reason != "" {
			logger.Debug("font subsetting: program left whole", slog.Any("objects", group), slog.String("reason", reason))
			continue
		}
		// The members of a group share one subset, so the encoding is held
		// once whatever their number.
		var replacement ast.PdfStream
		if size := int64(len(plan.encoded)); retained+size <= lim.MaxFontProgCacheBytes() {
			retained += size
			replacement = ast.NewStream(plan.dict, plan.encoded)
		} else {
			replacement = ast.NewLazyStream(plan.dict, plan.redo(w.g, w.dec))
		}
		logger.Debug("font subsetting: program subset", slog.Any("objects", group),
			slog.String("kind", plan.kind.String()), slog.Bool("cid_keyed", plan.cidKeyed),
			slog.Int("glyphs_kept", len(plan.gids)), slog.Int64("saved_bytes", plan.saved))
		for _, num := range group {
			alloc.AllocAt(num, replacement)
			stats.Programs++
			stats.SavedBytes += plan.saved
		}
	}
	return stats, nil
}

// programGroups returns the objects a font descriptor entry holds as an
// embedded program, grouped by their serialized bytes, each group and the
// groups in object-number order. The programs of one group share one subset,
// keeping the glyphs every font drawing from any of them shows: a subset of
// its own for each would make byte-identical programs differ, which stream
// dedup ([writer.DedupStreams]) then cannot merge, and a document embedding
// one program several times would grow.
//
// # Corpus evidence
//
//   - litho glyphs_2b.pdf: two fonts draw from byte-identical programs. A
//     subset of its own for each grew the output of flatten-form --compact
//     from 2,066 to 2,561 bytes, since dedup no longer merged them.
func programGroups(ctx context.Context, g *graph) ([][]int, error) {
	var nums []int
	for num, slots := range g.refs {
		for _, s := range slots {
			if _, key := s.split(); programKeys[key] {
				if _, isStream := g.pending[num].(ast.PdfStream); isStream {
					nums = append(nums, num)
				}
				break
			}
		}
	}
	slices.Sort(nums)
	byKey := make(map[[sha256.Size]byte]int)
	var groups [][]int
	for _, num := range nums {
		h := sha256.New()
		if _, err := writer.Serialize(ctx, h, g.pending[num]); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, fmt.Errorf("grouping font programs: %w", ctxErr)
			}
			// A program that does not serialize stays in a group of its own:
			// planning it fails the same way and leaves it whole.
			groups = append(groups, []int{num})
			continue
		}
		var key [sha256.Size]byte
		copy(key[:], h.Sum(nil))
		if i, ok := byKey[key]; ok {
			groups[i] = append(groups[i], num)
			continue
		}
		byKey[key] = len(groups)
		groups = append(groups, []int{num})
	}
	return groups, nil
}

// subsetPlan is the subset [walk.planProgram] chose for one program.
type subsetPlan struct {
	source ast.PdfStream
	kind   programKind
	// cidKeyed reports a CID-keyed CFF program.
	cidKeyed bool
	gids     []uint16
	dict     ast.PdfDict
	encoded  []byte
	saved    int64
}

// planProgram returns the subset the byte-identical program objects of group
// share, or why they stay whole. The error is only ever the context's.
func (w *walk) planProgram(ctx context.Context, group []int) (subsetPlan, string, error) {
	s, _ := w.g.pending[group[0]].(ast.PdfStream)
	var users []programUser
	for _, num := range group {
		u, reason := w.programUsers(num)
		if reason != "" {
			return subsetPlan{}, reason, nil
		}
		users = append(users, u...)
	}
	for _, u := range users {
		if reason := w.fontSeen(u.top); reason != "" {
			return subsetPlan{}, reason, nil
		}
	}
	data, ok, err := decode(ctx, w.g, w.dec, s)
	if err != nil || !ok {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return subsetPlan{}, "", ctxErr
		}
		return subsetPlan{}, "the program does not decode whole", nil
	}
	kind, ok := kindOf(data)
	if !ok {
		return subsetPlan{}, "an OpenType, Type 1 or unknown program", nil
	}
	wantKey := map[programKind]string{kindTrueType: "FontFile2", kindCFF: "FontFile3"}[kind]
	for _, num := range group {
		for _, h := range w.g.holders(loc{obj: num}) {
			if h.key != wantKey {
				return subsetPlan{}, fmt.Sprintf("a program held by /%s, which names another format than its bytes", h.key), nil
			}
		}
	}
	p, err := readProgram(ctx, data, kind)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return subsetPlan{}, "", ctxErr
		}
		return subsetPlan{}, fmt.Sprintf("the program does not read: %v", err), nil
	}
	keep := glyphSet{0: true}
	for _, u := range users {
		use := w.uses[u.top.at]
		if use == nil {
			continue
		}
		if u.font.at == u.top.at {
			if p.byCID != nil {
				return subsetPlan{}, "a simple font drawing from a CID-keyed program", nil
			}
			w.simpleGlyphs(u.font, p, use, keep)
			continue
		}
		if reason := w.compositeGlyphs(ctx, u.font, u.top, p, use, keep); reason != "" {
			return subsetPlan{}, reason, nil
		}
	}
	gids := make([]uint16, 0, len(keep))
	for gid := range keep {
		gids = append(gids, gid)
	}
	slices.Sort(gids)

	plan := subsetPlan{source: s, kind: kind, cidKeyed: p.byCID != nil, gids: gids}
	sub, encoded, dict, reason, err := plan.build(ctx, data)
	if err != nil || reason != "" {
		return subsetPlan{}, reason, err
	}
	again, err := readProgram(ctx, sub, kind)
	if err != nil || !reflect.DeepEqual(again, p) {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return subsetPlan{}, "", ctxErr
		}
		return subsetPlan{}, "the subset does not read as the program does", nil
	}
	before, err := serializedSize(ctx, s)
	if err != nil {
		return subsetPlan{}, "", ctx.Err()
	}
	after, err := serializedSize(ctx, ast.NewStream(dict, encoded))
	if err != nil {
		return subsetPlan{}, "", ctx.Err()
	}
	if after >= before {
		return subsetPlan{}, "the subset is not smaller", nil
	}
	decoded, ok, err := writer.DecodeWhole(ctx, w.dec, ast.NewStream(dict, encoded))
	if err != nil || !ok || !bytes.Equal(decoded, sub) {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return subsetPlan{}, "", ctxErr
		}
		return subsetPlan{}, "the compressed subset does not decode to the subset", nil
	}
	plan.dict, plan.encoded, plan.saved = dict, encoded, before-after
	return plan, "", nil
}

// build subsets data, the program's decoded bytes, and returns the subset,
// its FlateDecode encoding and the stream dictionary describing it, or why
// the program cannot be subset.
func (p subsetPlan) build(ctx context.Context, data []byte) ([]byte, []byte, ast.PdfDict, string, error) {
	var (
		sub []byte
		err error
	)
	switch p.kind {
	case kindTrueType:
		sub, err = truetype.SubsetInPlace(ctx, data, p.gids)
	case kindCFF:
		sub, err = cff.SubsetInPlace(ctx, data, p.gids)
	}
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, nil, nil, "", ctxErr
		}
		return nil, nil, nil, fmt.Sprintf("the program cannot be subset: %v", err), nil
	}
	var buf bytes.Buffer
	zw, err := zlib.NewWriterLevel(&buf, zlib.BestCompression)
	if err != nil {
		return nil, nil, nil, "", fmt.Errorf("compressing a subset: %w", err)
	}
	if _, err := zw.Write(sub); err != nil {
		return nil, nil, nil, "", fmt.Errorf("compressing a subset: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, nil, nil, "", fmt.Errorf("compressing a subset: %w", err)
	}
	dict := maps.Clone(p.source.Dict)
	dict["Filter"] = ast.PdfName("FlateDecode")
	delete(dict, "DecodeParms")
	delete(dict, "DL")
	if _, has := dict["Length1"]; has || p.kind == kindTrueType {
		// ISO 32000-1, 9.9, Table 127 gives a FontFile2 stream the length of
		// its decoded program.
		dict["Length1"] = ast.PdfInt(len(sub))
	}
	return sub, buf.Bytes(), dict, "", nil
}

// redo returns a loader that redoes the plan's subset from the program's
// source bytes, for a program [Run] did not keep in memory. The result must
// have the length the plan measured, since the pass chose it on that size.
func (p subsetPlan) redo(g *graph, dec *filter.Decoder) func(context.Context) ([]byte, error) {
	want := len(p.encoded)
	return func(ctx context.Context) ([]byte, error) {
		data, ok, err := decode(ctx, g, dec, p.source)
		if err != nil {
			return nil, fmt.Errorf("decoding a subset program's source: %w", err)
		}
		if !ok {
			return nil, fmt.Errorf("%w: its source no longer decodes whole", ErrSubsetDiverged)
		}
		_, encoded, _, reason, err := p.build(ctx, data)
		if err != nil {
			return nil, err
		}
		if reason != "" || len(encoded) != want {
			return nil, fmt.Errorf("%w: %d bytes where %d were measured", ErrSubsetDiverged, len(encoded), want)
		}
		return encoded, nil
	}
}

// serializedSize returns how many bytes s takes as written, dictionary and
// data together, without holding the serialization.
func serializedSize(ctx context.Context, s ast.PdfStream) (int64, error) {
	n, err := writer.Serialize(ctx, byteCounter{}, s)
	if err != nil {
		return 0, fmt.Errorf("measuring a stream: %w", err)
	}
	return n, nil
}

// byteCounter is an io.Writer that discards what it is given.
type byteCounter struct{}

// Write discards p.
func (byteCounter) Write(p []byte) (int, error) { return len(p), nil }
