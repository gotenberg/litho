package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// The selectors of the converter corpora whose sources are test suites:
// resvg's SVG tests and the web-platform-tests reftests. Each writes its own
// list (tests.json, reftests.json) and files.sha256, the hash of every file
// the list names plus the licences and fonts, sorted by path.

// resvgAreas are the area directories under crates/resvg/tests/tests/ and
// the most tests each keeps.
var resvgAreas = map[string]int{
	"structure/svg": 16, "structure/g": 4, "structure/defs": 4, "structure/use": 10, "structure/symbol": 8,
	"structure/switch": 6, "structure/systemLanguage": 3, "structure/style": 10, "structure/style-attribute": 4,
	"structure/transform": 12, "shapes/rect": 12, "shapes/circle": 6, "shapes/ellipse": 6, "shapes/line": 6,
	"shapes/polyline": 4, "shapes/polygon": 4, "shapes/path": 16, "painting/fill": 10, "painting/fill-opacity": 3,
	"painting/fill-rule": 3, "painting/stroke": 5, "painting/stroke-dasharray": 6, "painting/stroke-dashoffset": 3,
	"painting/stroke-linecap": 3, "painting/stroke-linejoin": 4, "painting/stroke-miterlimit": 3,
	"painting/stroke-opacity": 3, "painting/stroke-width": 4, "painting/opacity": 4, "painting/display": 4,
	"painting/visibility": 4, "painting/color": 4, "paint-servers/linearGradient": 12,
	"paint-servers/radialGradient": 10, "paint-servers/stop": 6, "paint-servers/stop-color": 3,
	"paint-servers/stop-opacity": 3, "masking/clipPath": 10, "masking/clip-rule": 2, "text/text": 6,
	"text/tspan": 4, "text/text-anchor": 3, "text/font-size": 3,
}

// resvgReject are the constructs a test may not use, each with the reason it
// is rejected.
var resvgReject = []struct {
	re  *regexp.Regexp
	why string
}{
	{regexp.MustCompile(`<filter|\bfilter\s*[=:]`), "filter"},
	{regexp.MustCompile(`<mask|\bmask\s*[=:]`), "mask"},
	{regexp.MustCompile(`<pattern`), "pattern"},
	{regexp.MustCompile(`<marker|marker-(start|mid|end)|\bmarker\s*[=:]`), "marker"},
	{regexp.MustCompile(`<animate|<set\b`), "animation"},
	{regexp.MustCompile(`<foreignObject`), "foreignObject"},
	{regexp.MustCompile(`<textPath`), "textPath"},
	{regexp.MustCompile(`<image\b`), "image"},
	{regexp.MustCompile(`<script|\bon[a-z]+\s*=`), "script"},
	{regexp.MustCompile(`<!ENTITY`), "XML entity"},
	{regexp.MustCompile(`@import|@font-face|@media`), "at-rule"},
	{regexp.MustCompile(`context-fill|context-stroke`), "context paint"},
	{regexp.MustCompile(`mix-blend-mode|isolation|paint-order|vector-effect`), "compositing"},
	{regexp.MustCompile(`transform-origin|transform-box|\btransform\s*:`), "CSS transform"},
	{regexp.MustCompile(`shape-rendering|text-rendering|image-rendering|color-rendering|color-interpolation`), "rendering hint"},
	{regexp.MustCompile(`textLength|lengthAdjust|\brotate\s*=`), "text adjustment"},
	{regexp.MustCompile(`letter-spacing|word-spacing|text-decoration|baseline-shift|dominant-baseline|alignment-baseline`), "text property"},
	{regexp.MustCompile(`font-variant|font-stretch|font-kerning|font-feature|font-variation|writing-mode|direction|unicode-bidi`), "text property"},
	{regexp.MustCompile(`\bfont\s*[=:]|font-size-adjust`), "font shorthand"},
	{regexp.MustCompile(`spreadMethod\s*=\s*"(reflect|repeat)"`), "spread method"},
	{regexp.MustCompile(`<style[^>]*>[^<]*:(first|last|nth|not|hover)`), "pseudo-class"},
	{regexp.MustCompile(`\bcalc\(|\bvar\(`), "CSS function"},
	{regexp.MustCompile(`\b\d*\.?\d+(vw|vh|vmin|vmax|ch|ic|lh|rlh|cap|rem)\b`), "unit"},
}

// resvgHref finds an href, which may only point inside the document.
var resvgHref = regexp.MustCompile(`href\s*=\s*"`)

// resvgUndefined finds the title resvg gives a test whose expected rendering
// no specification decides: "(UB)", undefined behaviour.
var resvgUndefined = regexp.MustCompile(`<title>[^<]*\(UB\)`)

