package html

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"strings"

	dom "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// collector parses the document's stylesheets. It lives for one [Convert]
// call.
type collector struct {
	res    *resources
	base   string
	logger *slog.Logger

	styles   int
	warnings []compose.Warning
}

// stylesheets returns the sheets of the cascade in order (CSS Cascade 4,
// 6.2): the user agent stylesheet, the user stylesheets user in the order
// given, then the author's <style> and <link rel=stylesheet> elements in
// document order (HTML Standard, 4.2.6 and 4.6.7.18).
func (c *collector) stylesheets(ctx context.Context, user []string, elems []*dom.Node) []css.Sheet {
	sheets := []css.Sheet{{Sheet: css.HTMLUserAgent(), Origin: css.UserAgent}}
	for i, body := range user {
		if ctx.Err() != nil {
			break
		}
		if s := c.userSheet(ctx, i+1, body); s != nil {
			sheets = append(sheets, css.Sheet{Sheet: s, Origin: css.User})
		}
	}
	for _, n := range elems {
		if ctx.Err() != nil {
			break
		}
		var s *css.Stylesheet
		if n.DataAtom == atom.Style {
			s = c.styleElement(ctx, n)
		} else {
			s = c.linkElement(ctx, n)
		}
		if s != nil {
			sheets = append(sheets, css.Sheet{Sheet: s, Origin: css.Author})
		}
	}
	return sheets
}

// userSheet parses body, the n-th user stylesheet. It is held to the caps
// of a resource and charged to the conversion's budget as a linked
// stylesheet is, and left out with the same warning past them; its url()
// values and imports resolve against the document's base, as a <style>
// element's do.
func (c *collector) userSheet(ctx context.Context, n int, body string) *css.Stylesheet {
	name := fmt.Sprintf("user stylesheet %d", n)
	if err := c.res.admit(ctx, len(body)); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		c.warn(warningCode(err), fmt.Sprintf("the %s was left out: %v", name, err))
		return nil
	}
	// The query keeps its key distinct from the document's own URL and
	// from every <style> element's.
	key := resolveRef(c.base, fmt.Sprintf("?litho-user-style=%d", n))
	return c.parse(ctx, []byte(body), key, "", name)
}

// styleElement parses a <style> element whose type is CSS (HTML Standard,
// 4.2.6): an absent or empty type, or text/css.
func (c *collector) styleElement(ctx context.Context, n *dom.Node) *css.Stylesheet {
	if t, ok := attr(n, "type"); ok && strings.TrimSpace(t) != "" && !strings.EqualFold(strings.TrimSpace(t), "text/css") {
		return nil
	}
	c.styles++
	// A style element's url() values and imports resolve against the
	// document's base; the query keeps its key distinct from the
	// document's own URL and from every other style element's.
	key := resolveRef(c.base, fmt.Sprintf("?litho-style=%d", c.styles))
	media, _ := attr(n, "media")
	return c.parse(ctx, []byte(textContent(n)), key, media, "")
}

// linkElement parses the stylesheet a <link rel=stylesheet href> names
// (HTML Standard, 4.6.7.18): not an alternate one, and of type text/css
// when a type is given.
func (c *collector) linkElement(ctx context.Context, n *dom.Node) *css.Stylesheet {
	rel, _ := attr(n, "rel")
	isSheet, alternate := false, false
	for tok := range strings.FieldsSeq(strings.ToLower(rel)) {
		switch tok {
		case "stylesheet":
			isSheet = true
		case "alternate":
			alternate = true
		}
	}
	href, _ := attr(n, "href")
	if !isSheet || alternate || strings.TrimSpace(href) == "" {
		return nil
	}
	if t, ok := attr(n, "type"); ok && strings.TrimSpace(t) != "" {
		mt, _, err := mime.ParseMediaType(t)
		if err != nil || mt != "text/css" {
			return nil
		}
	}
	f, err := c.res.fetchTyped(ctx, resolveRef(c.base, href))
	if err == nil && f.mediaType != "" && f.mediaType != "text/css" {
		// A stylesheet served under another type is not applied (HTML
		// Standard, 4.6.7.18, the fetch and process steps for a
		// standards-mode document; Chromium refuses it cross-origin).
		err = fmt.Errorf("%w: it is served as %s", errStylesheetMIMEType, f.mediaType)
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		c.warn(warningCode(err), fmt.Sprintf("the stylesheet %s was left out: %v", displayURL(strings.TrimSpace(href)), err))
		return nil
	}
	media, _ := attr(n, "media")
	// Its url() values and imports resolve against its own URL, the
	// response's after redirects (CSS Values 4, 4.5.1).
	return c.parse(ctx, f.data, f.url, media, "")
}

// parse parses body, the stylesheet at u, under the media query list
// media, "" for all. A warning names the sheet name, its URL when name is
// empty. A media list is applied by importing the sheet under
// it, so its rules carry the condition as @import gives it (CSS Cascade 4,
// 2) and its own imports stay valid.
func (c *collector) parse(ctx context.Context, body []byte, u, media, name string) *css.Stylesheet {
	fetch := func(ctx context.Context, target string) ([]byte, string, error) {
		if target == u {
			return body, u, nil
		}
		f, err := c.res.fetchTyped(ctx, target)
		return f.data, f.url, err
	}
	src, base := body, u
	if m := strings.TrimSpace(media); m != "" && !strings.EqualFold(m, "all") {
		if strings.ContainsAny(m, ";{}\"'\\") {
			// No valid media query list holds these, so it matches
			// nothing (Media Queries 4, 3.2, not all).
			return nil
		}
		src, base = []byte(`@import url("`+cssString(u)+`") `+m+";"), ""
	}
	s, err := css.Parse(ctx, src, css.ParseOptions{BaseURL: base, Fetch: fetch, Logger: c.logger})
	if err != nil {
		if errors.Is(err, css.ErrTooLarge) {
			// The rules read before the cap still apply, as a browser
			// applies a stylesheet cut short.
			if name == "" {
				name = "stylesheet " + displayURL(u)
			}
			c.warn("css-limit", fmt.Sprintf("the %s was cut short: %v", name, displayURL(err.Error())))
			return s
		}
		// The only other error is ctx's, which the conversion returns at
		// its next check.
		return nil
	}
	return s
}

// warn records a warning.
func (c *collector) warn(code, msg string) {
	c.warnings = append(c.warnings, compose.Warning{Code: code, Message: msg})
}

// cssString escapes s for a double-quoted CSS string (CSS Syntax 3, 4.3.5).
func cssString(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n', '\r', '\f':
			fmt.Fprintf(&b, "\\%x ", r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
