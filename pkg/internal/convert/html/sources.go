package html

import (
	"mime"
	"slices"
	"strconv"
	"strings"

	dom "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// maxSrcsetCandidates caps the image candidates read from one srcset
// attribute: 64. Anti-DoS guard, not spec-mandated: the candidates are
// sorted, and a responsive image lists a handful of widths or densities.
// Candidates past the cap are ignored.
const maxSrcsetCandidates = 64

// maxPictureSources caps the <source> elements of one <picture> the
// selection considers: 32. Anti-DoS guard, not spec-mandated: each may
// evaluate a media query and a sizes attribute, and a picture offers a
// few formats at a few breakpoints. Sources past the cap are ignored.
const maxPictureSources = 32

// pictureTypes are the image media types litho decodes, what a <source
// type> must name to be chosen (HTML Standard, 4.8.4.3.6, update the
// source set: a type the user agent does not support skips the source).
// Immutable lookup table.
var pictureTypes = map[string]bool{
	"image/png": true, "image/apng": true, "image/jpeg": true, "image/pjpeg": true, "image/jpg": true,
	"image/gif": true, "image/webp": true, "image/bmp": true, "image/x-ms-bmp": true, "image/tiff": true,
	"image/svg+xml": true,
}

// srcCandidate is one image candidate of a srcset attribute (HTML
// Standard, 4.8.4.3.10): its URL and its pixel density, or its width in
// CSS pixels for a w descriptor.
type srcCandidate struct {
	url     string
	density float64
	width   int
}

// imageSource returns the URL, as the author wrote it, of the picture the
// <img> n shows, "" for none, and the pixel density of its candidate,
// which divides the picture's natural size (HTML Standard, 4.8.4.3.6,
// density-corrected natural width and height: a 2x picture draws at half
// its pixel size). It follows the HTML Standard's selection of
// an image source (4.8.4.3.6 and 4.8.4.3.8) for a device pixel ratio of
// 1, Chromium's when it prints:
//
//   - In a <picture>, the first <source> before n with a srcset, whose
//     media matches the medium and whose type litho decodes, gives the
//     source set; without one, n's own srcset and src do.
//   - Its candidates' densities come from their x descriptors, 1 without
//     one, or for w descriptors from the width divided by the slot size
//     its sizes attribute gives (4.8.4.3.11), 100vw without one.
//   - n's src joins its own srcset as a 1x candidate unless one already
//     is 1x or a candidate has a w descriptor.
//   - The candidate is the one of lowest density at least 1, else the one
//     of highest density: the smallest picture that does not upscale, as
//     Chromium picks it.
func (b *builder) imageSource(n *dom.Node) (string, float64) {
	if p := n.Parent; p != nil && p.Type == dom.ElementNode && p.DataAtom == atom.Picture && p.Namespace == "" {
		seen := 0
		for c := p.FirstChild; c != nil && c != n && seen < maxPictureSources; c = c.NextSibling {
			if c.Type != dom.ElementNode || c.DataAtom != atom.Source || c.Namespace != "" {
				continue
			}
			seen++
			if u, d, ok := b.sourceElement(c); ok {
				return u, d
			}
		}
	}
	src, _ := attr(n, "src")
	srcset, _ := attr(n, "srcset")
	cands := parseSrcset(srcset)
	if strings.Trim(src, "\t\n\f\r ") != "" && !slices.ContainsFunc(cands, func(c srcCandidate) bool {
		return c.width > 0 || c.density == 1
	}) {
		cands = append(cands, srcCandidate{url: src, density: 1})
	}
	if len(cands) == 0 {
		return src, 1
	}
	sizes, _ := attr(n, "sizes")
	return pickCandidate(cands, b.slotSize(sizes))
}

// sourceElement returns the URL a <source> of a <picture> selects, false
// when it is skipped: no srcset candidate, a media that does not match or
// a type litho does not decode.
func (b *builder) sourceElement(s *dom.Node) (string, float64, bool) {
	srcset, _ := attr(s, "srcset")
	cands := parseSrcset(srcset)
	if len(cands) == 0 {
		return "", 0, false
	}
	if m, ok := attr(s, "media"); ok && strings.TrimSpace(m) != "" && !css.MatchMedia(m, b.media) {
		return "", 0, false
	}
	if t, ok := attr(s, "type"); ok && strings.TrimSpace(t) != "" {
		mt, _, err := mime.ParseMediaType(t)
		if err != nil || !pictureTypes[mt] {
			return "", 0, false
		}
	}
	sizes, _ := attr(s, "sizes")
	u, d := pickCandidate(cands, b.slotSize(sizes))
	return u, d, true
}

// pickCandidate returns the URL and density of the candidate of lowest
// density at least 1, else of highest density, slot being the slot size in
// CSS pixels that turns a width into a density. Ties keep the first in
// source order.
func pickCandidate(cands []srcCandidate, slot float64) (string, float64) {
	type dc struct {
		url     string
		density float64
	}
	ds := make([]dc, 0, len(cands))
	for _, c := range cands {
		d := c.density
		if c.width > 0 {
			d = float64(c.width) / slot
		}
		ds = append(ds, dc{url: c.url, density: d})
	}
	slices.SortStableFunc(ds, func(a, b dc) int {
		switch {
		case a.density < b.density:
			return -1
		case a.density > b.density:
			return 1
		}
		return 0
	})
	for _, d := range ds {
		if d.density >= 1 {
			return d.url, d.density
		}
	}
	last := ds[len(ds)-1]
	return last.url, last.density
}

// parseSrcset parses a srcset attribute (HTML Standard, 4.8.4.3.10, parse
// a srcset attribute): comma-separated candidates, each a URL and at most
// one descriptor of each of w, x and h. A candidate with an invalid or a
// repeated descriptor, both w and x, h without w, or a zero width or
// density is dropped. At most [maxSrcsetCandidates] are kept.
func parseSrcset(s string) []srcCandidate {
	var out []srcCandidate
	i := 0
	for len(out) < maxSrcsetCandidates {
		for i < len(s) && (isHTMLSpace(s[i]) || s[i] == ',') {
			i++
		}
		if i >= len(s) {
			break
		}
		start := i
		for i < len(s) && !isHTMLSpace(s[i]) {
			i++
		}
		u := s[start:i]
		var descs []string
		if strings.HasSuffix(u, ",") {
			u = strings.TrimRight(u, ",")
		} else {
			descs, i = srcsetDescriptors(s, i)
		}
		if c, ok := srcsetCandidate(u, descs); ok {
			out = append(out, c)
		}
	}
	return out
}

// srcsetDescriptors tokenizes the descriptors that follow a candidate's
// URL from s[i:], up to the comma that ends the candidate outside
// parentheses, and returns them with the index after that comma.
func srcsetDescriptors(s string, i int) ([]string, int) {
	var descs []string
	var cur strings.Builder
	inParens := false
	flush := func() {
		if cur.Len() > 0 {
			descs = append(descs, cur.String())
			cur.Reset()
		}
	}
	for ; i < len(s); i++ {
		c := s[i]
		switch {
		case inParens:
			cur.WriteByte(c)
			if c == ')' {
				inParens = false
			}
		case isHTMLSpace(c):
			flush()
		case c == ',':
			flush()
			return descs, i + 1
		case c == '(':
			cur.WriteByte(c)
			inParens = true
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return descs, i
}

// srcsetCandidate parses one candidate's descriptors.
func srcsetCandidate(u string, descs []string) (srcCandidate, bool) {
	c := srcCandidate{url: u}
	if u == "" {
		return c, false
	}
	hasW, hasX, hasH := false, false, false
	for _, d := range descs {
		if len(d) < 2 {
			return c, false
		}
		num := d[:len(d)-1]
		switch d[len(d)-1] {
		case 'w':
			v, ok := htmlInteger(num)
			if hasW || hasX || !ok || v <= 0 {
				return c, false
			}
			hasW, c.width = true, v
		case 'x':
			v, ok := htmlFloat(num)
			// A zero density would make the picture infinitely large;
			// Chromium drops it too.
			if hasW || hasX || hasH || !ok || v <= 0 {
				return c, false
			}
			hasX, c.density = true, v
		case 'h':
			v, ok := htmlInteger(num)
			if hasH || hasX || !ok || v <= 0 {
				return c, false
			}
			hasH = true
		default:
			return c, false
		}
	}
	if hasH && !hasW {
		return c, false
	}
	if !hasW && !hasX {
		c.density = 1
	}
	return c, true
}

// slotSize returns the slot size in CSS pixels a sizes attribute gives
// (HTML Standard, 4.8.4.3.11, parse a sizes attribute): the length of the
// first entry whose media condition matches the medium, or that has none,
// 100vw when no entry does. A length in calc() or another function is
// not read: its entry is skipped.
func (b *builder) slotSize(sizes string) float64 {
	vw := b.media.Width / css.Pt
	if vw <= 0 {
		vw = DefaultPageWidth / css.Pt
	}
	for _, entry := range splitTopLevel(sizes) {
		entry = strings.Trim(entry, "\t\n\f\r ")
		if entry == "" {
			continue
		}
		cond, value := "", entry
		if k := strings.LastIndexAny(entry, "\t\n\f\r )"); k >= 0 {
			cond, value = entry[:k+1], entry[k+1:]
		}
		px, ok := sizeLength(value, b.media)
		if !ok || px <= 0 {
			continue
		}
		if cond = strings.Trim(cond, "\t\n\f\r "); cond != "" && (!isMediaCondition(cond) || !css.MatchMedia(cond, b.media)) {
			continue
		}
		return px
	}
	return vw
}

// isMediaCondition reports a text that starts as a <media-condition>
// does (Media Queries 4, 3): a parenthesised test, or "not" before one.
// A media type, which a media query list allows and a sizes attribute
// does not, fails it.
func isMediaCondition(cond string) bool {
	if rest, ok := strings.CutPrefix(strings.ToLower(cond), "not"); ok && rest != "" && (isHTMLSpace(rest[0]) || rest[0] == '(') {
		cond = strings.TrimLeft(rest, "\t\n\f\r ")
	}
	return strings.HasPrefix(cond, "(")
}

// splitTopLevel splits s at the commas outside parentheses.
func splitTopLevel(s string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth = max(depth-1, 0)
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// sizeUnits are the CSS pixels of one unit of each absolute length and of
// the font-relative ones at the initial font size, 16px (CSS Values 4,
// 6.2; HTML Standard, 4.8.4.3.11 resolves em against the initial value).
// The viewport units are resolved in [sizeLength]. Immutable lookup table.
var sizeUnits = map[string]float64{
	"px": 1, "em": 16, "rem": 16, "ex": 8, "ch": 8,
	"pt": 96.0 / 72, "pc": 16, "in": 96, "cm": 96 / 2.54, "mm": 96 / 25.4, "q": 96 / 101.6,
}

// sizeLength parses a <source-size-value> length in CSS pixels, with m
// the medium the viewport units measure. A unitless zero is a length.
func sizeLength(v string, m css.Media) (float64, bool) {
	v = strings.ToLower(v)
	end := 0
	for end < len(v) && (v[end] >= '0' && v[end] <= '9' || v[end] == '.' || v[end] == '-' || v[end] == '+' || v[end] == 'e' && end > 0 && end+1 < len(v) && (v[end+1] >= '0' && v[end+1] <= '9' || v[end+1] == '-' || v[end+1] == '+')) {
		end++
	}
	n, ok := htmlFloat(v[:end])
	if !ok {
		return 0, false
	}
	unit := v[end:]
	w, h := m.Width/css.Pt, m.Height/css.Pt
	switch unit {
	case "":
		return 0, n == 0
	case "vw":
		return n * w / 100, true
	case "vh":
		return n * h / 100, true
	case "vmin":
		return n * min(w, h) / 100, true
	case "vmax":
		return n * max(w, h) / 100, true
	}
	if f, ok := sizeUnits[unit]; ok {
		return n * f, true
	}
	return 0, false
}

// htmlInteger parses a valid non-negative integer (HTML Standard,
// 2.3.4.1): ASCII digits alone, at most 9 of them.
func htmlInteger(s string) (int, bool) {
	if s == "" || len(s) > 9 {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	v, err := strconv.Atoi(s)
	return v, err == nil
}

// htmlFloat parses a valid floating-point number (HTML Standard, 2.3.4.3):
// an optional minus, digits with an optional fraction or a fraction
// alone, and an optional exponent. A leading plus is accepted, as the CSS
// numbers of a sizes attribute allow it.
func htmlFloat(s string) (float64, bool) {
	i := 0
	if i < len(s) && (s[i] == '-' || s[i] == '+') {
		i++
	}
	digits := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
		digits++
	}
	if i < len(s) && s[i] == '.' {
		i++
		frac := 0
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
			frac++
		}
		if frac == 0 {
			return 0, false
		}
		digits += frac
	}
	if digits == 0 {
		return 0, false
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '-' || s[i] == '+') {
			i++
		}
		exp := 0
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
			exp++
		}
		if exp == 0 {
			return 0, false
		}
	}
	if i != len(s) {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil && v-v == 0
}

// isHTMLSpace reports ASCII whitespace (HTML Standard, Infra 4.6).
func isHTMLSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r'
}