// resvgExtras are the files the tests read beside their own: the fonts the
// text tests draw with, their licences, and resvg's licence.
var resvgExtras = []string{
	"crates/resvg/tests/fonts/Amiri-LICENSE-OFL.txt", "crates/resvg/tests/fonts/Amiri-Regular.ttf",
	"crates/resvg/tests/fonts/Noto-LICENSE-OFL.txt", "crates/resvg/tests/fonts/NotoMono-Regular.ttf",
	"crates/resvg/tests/fonts/NotoSans-Bold.ttf", "crates/resvg/tests/fonts/NotoSans-Italic.ttf",
	"crates/resvg/tests/fonts/NotoSans-Regular.ttf", "crates/resvg/tests/fonts/NotoSerif-Regular.ttf",
	"LICENSE-MIT",
}

// resvgTest is one entry of tests.json.
type resvgTest struct {
	ID     string `json:"id"`
	PNG    string `json:"png"`
	Width  uint32 `json:"width"`
	Height uint32 `json:"height"`
}

// selectResvg selects the resvg tests of each area: an SVG with a PNG
// reference, using none of the constructs resvgReject lists, of a defined
// rendering, in the order of the SHA-256 of its path, the first of the
// area's cap kept.
func selectResvg(_ context.Context, checkout, _, dir string, _ map[string]string) error {
	const testsDir = "crates/resvg/tests/tests"
	root := filepath.Join(checkout, filepath.FromSlash(testsDir))
	areas := make([]string, 0, len(resvgAreas))
	for a := range resvgAreas {
		areas = append(areas, a)
	}
	sort.Strings(areas)
	var chosen []resvgTest
	total := 0
	for _, area := range areas {
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(area)))
		if err != nil {
			return fmt.Errorf("area %s: %w", area, err)
		}
		type cand struct {
			key string
			t   resvgTest
		}
		var cands []cand
		for _, e := range entries {
			name := e.Name()
			if !strings.HasSuffix(name, ".svg") {
				continue
			}
			rel := area + "/" + name
			png := filepath.Join(root, filepath.FromSlash(strings.TrimSuffix(rel, ".svg")+".png"))
			if _, err := os.Stat(png); err != nil {
				continue
			}
			src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))) //nolint:gosec // a test of the checkout
			if err != nil {
				return err
			}
			if resvgRejected(src) || resvgUndefined.Match(src) {
				continue
			}
			w, h, ok := pngSize(png)
			if !ok {
				continue
			}
			sum := sha256.Sum256([]byte(rel))
			cands = append(cands, cand{hex.EncodeToString(sum[:]), resvgTest{ID: rel, PNG: strings.TrimSuffix(rel, ".svg") + ".png", Width: w, Height: h}})
		}
		total += len(cands)
		sort.Slice(cands, func(i, j int) bool { return cands[i].key < cands[j].key })
		for _, c := range cands[:min(resvgAreas[area], len(cands))] {
			chosen = append(chosen, c.t)
		}
	}
	sort.Slice(chosen, func(i, j int) bool { return chosen[i].ID < chosen[j].ID })
	fmt.Fprintf(os.Stderr, "resvg: %d candidates, %d chosen\n", total, len(chosen))
	paths := slices.Clone(resvgExtras)
	for _, t := range chosen {
		paths = append(paths, testsDir+"/"+t.ID, testsDir+"/"+t.PNG)
	}
	if err := writeJSON(filepath.Join(dir, "tests.json"), chosen); err != nil {
		return err
	}
	return writeHashList(checkout, dir, paths)
}

func resvgRejected(src []byte) bool {
	for _, r := range resvgReject {
		if r.re.Match(src) {
			return true
		}
	}
	// An href naming anything but a fragment reads an external file.
	for _, loc := range resvgHref.FindAllIndex(src, -1) {
		if loc[1] < len(src) && src[loc[1]] != '#' {
			return true
		}
	}
	return false
}

// pngSize returns the width and height of the PNG at p.
func pngSize(p string) (uint32, uint32, bool) {
	f, err := os.Open(p) //nolint:gosec // a reference of the checkout
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	head := make([]byte, 24)
	if _, err := io.ReadFull(f, head); err != nil || !bytes.Equal(head[:8], []byte("\x89PNG\r\n\x1a\n")) {
		return 0, 0, false
	}
	return binary.BigEndian.Uint32(head[16:]), binary.BigEndian.Uint32(head[20:]), true
}

// writeHashList writes files.sha256 into dir: "<sha256>  <path>" for each
// path of the checkout, sorted by path, each once.
func writeHashList(checkout, dir string, paths []string) error {
	sort.Strings(paths)
	paths = slices.Compact(paths)
	var b strings.Builder
	for _, p := range paths {
		digest, err := fileDigest(filepath.Join(checkout, filepath.FromSlash(p)))
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s  %s\n", digest, p)
	}
	return writeFileAtomic(filepath.Join(dir, "files.sha256"), []byte(b.String()))
}

