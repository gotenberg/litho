package html

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	dom "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// textDefaultsCSS is chrome://resources/css/text_defaults.css, the first
// stylesheet of Chromium's header and footer template page
// (ui/webui/resources/css/text_defaults.css), resolved for Linux and the
// en-US locale by webui::GetFontFamily and GetFontSize
// (ui/base/webui/web_ui_util.cc): the user interface font, Noto Sans in
// the Gotenberg image, then IDS_WEB_FONT_FAMILY, at IDS_WEB_FONT_SIZE,
// 75%.
const textDefaultsCSS = `body {
  font-family: "Noto Sans", system-ui, sans-serif;
  font-size: 75%;
}

button {
  font-family: "Noto Sans", system-ui, sans-serif;
}
`

// templateCSS is the stylesheet of Chromium's header and footer template
// page (components/printing/resources/print_header_footer_template_page.html),
// verbatim, then the block layout fallback.
const templateCSS = `
  body {
    display: flex;
    flex-direction: column;
    margin: 0;
  }

  #header,
  #footer {
    display: flex;
    flex: none;
  }

  #header {
    align-items: flex-start;
    padding-top: 15pt;
  }

  #footer {
    align-items: flex-end;
    padding-bottom: 15pt;
  }

  #content {
    flex: auto;
  }

  .left {
    flex: none;
    padding-left: 24pt;
    padding-right: 6pt;
  }

  .center {
    flex: auto;
    padding-left: 24pt;
    padding-right: 24pt;
    text-align: center;
  }

  .right {
    flex: none;
    /* historically does not account for RTL */
    padding-left: 6pt;
    padding-right: 24pt;
  }

  .grow {
    flex: auto;
  }

  .text {
    font-size: 8pt;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
` + blockFallbackCSS

// blockFallbackCSS lays the template out with block and inline layout where
// flexbox is not supported, which it detects as align-items unknown: the
// header's items side by side at its top, the footer at the bottom of the
// page with its items side by side at its bottom, as the flex layout of
// [templateCSS] places a row of items that keep their content size, and a
// lone growing item as wide as the page. Items that share the width
// between them, a center between a left and a right, need flexbox. Its
// :where() selectors weigh nothing, so the template's own rules win. Once
// the engine lays flex containers out the condition fails and Chromium's
// stylesheet alone applies.
const blockFallbackCSS = `
  @supports not (align-items: flex-end) {
    body {
      position: relative;
    }

    :where(#header, #footer) > * {
      display: inline-block;
      vertical-align: top;
    }

    :where(#footer) > * {
      vertical-align: bottom;
    }

    :where(#header, #footer) > :where(.center, .grow):only-child {
      display: block;
    }

    #footer {
      position: absolute;
      left: 0;
      right: 0;
      bottom: 0;
      height: auto !important;
    }
  }
`

// templateSkeleton is the body of Chromium's template page.
const templateSkeleton = `<!doctype html><html><head><style>` + textDefaultsCSS + `</style><style>` + templateCSS + `</style></head>` +
	`<body><div id="header"></div><div id="content"></div><div id="footer"></div></body></html>`

// templateClasses are the classes whose elements' text the template fills,
// in the order Chromium fills them: an element of two takes the last.
var templateClasses = []string{"date", "title", "url", "pageNumber", "totalPages"}

// chromiumDate formats t as Chromium's template does for the en-US locale,
// Intl.DateTimeFormat with the short date and time styles: "11/6/26, 10:30
// AM". The zero time is "".
func chromiumDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	h, ampm := t.Hour()%12, "AM"
	if t.Hour() >= 12 {
		ampm = "PM"
	}
	if h == 0 {
		h = 12
	}
	return fmt.Sprintf("%d/%d/%02d, %d:%02d %s", int(t.Month()), t.Day(), t.Year()%100, h, t.Minute(), ampm)
}

// headerFooter is a page's header and footer template being laid out.
type headerFooter struct {
	doc            *dom.Node
	body           *dom.Node
	header, footer *dom.Node
	// texts are the elements of class text, ids their anchors.
	texts []*dom.Node
	ids   map[*dom.Node]string
}

