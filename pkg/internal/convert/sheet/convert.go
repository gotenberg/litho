package sheet

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/numfmt"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// letterWidth and letterHeight are the paper of a document that prints no
// page, a blank US Letter page, LibreOffice's en-US default.
const (
	letterWidth  = 612
	letterHeight = 792
)

// builder holds what one conversion shares.
type builder struct {
	ctx      context.Context
	doc      *Document
	fonts    *text.Fonts
	opts     office.Options
	m        *measurer
	lang     string
	formats  map[string]*numfmt.Format
	warnings []compose.Warning
	warned   map[string]bool
}

// newBuilder returns the builder of one conversion.
func newBuilder(ctx context.Context, doc *Document, fonts *text.Fonts, opts office.Options) *builder {
	b := &builder{
		ctx: ctx, doc: doc, fonts: fonts, opts: opts, lang: cmp.Or(doc.Meta.Lang, opts.DefaultLang),
		formats: map[string]*numfmt.Format{}, warned: map[string]bool{},
	}
	b.m = newMeasurer(ctx, fonts, b.lang)
	return b
}

// warn records a warning.
func (b *builder) warn(code, msg string) {
	if b.warned[code+"\x00"+msg] {
		return
	}
	b.warned[code+"\x00"+msg] = true
	b.warnings = append(b.warnings, compose.Warning{Code: code, Message: msg})
}

// warnOnce records a warning of code once per conversion.
func (b *builder) warnOnce(code, msg string) {
	if b.warned[code] {
		return
	}
	b.warned[code] = true
	b.warnings = append(b.warnings, compose.Warning{Code: code, Message: msg})
}