// wptDirs are the WPT directories walked for reftests.
var wptDirs = []string{
	"css/CSS2/box-display", "css/CSS2/normal-flow", "css/CSS2/floats", "css/CSS2/floats-clear",
	"css/CSS2/margin-padding-clear", "css/CSS2/borders", "css/CSS2/text", "css/CSS2/lists", "css/CSS2/visuren",
	"css/CSS2/visudet", "css/CSS2/page-box", "css/CSS2/pagination", "css/CSS2/colors", "css/CSS2/backgrounds",
	"css/css-backgrounds", "css/css-text", "css/css-lists", "css/css-page", "css/css-break", "css/css-flexbox",
	"css/css-align", "css/css-grid", "css/css-multicol", "css/css-writing-modes", "css/css-overflow",
	"css/css-text-decor", "css/css-sizing",
	"css/CSS2/tables", "css/CSS2/fonts", "css/CSS2/linebox", "css/CSS2/positioning", "css/CSS2/abspos",
	"css/CSS2/generated-content", "css/CSS2/values", "css/CSS2/selectors", "css/css-tables", "css/css-fonts",
	"css/css-inline", "css/css-position", "css/css-transforms", "css/css-ruby", "css/css-content",
	"css/css-counter-styles", "css/css-values", "css/css-variables", "css/selectors", "css/css-color",
	"css/css-images", "css/css-display",
}

// wptAreaCap bounds the tests one area contributes, an area being a CSS2
// chapter directory or a whole CSS module, so the large ones do not crowd out
// the others; a CSS2 chapter without a cap of its own takes the "css/CSS2"
// cap. An area of a feature the converter does not lay out (ruby) keeps a
// few tests: they say the feature is missing as well as many would.
var wptAreaCap = map[string]int{
	"css/CSS2": 24, "css/css-backgrounds": 80, "css/css-text": 120, "css/css-lists": 40, "css/css-page": 40,
	"css/css-break": 40, "css/css-flexbox": 200, "css/css-align": 20, "css/css-grid": 200, "css/css-multicol": 80,
	"css/css-writing-modes": 60, "css/css-overflow": 40, "css/css-text-decor": 40, "css/css-sizing": 40,
	"css/CSS2/tables": 100, "css/CSS2/fonts": 40, "css/CSS2/positioning": 40, "css/CSS2/generated-content": 60,
	"css/CSS2/selectors": 40, "css/css-tables": 80, "css/css-fonts": 60, "css/css-inline": 60,
	"css/css-position": 60, "css/css-transforms": 100, "css/css-ruby": 10, "css/css-content": 40,
	"css/css-counter-styles": 40, "css/css-values": 60, "css/css-variables": 60, "css/selectors": 60,
	"css/css-color": 60, "css/css-images": 60, "css/css-display": 60,
}

func compileAll(flags string, patterns ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(patterns))
	for i, p := range patterns {
		out[i] = regexp.MustCompile(flags + p)
	}
	return out
}

// The forbidden patterns an area may lift (wptAreaRules.lift).
var (
	wptFontFace = regexp.MustCompile(`(?i)@font-face`)
	wptVertical = regexp.MustCompile(`(?i)vertical-(rl|lr)`)
	wptSideways = regexp.MustCompile(`(?i)sideways`)
)

// wptForbidden keeps a test out: scripts, SVG, MathML, form controls,
// embedded documents and media, @font-face, data: and javascript: URLs, a
// reftest-wait class, vertical writing and event handlers.
var wptForbidden = append(compileAll("(?i)",
	`<script\b`, `<svg\b`, `<math\b`, `<iframe\b`, `<object\b`, `<embed\b`, `<canvas\b`, `<video\b`, `<audio\b`,
	`<input\b`, `<button\b`, `<select\b`, `<textarea\b`, `<frameset\b`, `\bdata:`, `\bjavascript:`,
	`\.svg\b`, `reftest-wait`, `\bon[a-z]+\s*=`), wptFontFace, wptVertical, wptSideways)

// wptTransform3D finds a 3D transform: a perspective, a 3D rendering
// context, a back face, or a 3D transform function (CSS Transforms 2).
var wptTransform3D = regexp.MustCompile(`(?i)perspective|preserve-3d|backface-visibility|matrix3d|` +
	`\b(translate|rotate|scale)(3d|Z)\s*\(|\brotate[XY]\s*\(`)