// decorate draws the header and footer templates on the sheet pg, the
// page of index k of a layout of total pages placed on it (CSS Page 3 has
// no such thing: Chromium's printToPDF headerTemplate and footerTemplate).
// The templates are laid out on the sheet, as large as it is and with the
// margins of the page k's @page rules give it, whatever the print scale:
// Chromium draws them on the printed page, unscaled
// (PrintRenderFrameHelper::PrintHeaderAndFooter). They paint over the
// page's content, as artifacts.
func (p *prepared) decorate(ctx context.Context, pg *compose.Page, k, total int) error {
	if p.o.HeaderTemplate == "" && p.o.FooterTemplate == "" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	info := layout.PageInfo{Index: k}
	if k < len(p.infos) {
		info = p.infos[k]
	}
	pb := layout.PageBox{Width: pg.Width, Height: pg.Height, Margin: p.pages.entry(info).box.Margin}
	frags, err := p.templatePage(ctx, pb, k+1, total)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		p.warnOnce(fmt.Sprintf("the header and footer of page %d were left out: %v", k+1, err))
		return nil
	}
	pg.Content = append(pg.Content, frags...)
	return nil
}

// templatePage lays the templates out for page number page of pages,
// whose page box is pb, and returns what they paint.
func (p *prepared) templatePage(ctx context.Context, pb layout.PageBox, page, pages int) ([]compose.Fragment, error) {
	hf, err := p.buildTemplate(pb, page, pages)
	if err != nil {
		return nil, err
	}
	res, err := p.layTemplate(ctx, hf, pb)
	if err != nil {
		return nil, err
	}
	// Chromium hides an element of class text that reaches into the page
	// area, its padding left out (elementIntersects in the template's
	// script).
	top, bottom := pb.Margin[css.Top], pb.Height-pb.Margin[css.Bottom]
	left, right := pb.Margin[css.Left], pb.Width-pb.Margin[css.Right]
	hidden := false
	for _, n := range hf.texts {
		r, ok := res.Extents[hf.ids[n]]
		if !ok {
			continue
		}
		if left < r.X+r.Width && right > r.X && top < r.Y+r.Height && bottom > r.Y {
			setStyle(n, attrOr(n, "style")+"; visibility: hidden")
			hidden = true
		}
	}
	if hidden {
		if res, err = p.layTemplate(ctx, hf, pb); err != nil {
			return nil, err
		}
	}
	if len(res.Pages) == 0 {
		return nil, nil
	}
	return artifacts(res.Pages[0].Content), nil
}

// layTemplate lays the template document of hf out on a page of the size
// of pb without margins.
func (p *prepared) layTemplate(ctx context.Context, hf *headerFooter, pb layout.PageBox) (*layout.Result, error) {
	o := Options{PageWidth: pb.Width, PageHeight: pb.Height, Media: MediaPrint, MediaFeatures: p.o.MediaFeatures, Logger: p.o.Logger}
	o, err := o.withDefaults()
	if err != nil {
		return nil, err
	}
	tp, err := prepareTagged(ctx, hf.doc, o, p.fonts, newPrintSetup(o), hf.ids)
	if err != nil {
		return nil, err
	}
	tp.layout.PageFor, tp.layout.PageAt, tp.layout.MarginContent = nil, nil, nil
	tp.layout.Page = layout.PageBox{Width: pb.Width, Height: pb.Height}
	tp.layout.MaxPages = maxSubLayoutPages
	return layout.Layout(ctx, tp.root, p.fonts, tp.layout)
}

