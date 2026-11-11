package plaintext

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// maxParagraphRunes bounds the characters one [text.Fonts.Layout] call lays
// out. Anti-DoS guard, not spec-mandated: shaping holds a few hundred bytes a
// character, so a source of one huge line would otherwise ask for gigabytes
// in one call. A longer source line is laid out in pieces of this length,
// each starting a new line. 65,536 characters fill some twenty pages, far
// past any line a person writes.
const maxParagraphRunes = 1 << 16

// maxMissingGlyphWarnings bounds the distinct characters reported as
// missing. Anti-DoS guard, not spec-mandated: a source can hold every code
// point once, and a warning a character would then outweigh the document.
// One more warning says the list stops.
const maxMissingGlyphWarnings = 256

// Warning codes [Convert] reports.
const (
	// CodeMissingGlyph flags a character no indexed face covers, drawn as
	// the matched face's .notdef glyph, or, once per document, emoji no
	// indexed face covers, drawn blank.
	CodeMissingGlyph = "missing-glyph"
	// CodeLineSplit flags a source line longer than [maxParagraphRunes]
	// characters, laid out in pieces.
	CodeLineSplit = "line-split"
)

// bom is the UTF-8 encoding of U+FEFF, the byte order mark.
var bom = []byte{0xEF, 0xBB, 0xBF}

// Convert lays src out as a Document.
//
// It reads src to its end, at most o.MaxInputBytes bytes, and returns an
// error wrapping [ErrInputTooLarge] past them; src is never closed. It
// returns an error wrapping [ErrInvalidOptions] for opts out of range or a
// nil fonts, [text.ErrNoFonts] when fonts indexed no face, [ErrTooManyPages]
// when the layout needs more than o.MaxPages pages, and ctx's error when ctx
// ends first.
//
// The document always has at least one page, blank for an empty source. Its
// output depends on the bytes of src, opts and the faces fonts indexed alone.
// See the package documentation for the layout rules.
//
// With [Options.Sink], each page goes to the sink once the next one starts
// or the source ends, its glyphs given back to the conversion's memory
// budget, and the document returned holds no page: its warnings name the
// pages in the order they were handed over. An error the sink returns ends
// the conversion with it.
func Convert(ctx context.Context, src io.Reader, fonts *text.Fonts, opts Options) (*compose.Document, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	o, err := opts.withDefaults()
	if err != nil {
		return nil, err
	}
	if fonts == nil {
		return nil, fmt.Errorf("%w: no font index", ErrInvalidOptions)
	}
	data, err := readCapped(src, o.MaxInputBytes)
	if err != nil {
		return nil, err
	}

	style := text.Style{Families: o.FontFamilies, Size: o.FontSize, Color: compose.Black, Lang: o.DefaultLang}
	face := fonts.Match(style)
	if face == nil {
		return nil, fmt.Errorf("matching %s: %w", strings.Join(o.FontFamilies, ", "), text.ErrNoFonts)
	}
	m := face.Metrics(o.FontSize)
	l := &layouter{
		fonts:      fonts,
		opts:       o,
		style:      style,
		ascent:     m.Ascent,
		descent:    m.Descent,
		lineHeight: o.FontSize * o.LineHeight,
		width:      o.PageWidth - o.MarginLeft - o.MarginRight,
		bottom:     o.PageHeight - o.MarginBottom,
		root:       &compose.StructElem{Role: "Document"},
		seen:       make(map[rune]bool),
		mem:        limits.BudgetFrom(ctx),
	}
	if err := l.run(ctx, data); err != nil {
		return nil, err
	}
	if l.pages == 0 {
		if err := l.newPage(ctx); err != nil {
			return nil, err
		}
	}
	if err := l.handOver(ctx); err != nil {
		return nil, err
	}
	l.doc.Structure = l.root
	l.doc.Metadata.Lang = o.DefaultLang
	return &l.doc, nil
}