// wptFlexForbidden are what the flexbox and alignment areas leave out on top
// of wptForbidden: features the converter does not lay out in a flex
// container.
var wptFlexForbidden = compileAll("(?i)",
	`display\s*:\s*(inline-)?grid`, `\bgrid-`, `\bcolumns?(-count|-width)?\s*:`, `\b(min|max|fit)-content\b`,
	`\baspect-ratio\b`, `visibility\s*:\s*collapse`, `display\s*:\s*contents`, `\bcontain(-intrinsic-[a-z-]+)?\s*:`,
	`\bsticky\b`, `\bzoom\s*:`, `-webkit-box\b`, `writing-mode`, `\banchor`, `<details\b`, `<fieldset\b`,
	`flex-wrap\s*:\s*balance`, `flex-line-count`)

// wptLayoutForbidden keeps the tests of the grid, multi-column, writing
// mode, overflow, text decoration and sizing areas to what those areas add.
// A writing mode other than horizontal-tb is checked by
// wptOtherWritingMode, which needs a lookahead the regexp package lacks.
var wptLayoutForbidden = compileAll("(?i)",
	`\bsubgrid\b`, `\bmasonry\b`, `\baspect-ratio\b`, `display\s*:\s*contents`, `\bcontain(-intrinsic-[a-z-]+)?\s*:`,
	`\bsticky\b`, `\bzoom\s*:`, `text-orientation`, `text-combine-upright`, `\banchor`, `<details\b`, `<fieldset\b`,
	`flex-wrap\s*:\s*balance`, `flex-line-count`)

var wptWritingMode = regexp.MustCompile(`(?i)writing-mode\s*:\s*`)

// wptOtherWritingMode reports a writing-mode declaration whose value is not
// horizontal-tb.
func wptOtherWritingMode(text string) bool {
	for _, loc := range wptWritingMode.FindAllStringIndex(text, -1) {
		if !strings.HasPrefix(strings.ToLower(text[loc[1]:]), "horizontal-tb") {
			return true
		}
	}
	return false
}

// wptAreaRules are an area's extra forbidden patterns, the patterns of
// wptForbidden it lifts, whether it forbids another writing mode, and what
// every test of it must hold.
type wptAreaRules struct {
	forbidden   []*regexp.Regexp
	lift        []*regexp.Regexp
	writingMode bool
	required    *regexp.Regexp
}

// wptFontFaceRules are the rules of the areas that take a test with an
// @font-face rule: its fonts resolve inside the test's directory as its
// other resources do, and the converter loads them.
var wptFontFaceRules = wptAreaRules{lift: []*regexp.Regexp{wptFontFace}}

var wptAreas = map[string]wptAreaRules{
	"css/css-flexbox":            {forbidden: wptFlexForbidden},
	"css/css-align":              {forbidden: wptFlexForbidden, required: regexp.MustCompile(`(?i)display\s*:\s*(inline-)?flex\b`)},
	"css/css-grid":               {forbidden: wptLayoutForbidden, writingMode: true},
	"css/css-multicol":           {forbidden: wptLayoutForbidden, writingMode: true},
	"css/css-writing-modes":      {forbidden: wptLayoutForbidden, writingMode: true, required: regexp.MustCompile(`(?i)direction\s*:\s*rtl|unicode-bidi|\bdir\s*=\s*["']?(rtl|auto)`)},
	"css/css-overflow":           {forbidden: wptLayoutForbidden, writingMode: true},
	"css/css-text-decor":         {forbidden: wptLayoutForbidden, writingMode: true},
	"css/css-sizing":             {forbidden: wptLayoutForbidden, writingMode: true},
	"css/CSS2/tables":            wptFontFaceRules,
	"css/CSS2/fonts":             wptFontFaceRules,
	"css/CSS2/linebox":           wptFontFaceRules,
	"css/CSS2/positioning":       wptFontFaceRules,
	"css/CSS2/abspos":            wptFontFaceRules,
	"css/CSS2/generated-content": wptFontFaceRules,
	"css/CSS2/values":            wptFontFaceRules,
	"css/CSS2/selectors":         wptFontFaceRules,
	"css/css-tables":             wptFontFaceRules,
	"css/css-fonts":              wptFontFaceRules,
	"css/css-inline":             wptFontFaceRules,
	"css/css-position":           wptFontFaceRules,
	"css/css-transforms":         {forbidden: []*regexp.Regexp{wptTransform3D}, lift: wptFontFaceRules.lift},
	"css/css-ruby":               wptFontFaceRules,
	"css/css-content":            wptFontFaceRules,
	"css/css-counter-styles":     wptFontFaceRules,
	"css/css-values":             wptFontFaceRules,
	"css/css-variables":          wptFontFaceRules,
	"css/selectors":              wptFontFaceRules,
	"css/css-display":            wptFontFaceRules,
	"css/css-color":              wptFontFaceRules,
	"css/css-images":             wptFontFaceRules,
}