// buildTemplate returns Chromium's template page for page number page of
// pages on the page box pb: the body as large as the page, the header as
// tall as the top margin and the footer as the bottom one, each holding
// its template parsed as an element's inner HTML, and the classes filled
// (setupHeaderFooterTemplate in the template's script).
func (p *prepared) buildTemplate(pb layout.PageBox, page, pages int) (*headerFooter, error) {
	doc, err := dom.Parse(strings.NewReader(templateSkeleton))
	if err != nil {
		return nil, fmt.Errorf("parsing the template page: %w", err)
	}
	hf := &headerFooter{doc: doc, ids: map[*dom.Node]string{}}
	var head *dom.Node
	walk(doc, func(n *dom.Node) bool {
		if n.Type == dom.ElementNode {
			switch {
			case n.DataAtom == atom.Head:
				head = n
			case n.DataAtom == atom.Body:
				hf.body = n
			case attrOr(n, "id") == "header":
				hf.header = n
			case attrOr(n, "id") == "footer":
				hf.footer = n
			}
		}
		return true
	})
	if head == nil || hf.body == nil || hf.header == nil || hf.footer == nil {
		return nil, fmt.Errorf("%w: the template page lost its structure", ErrInvalidOptions)
	}
	setStyle(hf.body, fmt.Sprintf("width: %spt; height: %spt", ptString(pb.Width), ptString(pb.Height)))
	setStyle(hf.header, fmt.Sprintf("height: %spt", ptString(pb.Margin[css.Top])))
	setStyle(hf.footer, fmt.Sprintf("height: %spt", ptString(pb.Margin[css.Bottom])))
	for _, f := range []struct {
		box *dom.Node
		src string
	}{{hf.header, p.o.HeaderTemplate}, {hf.footer, p.o.FooterTemplate}} {
		kids, err := dom.ParseFragment(strings.NewReader(f.src), &dom.Node{Type: dom.ElementNode, Data: "div", DataAtom: atom.Div})
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrTooDeep, err)
		}
		for _, k := range kids {
			switch {
			case k.Type == dom.TextNode && strings.TrimLeft(k.Data, " \t\n\f\r") == "":
				// White space between flex items generates no box (CSS
				// Flexbox 1, 4); dropping it keeps the block fallback's
				// items side by side alone.
			case k.Type == dom.ElementNode && (k.DataAtom == atom.Style || k.DataAtom == atom.Link):
				// A stylesheet applies to the whole template page wherever
				// it sits; in the head it makes no item of the fallback.
				head.AppendChild(k)
			default:
				f.box.AppendChild(k)
			}
		}
	}
	values := map[string]string{
		"date": chromiumDate(p.o.Date), "title": p.meta.Title, "url": p.o.BaseURL,
		"pageNumber": strconv.Itoa(page), "totalPages": strconv.Itoa(pages),
	}
	for _, cls := range templateClasses {
		for _, n := range elementsOfClass(doc, cls) {
			for c := n.FirstChild; c != nil; c = n.FirstChild {
				n.RemoveChild(c)
			}
			n.AppendChild(&dom.Node{Type: dom.TextNode, Data: values[cls]})
		}
	}
	for i, n := range elementsOfClass(doc, "text") {
		hf.texts = append(hf.texts, n)
		hf.ids[n] = "litho-template-text-" + strconv.Itoa(i)
	}
	return hf, nil
}

// elementsOfClass returns the elements of doc whose class attribute lists
// cls, in document order.
func elementsOfClass(doc *dom.Node, cls string) []*dom.Node {
	var out []*dom.Node
	walk(doc, func(n *dom.Node) bool {
		if n.Type == dom.ElementNode && n.DataAtom != atom.Template {
			for _, c := range strings.Fields(attrOr(n, "class")) {
				if c == cls {
					out = append(out, n)
					break
				}
			}
		}
		return true
	})
	return out
}

// attrOr returns the value of n's attribute key, "" when it has none.
func attrOr(n *dom.Node, key string) string {
	v, _ := attr(n, key)
	return v
}

// setStyle sets n's style attribute to val, as the template's script sets
// element.style.
func setStyle(n *dom.Node, val string) {
	for i := range n.Attr {
		if n.Attr[i].Namespace == "" && n.Attr[i].Key == "style" {
			n.Attr[i].Val = val
			return
		}
	}
	n.Attr = append(n.Attr, dom.Attribute{Key: "style", Val: val})
}

// ptString formats a length in points for a style attribute.
func ptString(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
