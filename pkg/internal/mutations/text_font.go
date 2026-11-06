package mutations

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/typography/embed"
)

// TextFontCandidate reads one font program a [TextFontSource] offers.
type TextFontCandidate func(ctx context.Context) ([]byte, error)

// TextFontRequest describes the font a line of text asks for.
type TextFontRequest struct {
	// StandardFont is the standard 14 font the text was asked in, such as
	// Helvetica-Bold, which a source matches by family and style; "" stands
	// for Helvetica.
	StandardFont string
	// Text is the line to draw. Every character of it has to have a glyph in
	// the program that draws it.
	Text string
}

// TextFontSource offers the programs that may draw a line of text, best
// first: TrueType programs, or OpenType programs with CFF outlines. A program
// that does not cover every character of the text gives way to the next one.
// A source shared by concurrent edits must be safe for concurrent use.
type TextFontSource func(ctx context.Context, req TextFontRequest) ([]TextFontCandidate, error)

// maxTextFontCandidates bounds the programs one line of text tries. Anti-DoS
// guard, not spec-mandated: a system ships a few hundred font files, and a
// line's coverage is settled by the first one that has its glyphs.
const maxTextFontCandidates = 4096

// maxUncoveredListed bounds how many characters a refusal names.
const maxUncoveredListed = 8

// textFonts embeds, once per program, the fonts the lines of text of one
// edit draw with. A line asks [textFonts.draw] for its font, which reserves
// the font's object and records the glyphs the line uses; [textFonts.finish]
// then writes each font subset to every glyph recorded, so two lines drawn
// with the same program share one embedded subset.
type textFonts struct {
	source TextFontSource
	fonts  []*sharedTextFont
	// loaded caches the programs parsed, by the hash of their bytes, so a
	// program the source offers for every line is read once.
	loaded map[[32]byte]*embed.Font
}

// sharedTextFont is one program the edit draws with: the font object it
// is reached by and the subset recording the glyphs its lines draw (see
// [embed.Subset]).
type sharedTextFont struct {
	font *embed.Font
	sub  *embed.Subset
	ref  ast.PdfIndirectRef
}

// encode records the glyphs text draws and returns text as a hex string of
// the codes that select them. Each glyph is recorded with the character it
// is drawn for, which its ToUnicode entry names: a glyph two characters
// share, as a CJK ideograph and its Kangxi radical do, extracts as the one
// the text held, not as the first the program's cmap lists. A character the
// program lacks draws .notdef and names nothing.
func (s *sharedTextFont) encode(text string) string {
	var b strings.Builder
	b.WriteString("<")
	for _, r := range text {
		gid, ok := s.font.GID(r)
		chars := string(r)
		if !ok {
			gid, chars = 0, ""
		}
		fmt.Fprintf(&b, "%04X", s.sub.Code(gid, chars))
	}
	b.WriteString(">")
	return b.String()
}

// newTextFonts returns the fonts of one edit, offered by source, which may
// be nil: every line then fails with [ErrNoFontCovers].
func newTextFonts(source TextFontSource) *textFonts {
	return &textFonts{source: source, loaded: make(map[[32]byte]*embed.Font)}
}

// drawnText is what a line of text draws with: the font object, the name it
// is reached by in a resource dictionary, and the text encoded for it.
type drawnText struct {
	ref     ast.PdfIndirectRef
	resName string
	encoded string
	// font is the program that draws the text, which a placed stamp measures.
	font *textFont
}

// draw returns the font that draws req's text: a font of an earlier line
// when its program covers the text, else the first program the source offers
// that does. It fails with [ErrNoFontCovers], naming the characters no
// program has, when none does.
func (t *textFonts) draw(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, req TextFontRequest) (drawnText, error) {
	for _, s := range t.fonts {
		if len(s.font.Uncovered(req.Text)) == 0 {
			return t.use(s, req.Text), nil
		}
	}
	var candidates []TextFontCandidate
	if t.source != nil {
		var err error
		candidates, err = t.source(ctx, req)
		if err != nil {
			return drawnText{}, fmt.Errorf("offering fonts for the text: %w", err)
		}
	}
	if len(candidates) > maxTextFontCandidates {
		candidates = candidates[:maxTextFontCandidates]
	}
	uncovered := distinctRunes(req.Text)
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return drawnText{}, err
		}
		data, err := candidate(ctx)
		if err != nil {
			return drawnText{}, fmt.Errorf("reading a font program: %w", err)
		}
		if len(data) == 0 {
			continue
		}
		font, err := t.load(doc, data)
		if err != nil {
			continue
		}
		missing := font.Uncovered(req.Text)
		if len(missing) == 0 {
			s := &sharedTextFont{font: font, sub: font.NewSubset(), ref: alloc.Alloc(ast.PdfNull{})}
			t.fonts = append(t.fonts, s)
			return t.use(s, req.Text), nil
		}
		if len(missing) < len(uncovered) {
			uncovered = missing
		}
	}
	return drawnText{}, fmt.Errorf("%w: %s", ErrNoFontCovers, describeRunes(uncovered))
}

// use records the glyphs text draws with s and returns the drawing.
func (t *textFonts) use(s *sharedTextFont, text string) drawnText {
	return drawnText{
		ref:     s.ref,
		resName: fmt.Sprintf("TextF%d", s.ref.ObjectNum),
		encoded: s.encode(text),
		font:    s.font,
	}
}

// load parses data, once per distinct program.
func (t *textFonts) load(doc *document.Document, data []byte) (*embed.Font, error) {
	key := sha256.Sum256(data)
	if f, ok := t.loaded[key]; ok {
		return f, nil
	}
	f, err := embed.Load(data)
	if err != nil {
		if doc != nil {
			doc.Logger.Debug("text font: skipping a program that does not parse", "err", err)
		}
		return nil, err
	}
	t.loaded[key] = f
	return f, nil
}

// finish writes every font drawn with, subset to the glyphs recorded.
func (t *textFonts) finish(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
	for _, s := range t.fonts {
		if err := ctx.Err(); err != nil {
			return err
		}
		s.sub.Write(ctx, alloc, textFontLogger(doc), s.ref)
	}
	return nil
}

// distinctRunes returns the characters of text in order, each once.
func distinctRunes(text string) []rune {
	var out []rune
	seen := make(map[rune]bool)
	for _, r := range text {
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	return out
}

// describeRunes names runes for a message: the first few quoted, the rest
// counted.
func describeRunes(runes []rune) string {
	if len(runes) == 0 {
		return "every character"
	}
	parts := make([]string, 0, maxUncoveredListed+1)
	for i, r := range runes {
		if i == maxUncoveredListed {
			parts = append(parts, fmt.Sprintf("and %d more", len(runes)-i))
			break
		}
		parts = append(parts, fmt.Sprintf("%q", r))
	}
	return "no font has " + strings.Join(parts, ", ")
}

// textFontLogger returns the logger font embedding reports through: doc's,
// or nil, which discards, when there is no document.
func textFontLogger(doc *document.Document) *slog.Logger {
	if doc == nil {
		return nil
	}
	return doc.Logger
}