// wptProbe is a part of an area a rule singles out and caps on its own: the
// tests of a feature the converter does not lay out, of which a few say as
// much as many would. Its tests are not candidates of the area's own rules,
// which forbid what the probe requires.
type wptProbe struct {
	name, dir string
	rules     wptAreaRules
	cap       int
}

var wptProbes = []wptProbe{
	{
		name: "css/css-writing-modes vertical", dir: "css/css-writing-modes", cap: 10,
		rules: wptAreaRules{
			forbidden: wptLayoutForbidden, lift: []*regexp.Regexp{wptVertical, wptSideways},
			required: regexp.MustCompile(`(?i)writing-mode\s*:\s*(vertical-(rl|lr)|sideways-(rl|lr))`),
		},
	},
	{
		name: "css/css-transforms 3D", dir: "css/css-transforms", cap: 10,
		rules: wptAreaRules{lift: wptFontFaceRules.lift, required: wptTransform3D},
	},
}

// wptSkipDirs hold no reftest of their own, tests of another kind, or
// reftests of layout modes outside the converter's scope.
var wptSkipDirs = []string{
	"support", "reference", "resources", "crashtests", "tentative", "animations", "parsing", "inheritance",
	"flexbox", "grid", "table", "multicol", "writing-mode", "subgrid", "masonry", "grid-lanes", "test-plan",
	"tools", "forms", "scroll-markers", "invalidation", "responsive-iframe", "contain-intrinsic-size",
	"aspect-ratio", "animation",
}

