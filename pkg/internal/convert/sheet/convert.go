package sheet

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
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
		ctx: ctx, doc: doc, fonts: fonts, opts: opts, lang: doc.Meta.Lang,
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
// Every sheet that is not hidden prints, in book order: each of its print
// areas, else its used range (the cells with a value, a fill or a border,
// and its drawings), cut into pages as Calc cuts it. Hidden rows and
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
// rules hold for, paint colour scales and draw data bars, as Calc does. The grid and the row and column headings print when the
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
// Charts are reserved as empty boxes with a warning; the conditional
// formatting rules that hold a formula and the icon sets are left out
// with a warning; right-to-left sheets are laid out left to
// right with a warning; rotated text turns about the cell's centre; the
// accounting underlines draw as plain ones.
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
	anyVisible := false
	for _, sh := range doc.Sheets {
		anyVisible = anyVisible || (sh != nil && !sh.Hidden)
	}
	for i, sh := range doc.Sheets {
		// A book whose sheets are all hidden shows its first one, as
		// Calc does.
		if sh == nil || (sh.Hidden && (anyVisible || len(plans) > 0)) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		sp := b.planSheet(i, sh)
		plans = append(plans, sp)
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
	at := make([]int, len(pages))
	for k, p := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pg, st, err := b.renderPage(p, k == 0 || pages[k-1].sp != p.sp, len(pages))
		if err != nil {
			return nil, err
		}
		out.Pages = append(out.Pages, pg)
		at[k] = len(out.Pages)
		if st != nil {
			out.Structure.Kids = append(out.Structure.Kids, st.Kids...)
		}
	}
	if len(pages) == 0 {
		// A book that prints no cell prints the first cell of its first
		// sheet on one page, with the sheet's header and footer and its
		// outline entry, as LibreOffice does.
		pg := &compose.Page{Width: letterWidth, Height: letterHeight}
		if len(plans) > 0 {
			sp := plans[0]
			pg.Width, pg.Height = sp.width, sp.height
			// The first cell prints, with what its styles paint.
			p := &pagePlan{sp: sp, number: max(sp.g.sh.Page.FirstPageNumber, 1)}
			if r, _, err := b.renderPage(p, true, 1); err == nil {
				pg = r
			}
			if !opts.OmitOutline {
				out.Outline = []compose.Outline{{Title: sp.g.sh.Name, Dest: compose.Dest{Page: 1}}}
			}
		}
		out.Pages = []*compose.Page{pg}
	}
	if !opts.OmitOutline {
		for _, sp := range plans {
			for _, p := range sp.pages {
				idx := indexOf(pages, p)
				if idx >= 0 && at[idx] > 0 {
					out.Outline = append(out.Outline, compose.Outline{Title: sp.g.sh.Name, Dest: compose.Dest{Page: at[idx]}})
					break
				}
			}
		}
	}
	out.Warnings = b.warnings
	return out, nil
}

// indexOf returns the index of p in pages, -1 when absent.
func indexOf(pages []*pagePlan, p *pagePlan) int {
	for i, q := range pages {
		if q == p {
			return i
		}
	}
	return -1
}

// paper returns the page size of a sheet: its own, or the options'
// paper, turned to landscape when the options ask.
func (b *builder) paper(sh *Sheet) (float64, float64) {
	w, h := sh.Page.Width, sh.Page.Height
	if !(w > 0) || !(h > 0) {
		w, h = letterWidth, letterHeight
	}
	if b.opts.PageWidth > 0 && b.opts.PageHeight > 0 {
		w, h = b.opts.PageWidth, b.opts.PageHeight
	}
	if b.opts.Landscape && w < h {
		w, h = h, w
	}
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
		b.warn(CodeConditional, fmt.Sprintf("sheet %s: %d conditional formatting rules (formulas, icon sets) are not applied", quote(sh.Name), n))
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