// readCapped reads src to its end, failing with [ErrInputTooLarge] past max
// bytes.
func readCapped(src io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(src, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading the plain text source: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%w: the source is longer than %d bytes", ErrInputTooLarge, maxBytes)
	}
	return data, nil
}

// layouter flows paragraphs down pages. It lives for one [Convert] call.
type layouter struct {
	fonts *text.Fonts
	opts  Options
	style text.Style

	ascent, descent, lineHeight float64
	// width is the line width, bottom the lowest a line's descent may reach.
	width, bottom float64

	doc compose.Document
	// page is the page being filled, nil before the first and once handed
	// over; pages counts the pages begun.
	page  *compose.Page
	pages int
	// mem is the conversion's budget, which the glyphs of a page handed
	// over are given back to.
	mem *limits.Budget
	// next is the baseline of the next line on page, and onPage the lines
	// already placed on it.
	next   float64
	onPage int
	// breaks counts the form feeds met since the last line placed.
	breaks int

	root *compose.StructElem
	// para is the P element of the source line being laid out, nil until it
	// places a run.
	para *compose.StructElem

	seen map[rune]bool
	// emojiWarned reports that the missing emoji warning was given.
	emojiWarned bool
	truncated   bool
}

// run walks the decoded source and lays out every source line.
func (l *layouter) run(ctx context.Context, data []byte) error {
	data = bytes.TrimPrefix(data, bom)
	var (
		buf strings.Builder
		// runes counts the characters in buf, col the column of the source
		// line the next character takes, for tab stops.
		runes, col int
		// formFed records a form feed on the current source line, whose
		// empty pieces take no line.
		formFed bool
	)
	flush := func() error {
		if buf.Len() == 0 {
			return nil
		}
		err := l.paragraph(ctx, buf.String())
		buf.Reset()
		runes = 0
		return err
	}
	endLine := func() error {
		if buf.Len() == 0 && !formFed {
			if err := l.place(ctx, nil); err != nil {
				return err
			}
		}
		err := flush()
		l.para = nil
		col, formFed = 0, false
		return err
	}

	for i := 0; i < len(data); {
		r, size := utf8.DecodeRune(data[i:])
		i += size
		// A lone invalid byte decodes as RuneError of size 1; a valid
		// U+FFFD is three bytes. Both read as U+FFFD.
		switch r {
		case '\r':
			if i < len(data) && data[i] == '\n' {
				i++
			}
			fallthrough
		case '\n':
			if err := endLine(); err != nil {
				return err
			}
			continue
		case '\f':
			if err := flush(); err != nil {
				return err
			}
			l.para = nil
			l.breaks++
			col, formFed = 0, true
			continue
		case '\t':
			n := l.opts.TabWidth - col%l.opts.TabWidth
			buf.WriteString(strings.Repeat(" ", n))
			runes += n
			col += n
		default:
			buf.WriteRune(r)
			runes++
			col++
		}
		if runes >= maxParagraphRunes {
			if err := flush(); err != nil {
				return err
			}
			l.doc.Warnings = append(l.doc.Warnings, compose.Warning{
				Code:    CodeLineSplit,
				Message: fmt.Sprintf("a source line longer than %d characters was laid out in pieces, each starting a new line", maxParagraphRunes),
				Page:    l.pages,
			})
		}
	}
	return flush()
}

// paragraph lays s, a piece of one source line, out at the line width and
// places its lines.
func (l *layouter) paragraph(ctx context.Context, s string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	lines, rep, err := l.fonts.Layout(ctx, []text.Run{{Text: s, Style: l.style}}, l.width, text.Options{Whitespace: text.Preserve})
	if err != nil {
		return fmt.Errorf("laying out a line of text: %w", err)
	}
	if len(lines) == 0 {
		// A piece of characters Layout drops, such as control characters,
		// draws nothing and takes a line as an empty line does.
		if err := l.place(ctx, nil); err != nil {
			return err
		}
	}
	first := 0
	for i := range lines {
		if err := l.place(ctx, lines[i].Runs); err != nil {
			return err
		}
		if i == 0 {
			first = l.pages
		}
	}
	if first == 0 {
		first = l.pages
	}
	l.reportMissing(rep.MissingGlyphs, first)
	if len(rep.MissingEmoji) > 0 && !l.emojiWarned {
		l.emojiWarned = true
		l.doc.Warnings = append(l.doc.Warnings, compose.Warning{
			Code:    CodeMissingGlyph,
			Message: "colour emoji fonts are not supported: emoji no text font covers are drawn blank",
			Page:    first,
		})
	}
	return nil
}