var (
	wptBadFlags = []string{"interact", "userstyle", "dom", "font", "svg", "animated", "nonHTML", "speech", "scroll"}
	wptVoid     = []string{"area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "param", "source", "track", "wbr"}
)

// wptAhemCSS is the one absolute URL a test may name: the corpus installs Ahem
// as a font instead.
const wptAhemCSS = "/fonts/ahem.css"

var (
	wptCSSURL     = regexp.MustCompile(`(?i)url\(\s*'([^'")]+)'\s*\)|url\(\s*"([^'")]+)"\s*\)|url\(\s*([^'")]+)\s*\)|@import\s+'([^'"]+)'|@import\s+"([^'"]+)"`)
	wptSelfClosed = regexp.MustCompile(`<([a-zA-Z][a-zA-Z0-9]*)\b[^<>]*/>`)
	wptScheme     = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*:`)
	wptFuzzyRef   = regexp.MustCompile(`^([^:;=]+\.[a-z]+):(.*)$`)
)

// wptScan is what a test or reference declares: its references, fuzziness,
// pages, flags and the resources it names.
type wptScan struct {
	match, mismatch, fuzzy []string
	flags                  map[string]bool
	pages                  string
	urls, styles           []string
	printClass             bool
}

func scanWPT(text string) *wptScan {
	s := &wptScan{flags: map[string]bool{}}
	z := html.NewTokenizer(strings.NewReader(text))
	inStyle := false
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return s
		case html.TextToken:
			if inStyle {
				s.styles = append(s.styles, string(z.Text()))
			}
		case html.EndTagToken:
			if name, _ := z.TagName(); string(name) == "style" {
				inStyle = false
			}
		case html.CommentToken, html.DoctypeToken:
			// Neither declares a reference, a resource or a style.
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			a := map[string]string{}
			for hasAttr {
				var k, v []byte
				k, v, hasAttr = z.TagAttr()
				a[string(k)] = string(v)
			}
			s.tag(string(name), a)
			if string(name) == "style" && tt == html.StartTagToken {
				inStyle = true
			}
		}
	}
}

func (s *wptScan) tag(name string, a map[string]string) {
	switch name {
	case "link":
		rel := strings.Fields(strings.ToLower(a["rel"]))
		switch {
		case slices.Contains(rel, "match"):
			s.match = append(s.match, a["href"])
		case slices.Contains(rel, "mismatch"):
			s.mismatch = append(s.mismatch, a["href"])
		case slices.Contains(rel, "stylesheet"):
			s.urls = append(s.urls, a["href"])
		}
	case "meta":
		switch strings.ToLower(a["name"]) {
		case "fuzzy":
			s.fuzzy = append(s.fuzzy, a["content"])
		case "reftest-pages":
			s.pages = strings.TrimSpace(a["content"])
		case "flags":
			for f := range strings.FieldsSeq(a["content"]) {
				s.flags[f] = true
			}
		}
	case "img":
		s.urls = append(s.urls, a["src"])
	case "html":
		if slices.Contains(strings.Fields(a["class"]), "reftest-print") {
			s.printClass = true
		}
	}
	if v, ok := a["style"]; ok {
		s.styles = append(s.styles, v)
	}
	if v, ok := a["background"]; ok {
		s.urls = append(s.urls, v)
	}
}

func cssURLs(text string) []string {
	var out []string
	for _, m := range wptCSSURL.FindAllStringSubmatch(text, -1) {
		for _, g := range m[1:] {
			if g != "" {
				out = append(out, g)
				break
			}
		}
	}
	return out
}

func xhtParsesAsHTML(text string) bool {
	if strings.Contains(text, "<![CDATA[") {
		return false
	}
	for _, m := range wptSelfClosed.FindAllStringSubmatch(text, -1) {
		if !slices.Contains(wptVoid, strings.ToLower(m[1])) {
			return false
		}
	}
	return true
}

var errWPTRejected = errors.New("rejected")

// wptRejectionError is errWPTRejected with the rule that kept a file out.
type wptRejectionError struct{ reason string }

func (r *wptRejectionError) Error() string        { return "rejected: " + r.reason }
func (r *wptRejectionError) Is(target error) bool { return target == errWPTRejected }

func rejectWPT(reason string) error { return &wptRejectionError{reason: reason} }

// wptRejectReason returns the rule a rejection names, empty for another
// error.
func wptRejectReason(err error) string {
	var r *wptRejectionError
	if errors.As(err, &r) {
		return r.reason
	}
	return ""
}

// wptForbids returns the forbidden pattern text holds, empty when none.
func wptForbids(text string, rules wptAreaRules) string {
	for _, re := range append(slices.Clone(wptForbidden), rules.forbidden...) {
		if slices.Contains(rules.lift, re) {
			continue
		}
		if re.MatchString(text) {
			return strings.TrimPrefix(re.String(), "(?i)")
		}
	}
	if rules.writingMode && wptOtherWritingMode(text) {
		return "writing-mode other than horizontal-tb"
	}
	return ""
}

func readText(p string) (string, error) {
	data, err := os.ReadFile(p) //nolint:gosec // a file of the checkout
	return string(data), err
}

// checkWPTFile scans one test or reference and returns what it declares and
// the resources it reads, or errWPTRejected.
func checkWPTFile(root, rel string, rules wptAreaRules) (*wptScan, []string, error) {
	p := filepath.Join(root, filepath.FromSlash(rel))
	if info, err := os.Stat(p); err != nil || !info.Mode().IsRegular() {
		return nil, nil, rejectWPT("missing file")
	}
	text, err := readText(p)
	if err != nil {
		return nil, nil, err
	}
	if why := wptForbids(text, rules); why != "" {
		return nil, nil, rejectWPT(why)
	}
	if strings.HasSuffix(rel, ".xht") && !xhtParsesAsHTML(text) {
		return nil, nil, rejectWPT("XHTML that does not parse as HTML")
	}
	s := scanWPT(text)
	for _, f := range wptBadFlags {
		if s.flags[f] {
			return nil, nil, rejectWPT("flag " + f)
		}
	}
	base := path.Dir(rel)
	if base == "." {
		base = ""
	}
	pending := slices.Clone(s.urls)
	for _, st := range s.styles {
		pending = append(pending, cssURLs(st)...)
	}
	seen := map[string]bool{}
	resources := map[string]bool{}
	for len(pending) > 0 {
		u := strings.TrimSpace(pending[len(pending)-1])
		pending = pending[:len(pending)-1]
		if u == "" || seen[u] || strings.HasPrefix(u, "#") {
			continue
		}
		seen[u] = true
		if u == wptAhemCSS {
			continue
		}
		if strings.HasPrefix(u, "/") || wptScheme.MatchString(u) {
			return nil, nil, rejectWPT("absolute or remote URL")
		}
		u, _, _ = strings.Cut(u, "#")
		u, _, _ = strings.Cut(u, "?")
		target := path.Clean(path.Join(base, u))
		if base != "" && !strings.HasPrefix(target+"/", base+"/") {
			return nil, nil, rejectWPT("resource outside the test's directory")
		}
		tp := filepath.Join(root, filepath.FromSlash(target))
		if info, err := os.Stat(tp); err != nil || !info.Mode().IsRegular() {
			return nil, nil, rejectWPT("missing resource")
		}
		resources[target] = true
		if !strings.HasSuffix(target, ".css") {
			continue
		}
		css, err := readText(tp)
		if err != nil {
			return nil, nil, err
		}
		if why := wptForbids(css, rules); why != "" {
			return nil, nil, rejectWPT(why)
		}
		for _, cu := range cssURLs(css) {
			if strings.HasPrefix(cu, "/") || wptScheme.MatchString(cu) {
				return nil, nil, rejectWPT("absolute or remote URL")
			}
			r, err := filepath.Rel(filepath.FromSlash("/"+base), filepath.FromSlash("/"+path.Clean(path.Join(path.Dir(target), cu))))
			if err != nil {
				return nil, nil, rejectWPT("resource outside the test's directory")
			}
			pending = append(pending, filepath.ToSlash(r))
		}
	}
	out := make([]string, 0, len(resources))
	for r := range resources {
		out = append(out, r)
	}
	sort.Strings(out)
	return s, out, nil
}

// wptArea returns the area of a test: css/CSS2/<chapter> or css/<module>.
func wptArea(id string) string {
	parts := strings.Split(id, "/")
	if parts[1] == "CSS2" {
		return strings.Join(parts[:3], "/")
	}
	return strings.Join(parts[:2], "/")
}

// parseFuzzy returns the fuzziness the values declare for the reference ref,
// [maxDiffLo, maxDiffHi, pixelsLo, pixelsHi], nil when none does, and false
// for an unparsed value, which keeps the test out.
func parseFuzzy(values []string, ref string) ([]int, bool) {
	var chosen []int
	for _, v := range values {
		v = strings.TrimSpace(v)
		if m := wptFuzzyRef.FindStringSubmatch(v); m != nil {
			if path.Base(m[1]) != path.Base(ref) {
				continue
			}
			v = m[2]
		} else if chosen != nil {
			continue
		}
		var parts []string
		for p := range strings.SplitSeq(v, ";") {
			if p = strings.TrimSpace(p); p != "" {
				parts = append(parts, p)
			}
		}
		if len(parts) != 2 {
			return nil, false
		}
		nums := []int{}
		for _, p := range parts {
			if _, after, ok := strings.Cut(p, "="); ok {
				p = after
			}
			lo, hi, _ := strings.Cut(p, "-")
			lo = strings.TrimSpace(lo)
			if lo == "" {
				lo = "0"
			}
			if hi = strings.TrimSpace(hi); hi == "" {
				hi = lo
			}
			l, err1 := strconv.Atoi(lo)
			h, err2 := strconv.Atoi(hi)
			if err1 != nil || err2 != nil || strings.ContainsAny(lo+hi, "+-") {
				return nil, false
			}
			nums = append(nums, l, h)
		}
		chosen = nums
	}
	return chosen, true
}

// wptRef is one reference of a reftest.
type wptRef struct {
	Fuzzy     []int    `json:"fuzzy,omitempty"`
	Path      string   `json:"path"`
	Rel       string   `json:"rel"`
	Resources []string `json:"resources"`
}

// wptEntry is one reftest of reftests.json.
type wptEntry struct {
	ID        string   `json:"id"`
	Pages     string   `json:"pages,omitempty"`
	Print     bool     `json:"print"`
	Refs      []wptRef `json:"refs"`
	Resources []string `json:"resources"`
}

// selectWPT selects the WPT reftests the converter can score: a test and its
// references plain HTML and CSS, every resource inside the test's
// directory, the area's own rules met, the areas capped by the SHA-256 of
// the test's path.
func selectWPT(_ context.Context, checkout, _, dir string, _ map[string]string) error {
	var tests []wptEntry
	rejected := map[string]int{}
	for _, top := range wptDirs {
		err := walkWPT(checkout, top, func(rel string) error {
			e, why, err := wptCandidate(checkout, rel, wptAreas[wptArea(rel)])
			switch {
			case err != nil, why == wptNotReftest:
			case why == "":
				tests = append(tests, e)
			default:
				rejected[why]++
			}
			return err
		})
		if err != nil {
			return err
		}
	}
	byArea := map[string][]wptEntry{}
	for _, t := range tests {
		byArea[wptArea(t.ID)] = append(byArea[wptArea(t.ID)], t)
	}
	limits := maps.Clone(wptAreaCap)
	for _, p := range wptProbes {
		err := walkWPT(checkout, p.dir, func(rel string) error {
			e, why, err := wptCandidate(checkout, rel, p.rules)
			if err == nil && why == "" {
				byArea[p.name] = append(byArea[p.name], e)
				tests = append(tests, e)
			}
			return err
		})
		if err != nil {
			return err
		}
		limits[p.name] = p.cap
	}
	var kept []wptEntry
	for a, group := range byArea {
		limit, ok := limits[a]
		if !ok && strings.HasPrefix(a, "css/CSS2/") {
			limit = wptAreaCap["css/CSS2"]
		}
		key := func(t wptEntry) string { s := sha256.Sum256([]byte(t.ID)); return hex.EncodeToString(s[:]) }
		sort.Slice(group, func(i, j int) bool { return key(group[i]) < key(group[j]) })
		kept = append(kept, group[:min(limit, len(group))]...)
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].ID < kept[j].ID })
	fmt.Fprintf(os.Stderr, "wpt: %d candidates, %d kept after the per-area caps\n", len(tests), len(kept))
	for _, r := range wptTopReasons(rejected, 5) {
		fmt.Fprintf(os.Stderr, "wpt: %d test files rejected for %s\n", rejected[r], r)
	}
	paths := []string{"LICENSE.md", "fonts/Ahem.ttf"}
	for _, t := range kept {
		paths = append(paths, t.ID)
		paths = append(paths, t.Resources...)
		for _, r := range t.Refs {
			paths = append(paths, r.Path)
			paths = append(paths, r.Resources...)
		}
	}
	if err := writeJSON(filepath.Join(dir, "reftests.json"), kept); err != nil {
		return err
	}
	return writeHashList(checkout, dir, paths)
}

// wptTopReasons returns the n reasons that rejected the most reftests, the
// most first, ties in name order.
func wptTopReasons(rejected map[string]int, n int) []string {
	reasons := slices.Collect(maps.Keys(rejected))
	sort.Slice(reasons, func(i, j int) bool {
		a, b := reasons[i], reasons[j]
		return rejected[a] > rejected[b] || (rejected[a] == rejected[b] && a < b)
	})
	return reasons[:min(n, len(reasons))]
}

// walkWPT calls fn on each test file under top: its files in name order
// before its subdirectories, wptSkipDirs left out.
func walkWPT(checkout, top string, fn func(rel string) error) error {
	entries, err := os.ReadDir(filepath.Join(checkout, filepath.FromSlash(top)))
	if err != nil {
		return err
	}
	var dirs []string
	for _, e := range entries {
		name := e.Name()
		switch {
		case e.IsDir():
			if !slices.Contains(wptSkipDirs, name) {
				dirs = append(dirs, top+"/"+name)
			}
		case hasAnySuffix(name, []string{".html", ".htm", ".xht"}) && !strings.Contains(name, "-ref") &&
			!strings.Contains(name, ".sub.") && !strings.Contains(name, ".tentative."):
			if err := fn(top + "/" + name); err != nil {
				return err
			}
		}
	}
	for _, d := range dirs {
		if err := walkWPT(checkout, d, fn); err != nil {
			return err
		}
	}
	return nil
}

// wptCandidate checks one test and its references under rules. It returns
// the entry, or the reason the test is kept out.
func wptCandidate(root, rel string, rules wptAreaRules) (wptEntry, string, error) {
	s, resources, err := checkWPTFile(root, rel, rules)
	if err != nil {
		return wptEntry{}, wptRejectReason(err), ignoreRejected(err)
	}
	if rules.required != nil {
		text, err := readText(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return wptEntry{}, "", err
		}
		if !rules.required.MatchString(text) {
			return wptEntry{}, "lacks " + strings.TrimPrefix(rules.required.String(), "(?i)"), nil
		}
	}
	if len(s.match) == 0 && len(s.mismatch) == 0 {
		return wptEntry{}, wptNotReftest, nil
	}
	e := wptEntry{ID: rel, Print: wptIsPrint(rel, s), Resources: resources, Pages: s.pages}
	for _, kind := range []struct {
		rel   string
		hrefs []string
	}{{"match", s.match}, {"mismatch", s.mismatch}} {
		for _, href := range kind.hrefs {
			ref := path.Clean(path.Join(path.Dir(rel), href))
			if strings.HasPrefix(href, "/") {
				ref = strings.TrimLeft(href, "/")
			}
			rs, rres, err := checkWPTFile(root, ref, rules)
			if err != nil {
				return wptEntry{}, "reference: " + wptRejectReason(err), ignoreRejected(err)
			}
			if len(rs.match) > 0 || len(rs.mismatch) > 0 {
				return wptEntry{}, "chained reference", nil
			}
			fuzzy, ok := parseFuzzy(s.fuzzy, ref)
			if !ok {
				return wptEntry{}, "unparsed fuzziness", nil
			}
			e.Refs = append(e.Refs, wptRef{Rel: kind.rel, Path: ref, Resources: rres, Fuzzy: fuzzy})
		}
	}
	return e, "", nil
}

// wptNotReftest is the reason given for a file that links no reference.
const wptNotReftest = "not a reftest"

func ignoreRejected(err error) error {
	if errors.Is(err, errWPTRejected) {
		return nil
	}
	return err
}

func wptIsPrint(rel string, s *wptScan) bool {
	stem := strings.TrimSuffix(path.Base(rel), path.Ext(rel))
	return strings.HasSuffix(stem, "-print") || strings.Contains("/"+rel, "/print/") || s.printClass
}
