package sheet

import (
	"cmp"
	"context"
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/numfmt"
	"github.com/gotenberg/litho/pkg/internal/symbolfont"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// FallbackWidth and FallbackHeight are the paper, in points, of a sheet
// whose file names none when the conversion names no default paper
// (office.Options.DefaultPage), and of the blank page a book that prints
// no cell prints: ISO 216 A4, the library's default paper and the paper
// LibreOffice falls back on in a locale that names none.
//
// # Corpus evidence
//
//   - litho/ods paper-grid-default-portrait and -landscape: a page layout
//     that names no size prints on 595.3 by 841.89 point pages, landscape
//     or not.
//   - litho/csv paper-table and libreoffice/csv: every CSV file prints on
//     595.3 by 841.89 point pages.
const (
	FallbackWidth  = 210 * 72 / 25.4
	FallbackHeight = 297 * 72 / 25.4
)

// builder holds what one conversion shares.
type builder struct {
	ctx   context.Context
	doc   *Document
	fonts *text.Fonts
	opts  office.Options
	// symbols maps what the symbol cells draw back to their codes.
	symbols  *office.SymbolCodes
	m        *measurer
	lang     string
	formats  map[string]*numfmt.Format
	warnings []typeset.Warning
	warned   map[string]bool
}

// newBuilder returns the builder of one conversion.
func newBuilder(ctx context.Context, doc *Document, fonts *text.Fonts, opts office.Options) *builder {
	b := &builder{
		ctx: ctx, doc: doc, fonts: fonts, opts: opts, symbols: office.NewSymbolCodes(), lang: cmp.Or(doc.Meta.Lang, opts.DefaultLang),
		formats: map[string]*numfmt.Format{}, warned: map[string]bool{},
	}
	b.m = newMeasurer(ctx, fonts, b.lang)
	b.m.fallback = b.fallback()
	return b
}

// fallback returns the font fallback rule of the program the book is
// printed as: Excel keeps fontconfig's, Calc follows LibreOffice's.
func (b *builder) fallback() text.Fallback {
	if b.doc.ExcelRules {
		return text.FallbackFontconfig
	}
	return text.FallbackLibreOffice
}

// symbolRule returns the rule a cell of a symbol font draws by: Excel's,
// which Word's stands for, or Calc's.
func (b *builder) symbolRule() symbolfont.Rule {
	if b.doc.ExcelRules {
		return symbolfont.RuleWord
	}
	return symbolfont.RuleLibreOffice
}

// warn records a warning once per code, effect and message.
func (b *builder) warn(code warn.Code, effect warn.Effect, msg string) {
	k := code.String() + "\x00" + effect.String() + "\x00" + msg
	if b.warned[k] {
		return
	}
	b.warned[k] = true
	b.warnings = append(b.warnings, typeset.Warning{Code: code, Effect: effect, Message: msg})
}

// Convert lays doc out as LibreOffice prints a spreadsheet, with the fonts
// of the index fonts, and returns the paged document. It honours
// office.Options: PageWidth and PageHeight and Landscape override every
// sheet's page setup, SinglePageSheets lays each sheet out on one page the
// size of its content, OmitOutline leaves the
// sheet outline out, PrintDate fills the date and time fields of the
// headers and footers, MaxPages caps the pages. It returns ctx's error
// when ctx ends first, typeset.ErrInvalidOptions for options out of range
// or a nil fonts or doc, text.ErrNoFonts for an index without faces, and
// limits.ErrTooManyPages past the page cap. The output depends on doc,
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
func Convert(ctx context.Context, doc *Document, fonts *text.Fonts, opts office.Options) (*typeset.Document, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	if doc == nil || fonts == nil {
		return nil, fmt.Errorf("%w: no document or no font index", typeset.ErrInvalidOptions)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if fonts.Len() == 0 {
		return nil, fmt.Errorf("laying the spreadsheet out: %w", text.ErrNoFonts)
	}
	b := newBuilder(ctx, doc, fonts, opts)
	doc, err := b.applyFormulas(doc)
	if err != nil {
		return nil, err
	}
	b.doc = doc
	if doc, err = b.applyPivots(doc); err != nil {
		return nil, err
	}
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
		sp, err := b.planSheet(i, sh)
		if err != nil {
			return nil, err
		}
		plans = append(plans, sp)
		starts = append(starts, len(pages))
		pages = append(pages, sp.pages...)
		if len(pages) > limit {
			return nil, fmt.Errorf("%w: more than %d", limits.ErrTooManyPages, limit)
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
	out := &typeset.Document{
		Metadata:  doc.Meta,
		Structure: &typeset.StructElem{Role: "Document", Lang: b.lang},
	}
	out.Metadata.Lang = b.lang
	if opts.Sink != nil {
		opts.Sink.Expect(max(len(pages), 1))
	}
	links := &sheetLinker{doc: doc, pages: pages, waiting: map[int][]*typeset.Dest{}}
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
		// footer and its outline entry, as LibreOffice does (see
		// blankBookPaper).
		w, h := b.opts.Paper(b.opts.DefaultPage(FallbackWidth, FallbackHeight))
		pg := &typeset.Page{Width: w, Height: h}
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
			if !doc.ExcelRules {
				blankBookPaper(pg, w, h)
			}
			tbl := tableOf(st)
			if tbl != nil {
				out.Structure.Kids = append(out.Structure.Kids, st.Kids...)
			}
			if !opts.OmitOutline {
				out.Outline = []typeset.Outline{{Title: sp.g.sh.Name, Dest: typeset.Dest{Page: 1, Elem: tbl}}}
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
			// The entry names the sheet's first page that holds an
			// element: a blank page printed before it has none to be the
			// structure destination ISO 14289-2, 8.8 asks of an outline
			// entry. A sheet none of whose pages holds one keeps its
			// first page.
			k := starts[i]
			for j := k; j < k+len(sp.pages) && j < len(tables); j++ {
				if tables[j] != nil {
					k = j
					break
				}
			}
			out.Outline = append(out.Outline, typeset.Outline{Title: sp.g.sh.Name, Dest: typeset.Dest{Page: k + 1, Elem: tables[k]}})
		}
	}
	out.Warnings = b.warnings
	return out, nil
}

// blankBookPaper sets the size of the page pg a book that prints no cell
// prints to w by h, the conversion's default paper, its content left where
// the sheet's own paper put it from the top left corner: Calc lays that
// page out on the sheet's page style and prints it on the default paper.
// Excel prints no page for such a book, so a workbook read from XLSX
// keeps its sheet's paper.
//
// # Corpus evidence
//
//   - libreoffice/ods: sc/tdf121716_EvenHeaderFooter.ods, two sheets
//     without a cell, the first on a Letter page style, prints one A4
//     page with the Letter page's header and footer.
//   - probe, ods: the same book with a 15 by 20 cm page style prints one
//     A4 page, its footer 2 cm above the bottom of the 20 cm page.
func blankBookPaper(pg *typeset.Page, w, h float64) {
	pg.Width, pg.Height = w, h
}

// handOver gives the page pg, laid out, to the sink of the options, and
// its glyphs back to the budget, or appends it to out without a sink.
func (b *builder) handOver(out *typeset.Document, pg *typeset.Page) error {
	b.symbols.Restore(pg)
	sink := b.opts.Sink
	if sink == nil {
		out.Pages = append(out.Pages, pg)
		return nil
	}
	if err := sink.Page(b.ctx, pg); err != nil {
		// The stream names the page it refuses.
		return err
	}
	limits.BudgetFrom(b.ctx).Release(typeset.CountGlyphs(pg) * limits.GlyphCost)
	return nil
}

// tableOf returns the element an outline entry or a link into a page
// names as its structure destination (ISO 32000-2, 12.3.2.3; ISO 14289-2,
// 8.8): the Table element among the kids of the page's structure root st,
// else, on a page that prints only drawings, its first Figure; nil for
// none.
func tableOf(st *typeset.StructElem) *typeset.StructElem {
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
	if !numeric.Positive(w) || !numeric.Positive(h) {
		w, h = b.opts.DefaultPage(FallbackWidth, FallbackHeight)
	}
	w, h = b.opts.Paper(w, h)
	return min(w, office.MaxPageSide), min(h, office.MaxPageSide)
}

// planSheet returns how the sheet sh prints. The builder's context stops
// it.
func (b *builder) planSheet(i int, sh *Sheet) (*sheetPlan, error) {
	g := newGrid(b.ctx, b.doc, sh)
	if g.largeMerges > 0 {
		b.warn(warn.LimitReached, warn.Approximated, fmt.Sprintf("sheet %s merges more than %d cells into one; such a merge is drawn as its first cell", quote(sh.Name), maxMergeCells))
	}
	g.need, g.overflowBudget = b.textNeed(g), maxOverflowCells
	if err := b.optimalHeights(g); err != nil {
		return nil, err
	}
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
		b.warn(warn.ContentUnsupported, warn.Approximated, fmt.Sprintf("sheet %s: %d conditional formatting rules (formulas, unknown icon sets) are not applied", quote(sh.Name), n))
	}
	if sh.RightToLeft {
		b.warn(warn.ContentUnsupported, warn.Approximated, "a right-to-left sheet is laid out left to right")
	}
	if err := sp.plan(b.ctx); err != nil {
		return nil, err
	}
	if len(sp.pages) >= maxSheetPages {
		b.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("sheet %s has more than %d pages; the rest are left out", quote(sh.Name), maxSheetPages))
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
	return sp, nil
}

// renderPage draws the page p, first reporting the first page of its
// sheet, total the document's page count.
func (b *builder) renderPage(p *pagePlan, first bool, total int) (*typeset.Page, *typeset.StructElem, error) {
	sp := p.sp
	pb := &pageBuilder{builder: b, g: sp.g, p: p}
	frags, links, st, err := pb.build()
	if err != nil {
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
	// higher, and the room below them grows as much. Excel keeps the
	// margins whole (sheetPlan.docArea).
	hf := &sp.g.sh.HeaderFooter
	if z != 1 && !sp.single && !sp.g.doc.ExcelRules {
		if headerShown(hf, false) && ps.HeaderTop >= 0 && ps.HeaderTop < sp.margin[css.Top] {
			shrink := float64((sp.margin[css.Top] - ps.HeaderTop) * (1 - z))
			oy -= shrink
			areaH += shrink
		}
		if headerShown(hf, true) && ps.FooterBottom >= 0 && ps.FooterBottom < sp.margin[css.Bottom] {
			areaH += float64((sp.margin[css.Bottom] - ps.FooterBottom) * (1 - z))
		}
	}
	if ps.CenterHorizontally {
		ox += float64((areaW - float64(pb.dataW*z)) / 2)
	}
	if ps.CenterVertically {
		oy += float64((areaH - float64(pb.dataH*z)) / 2)
	}
	if !sp.single && !sp.g.doc.ExcelRules {
		ox, oy = calcOrigin(ox, z), calcOrigin(oy, z)
	}
	pg := &typeset.Page{Width: sp.width, Height: sp.height}
	if !sp.single {
		pg.Content = append(pg.Content, b.margins(p, first, p.number, total, sp.width, sp.height)...)
	}
	grow := 0.0
	if !sp.g.doc.ExcelRules {
		grow = pb.borderOverhang()
	}
	pg.Content = append(pg.Content, &typeset.Group{
		Transform: typeset.Matrix{A: z, D: z, E: ox, F: oy},
		Clip:      &typeset.Path{Ops: rectOps(-grow, -grow, pb.dataW+2*grow, pb.dataH+2*grow), Fill: &typeset.Fill{}},
		Opacity:   1,
		Content:   frags,
	})
	for _, l := range links {
		l.Rect = typeset.Rect{X: float64(l.Rect.X*z) + ox, Y: float64(l.Rect.Y*z) + oy, Width: l.Rect.Width * z, Height: l.Rect.Height * z}
		pg.Links = append(pg.Links, l)
	}
	return pg, st, nil
}

// calcOrigin returns where Calc starts a page's cells, v page points from
// the paper's edge at zoom z: Calc adds the margin and the header's room in
// twips, unzoomed, and truncates the sum to whole 1/100 mm
// (ScPrintFunc::PrintPage, nStartX and nStartY, sc/source/ui/view
// printfun.cxx), as it truncates the rows and columns ([calcPrinted]).
//
// # Corpus evidence
//
//   - probe, ods: margins of 17.77 points (355 twips) and of 1.2345 cm
//     (700 twips) under a header of 0.777 cm (441 twips), converted by
//     Gotenberg 9.4.1, start the cells at 626, 1,234 and 2,012 hundredths
//     of a millimetre from the paper's edge (their border line one
//     hundredth further out): the twips' sums truncated, where the
//     lengths read straight in hundredths would start them at 627 and
//     1,235.
func calcOrigin(v, z float64) float64 {
	if !numeric.Positive(v) || !numeric.Positive(z) {
		return v
	}
	return float64(z * math.Floor(v/z*2540/72+1e-6) * 72 / 2540)
}

// borderOverhang returns how far the page's cell borders reach past the
// edges of its cells: each line's ends and half its width. Calc draws a
// border on the edge of the printed cells whole, its outer half too, so
// the page's clip grows that far.
//
// # Corpus evidence
//
//   - libreoffice/ods: sc/pivot-table-median.ods and
//     sc/tdf124651_simplePivotTable.ods print the 2-point frame of their
//     pivot tables on the first and last row's edge, both halves of the
//     line.
func (b *pageBuilder) borderOverhang() float64 {
	reach := 0.0
	for _, f := range b.borderPaths {
		p, ok := f.(*typeset.Path)
		if !ok || p.Stroke == nil || !numeric.Finite(p.Stroke.Width) || len(p.Ops) == 0 {
			continue
		}
		half := min(max(p.Stroke.Width, 0), maxBorderWidth) / 2
		x0, y0 := p.Ops[0].P[0].X, p.Ops[0].P[0].Y
		x1, y1 := x0, y0
		for _, op := range p.Ops {
			x0, x1 = min(x0, op.P[0].X), max(x1, op.P[0].X)
			y0, y1 = min(y0, op.P[0].Y), max(y1, op.P[0].Y)
		}
		// A straight line's width spreads across it alone: its ends
		// already reach past the corners as far as it is drawn.
		hx, hy := half, half
		if y0 == y1 {
			hx = 0
		}
		if x0 == x1 {
			hy = 0
		}
		reach = max(reach, hx-x0, hy-y0, x1+hx-b.dataW, y1+hy-b.dataH)
	}
	if !numeric.Finite(reach) {
		return 0
	}
	return min(reach, maxBorderWidth)
}