// Convert lays doc out as LibreOffice prints a spreadsheet, with the fonts
// of the index fonts, and returns the paged document. It honours
// office.Options: PageWidth and PageHeight and Landscape override every
// sheet's page setup, SinglePageSheets lays each sheet out on one page the
// size of its content, OmitOutline leaves the
// sheet outline out, PrintDate fills the date and time fields of the
// headers and footers, MaxPages caps the pages. It returns ctx's error
// when ctx ends first, office.ErrInvalidOptions for options out of range
// or a nil fonts or doc, text.ErrNoFonts for an index without faces, and
// office.ErrTooManyPages past the page cap. The output depends on doc,
// fonts' index and opts alone.
//
// # Pages
//
// The sheets [Printed] selects print, in book order: every sheet that is
// not hidden without a selection. A sheet that does not print still feeds
// the formulas, charts and pivot tables of the sheets that do; a link to
// it is dropped, and the page counts of the headers and footers count the
// pages that print. A sheet prints each of its print areas, else its used
// range (the cells with a value, a fill or a border, and its drawings),
// cut into pages as Calc cuts it. Hidden rows and
// columns take no room. Columns fill a page's width less its margins at
// the print scale, rows its height, the repeated rows and columns taking
// their room from every page after the first; manual breaks start a page
// unless the sheet fits that direction to pages. A fit to pages finds the
// largest whole percent up to 100 at which the sheet fits, by Calc's
// bisection. A page of rows and columns that hold no value and no drawing
// is not printed. Pages number down then over, or over then down, from
// the sheet's first page number or on from the previous sheet.
//
// # Cells
//
// A cell shows its value formatted by its number format, in its font,
// over its fill, inside its borders, a border shared with a neighbour
// drawn once with the heavier line. Text aligns left and values right by
// default; text that does not wrap overflows into the empty cells on the
// side its alignment leaves free and is clipped at the first cell with
// content; a number that does not fit its column shows "###", a General
// one first losing decimals. Rows without a custom height grow to their
// wrapped text. The sheet's conditional formats restyle the cells their
// rules hold for, paint colour scales, draw data bars and the icons of
// icon sets, as Calc does. The grid and the row and column headings print when the
// sheet asks for them, and pictures over the cells their anchors name.
//
// # Pivot tables
//
// A sheet's pivot tables are rebuilt from their source cells before the
// sheet is cut into pages, as Calc rebuilds them when it loads a
// workbook: the area the file saved the output in is emptied, and the
// output written in its place with Calc's captions ("Total Result",
// "(empty)", the field names), its frames and its DataPilot cell styles.
//
// # Not supported
//
// The conditional formatting rules whose formula is not computed are
// left out with a warning; right-to-left sheets are laid out left to
// right with a warning; text rotated by other than a quarter turn starts
// in the cell's corner, an approximation, with a warning; the accounting
// underlines draw as plain ones.
func Convert(ctx context.Context, doc *Document, fonts *text.Fonts, opts office.Options) (*compose.Document, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	if doc == nil || fonts == nil {
		return nil, fmt.Errorf("%w: no document or no font index", office.ErrInvalidOptions)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if fonts.Len() == 0 {
		return nil, fmt.Errorf("laying the spreadsheet out: %w", text.ErrNoFonts)
	}
	b := newBuilder(ctx, doc, fonts, opts)
	doc = b.applyFormulas(doc)
	b.doc = doc
	doc = b.applyPivots(doc)
	b.doc = doc
	limit := opts.MaxPages
	if limit <= 0 {
		limit = layout.DefaultMaxPages
	}
	var plans []*sheetPlan
	var pages []*pagePlan
	// starts holds the index in pages of each plan's first page.
	var starts []int
	printed, err := printedSheets(doc, opts)
	if err != nil {
		return nil, err
	}
	for i, sh := range doc.Sheets {
		if !printed[i] {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		sp := b.planSheet(i, sh)
		plans = append(plans, sp)
		starts = append(starts, len(pages))
		pages = append(pages, sp.pages...)
		if len(pages) > limit {
			return nil, fmt.Errorf("%w: more than %d pages", office.ErrTooManyPages, limit)
		}
	}
	// Page numbers: each sheet from its first page number, else on from
	// the previous sheet's last.
	n := 0
	for _, sp := range plans {
		if fp := sp.g.sh.Page.FirstPageNumber; fp > 0 {
			n = fp - 1
		}
		for _, p := range sp.pages {
			n++
			p.number = n
		}
	}
	out := &compose.Document{
		Metadata:  doc.Meta,
		Structure: &compose.StructElem{Role: "Document", Lang: b.lang},
	}
	out.Metadata.Lang = b.lang
	if opts.Sink != nil {
		opts.Sink.Expect(max(len(pages), 1))
	}
	links := &sheetLinker{doc: doc, pages: pages, waiting: map[int][]*compose.Dest{}}
	for k, p := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pg, st, err := b.renderPage(p, k == 0 || pages[k-1].sp != p.sp, len(pages))
		if err != nil {
			return nil, err
		}
		links.table(tableOf(st))
		if st != nil {
			out.Structure.Kids = append(out.Structure.Kids, st.Kids...)
		}
		links.resolve(pg)
		if err := b.handOver(out, pg); err != nil {
			return nil, err
		}
	}
	tables := links.tables
	if len(pages) == 0 {
		// A book that prints no cell prints the first page of its first
		// sheet, the cells its styles paint, with the sheet's header and
		// footer and its outline entry, as LibreOffice does.
		w, h := b.opts.Paper(b.opts.DefaultPage(letterWidth, letterHeight))
		pg := &compose.Page{Width: w, Height: h}
		if len(plans) > 0 {
			sp := plans[0]
			pg.Width, pg.Height = sp.width, sp.height
			// The first page of the first sheet prints, with what its
			// styles paint, else its first cell.
			p := sp.first
			if p == nil {
				p = &pagePlan{sp: sp}
			}
			p.number = max(sp.g.sh.Page.FirstPageNumber, 1)
			r, st, err := b.renderPage(p, true, 1)
			if err != nil {
				return nil, err
			}
			pg = r
			tbl := tableOf(st)
			if tbl != nil {
				out.Structure.Kids = append(out.Structure.Kids, st.Kids...)
			}
			if !opts.OmitOutline {
				out.Outline = []compose.Outline{{Title: sp.g.sh.Name, Dest: compose.Dest{Page: 1, Elem: tbl}}}
			}
		}
		if err := b.handOver(out, pg); err != nil {
			return nil, err
		}
	}
	if !opts.OmitOutline {
		// Every planned page is laid out and kept, in order: pages[k] is
		// page k+1.
		for i, sp := range plans {
			if len(sp.pages) == 0 {
				continue
			}
			k := starts[i]
			out.Outline = append(out.Outline, compose.Outline{Title: sp.g.sh.Name, Dest: compose.Dest{Page: k + 1, Elem: tables[k]}})
		}
	}
	out.Warnings = b.warnings
	return out, nil
}

// handOver gives the page pg, laid out, to the sink of the options, and
// its glyphs back to the budget, or appends it to out without a sink.
func (b *builder) handOver(out *compose.Document, pg *compose.Page) error {
	sink := b.opts.Sink
	if sink == nil {
		out.Pages = append(out.Pages, pg)
		return nil
	}
	if err := sink.Page(b.ctx, pg); err != nil {
		return fmt.Errorf("writing a page: %w", err)
	}
	limits.BudgetFrom(b.ctx).Release(compose.CountGlyphs(pg) * limits.GlyphCost)
	return nil
}

// tableOf returns the element an outline entry or a link into a page
// names as its structure destination (ISO 32000-2, 12.3.2.3; ISO 14289-2,
// 8.8): the Table element among the kids of the page's structure root st,
// else, on a page that prints only drawings, its first Figure; nil for
// none.
func tableOf(st *compose.StructElem) *compose.StructElem {
	if st == nil {
		return nil
	}
	for _, k := range st.Kids {
		if k.Role == "Table" {
			return k
		}
	}
	for _, k := range st.Kids {
		if k.Role == "Figure" {
			return k
		}
	}
	return nil
}

// paper returns the page size of a sheet: its own, or the options'
// paper in its own orientation, turned to landscape when the options ask
// (office.Options.Paper).
func (b *builder) paper(sh *Sheet) (float64, float64) {
	w, h := sh.Page.Width, sh.Page.Height
	if !positive(w) || !positive(h) {
		w, h = b.opts.DefaultPage(letterWidth, letterHeight)
	}
	w, h = b.opts.Paper(w, h)
	return min(w, office.MaxPageSide), min(h, office.MaxPageSide)
}

// planSheet returns how the sheet sh prints.
func (b *builder) planSheet(i int, sh *Sheet) *sheetPlan {
	g := newGrid(b.doc, sh)
	g.need, g.overflowBudget = b.textNeed(g), maxOverflowCells
	b.optimalHeights(g)
	sp := &sheetPlan{g: g, idx: i, zoom: 1, single: b.opts.SinglePageSheets}
	sp.width, sp.height = b.paper(sh)
	for k, m := range sh.Page.Margin {
		sp.margin[k] = min(max(m, 0), maxSide)
	}
	// Margins that leave no room shrink to leave one point.
	if sp.margin[css.Left]+sp.margin[css.Right] >= sp.width-1 {
		sp.margin[css.Left], sp.margin[css.Right] = 0, 0
	}
	if sp.margin[css.Top]+sp.margin[css.Bottom] >= sp.height-1 {
		sp.margin[css.Top], sp.margin[css.Bottom] = 0, 0
	}
	if !sp.single {
		b.growMargins(sp)
	}
	if n := unevaluated(g); n > 0 {
		b.warn(CodeConditional, fmt.Sprintf("sheet %s: %d conditional formatting rules (formulas, unknown icon sets) are not applied", quote(sh.Name), n))
	}
	if sh.RightToLeft {
		b.warnOnce(CodeRightToLeft, "a right-to-left sheet is laid out left to right")
	}
	sp.plan()
	if len(sp.pages) >= maxSheetPages {
		b.warn(CodeTooManyPages, fmt.Sprintf("sheet %s has more than %d pages; the rest are left out", quote(sh.Name), maxSheetPages))
	}
	if sp.single && len(sp.pages) > 0 {
		// LibreOffice's SinglePageSheets export sizes the page to the
		// printed cells alone: no margins, no header or footer.
		w, h := sp.singleSize()
		sp.margin = [4]float64{}
		sp.width = math.Min(w, office.MaxPageSide)
		sp.height = math.Min(h, office.MaxPageSide)
		sp.pages = sp.pages[:1]
	}
	return sp
}

// renderPage draws the page p, first reporting the first page of its
// sheet, total the document's page count.
func (b *builder) renderPage(p *pagePlan, first bool, total int) (*compose.Page, *compose.StructElem, error) {
	sp := p.sp
	pb := &pageBuilder{builder: b, g: sp.g, p: p}
	frags, links, st, err := pb.build()
	if err != nil {
		if errors.Is(err, layout.ErrTooManyPages) {
			return nil, nil, fmt.Errorf("%w: %w", office.ErrTooManyPages, err)
		}
		return nil, nil, err
	}
	ps := &sp.g.sh.Page
	z := sp.zoom
	areaW := sp.width - sp.margin[css.Left] - sp.margin[css.Right]
	areaH := sp.height - sp.margin[css.Top] - sp.margin[css.Bottom]
	ox, oy := sp.margin[css.Left], sp.margin[css.Top]
	// The header's and the footer's room in the margins shrinks with the
	// print scale, as Calc adds their height in zoomed units to the page
	// margins (ScPrintFunc::GetDocPageSize): the cells start that much
	// higher, and the room below them grows as much.
	hf := &sp.g.sh.HeaderFooter
	if z != 1 && !sp.single {
		if headerShown(hf, false) && ps.HeaderTop >= 0 && ps.HeaderTop < sp.margin[css.Top] {
			shrink := (sp.margin[css.Top] - ps.HeaderTop) * (1 - z)
			oy -= shrink
			areaH += shrink
		}
		if headerShown(hf, true) && ps.FooterBottom >= 0 && ps.FooterBottom < sp.margin[css.Bottom] {
			areaH += (sp.margin[css.Bottom] - ps.FooterBottom) * (1 - z)
		}
	}
	if ps.CenterHorizontally {
		ox += (areaW - pb.dataW*z) / 2
	}
	if ps.CenterVertically {
		oy += (areaH - pb.dataH*z) / 2
	}
	pg := &compose.Page{Width: sp.width, Height: sp.height}
	if !sp.single {
		pg.Content = append(pg.Content, b.margins(p, first, p.number, total, sp.width, sp.height)...)
	}
	pg.Content = append(pg.Content, &compose.Group{
		Transform: compose.Matrix{A: z, D: z, E: ox, F: oy},
		Clip:      &compose.Path{Ops: rectOps(0, 0, pb.dataW, pb.dataH), Fill: &compose.Fill{}},
		Opacity:   1,
		Content:   frags,
	})
	for _, l := range links {
		l.Rect = compose.Rect{X: l.Rect.X*z + ox, Y: l.Rect.Y*z + oy, Width: l.Rect.Width * z, Height: l.Rect.Height * z}
		pg.Links = append(pg.Links, l)
	}
	return pg, st, nil
}