// place puts one line of runs, nil for a blank line, at the next baseline,
// starting a page when the line does not fit or a form feed asks for one.
func (l *layouter) place(ctx context.Context, runs []*compose.Text) error {
	if l.page == nil && l.breaks > 0 {
		// The source starts with a form feed: the first page stays blank.
		if err := l.newPage(ctx); err != nil {
			return err
		}
	}
	for ; l.breaks > 0; l.breaks-- {
		if err := l.newPage(ctx); err != nil {
			return err
		}
	}
	if l.page == nil || (l.onPage > 0 && l.next+l.descent > l.bottom) {
		if err := l.newPage(ctx); err != nil {
			return err
		}
	}
	if len(runs) > 0 && l.para == nil {
		l.para = &compose.StructElem{Role: "P"}
		l.root.Kids = append(l.root.Kids, l.para)
	}
	for _, run := range runs {
		run.Origin = compose.Point{X: l.opts.MarginLeft + run.Origin.X, Y: l.next}
		run.Parent = l.para
		l.page.Content = append(l.page.Content, run)
	}
	l.next += l.lineHeight
	l.onPage++
	return nil
}

// newPage hands the page being filled over, starts a blank page and moves
// to its first baseline.
func (l *layouter) newPage(ctx context.Context) error {
	if l.pages >= l.opts.MaxPages {
		return fmt.Errorf("%w: the text needs more than %d pages", ErrTooManyPages, l.opts.MaxPages)
	}
	if err := l.handOver(ctx); err != nil {
		return err
	}
	l.page = &compose.Page{Width: l.opts.PageWidth, Height: l.opts.PageHeight}
	l.pages++
	if l.opts.Sink == nil {
		l.doc.Pages = append(l.doc.Pages, l.page)
	}
	l.next = l.opts.MarginTop + l.ascent
	l.onPage = 0
	return nil
}

// handOver gives the page being filled to [Options.Sink], when there is
// one, and gives its glyphs back to the budget: the sink keeps none of
// them.
func (l *layouter) handOver(ctx context.Context) error {
	if l.opts.Sink == nil || l.page == nil {
		return nil
	}
	p := l.page
	l.page = nil
	if err := l.opts.Sink.Page(ctx, p); err != nil {
		return fmt.Errorf("writing page %d: %w", l.pages, err)
	}
	l.mem.Release(compose.CountGlyphs(p) * limits.GlyphCost)
	return nil
}

// reportMissing warns once for each character of missing not reported yet,
// on page, the page the paragraph that lacks it starts on.
func (l *layouter) reportMissing(missing []rune, page int) {
	for _, r := range missing {
		if l.seen[r] || l.truncated {
			continue
		}
		if len(l.seen) >= maxMissingGlyphWarnings {
			l.truncated = true
			l.doc.Warnings = append(l.doc.Warnings, compose.Warning{
				Code:    CodeMissingGlyph,
				Message: fmt.Sprintf("more than %d characters no font covers; the others are not listed", maxMissingGlyphWarnings),
				Page:    page,
			})
			return
		}
		l.seen[r] = true
		l.doc.Warnings = append(l.doc.Warnings, compose.Warning{
			Code:    CodeMissingGlyph,
			Message: fmt.Sprintf("no font covers U+%04X; asked for %s, drawn as the .notdef glyph", r, strings.Join(l.opts.FontFamilies, ", ")),
			Page:    page,
		})
	}
}
