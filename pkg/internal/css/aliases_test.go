package css

import (
	"bufio"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// heldBackAliases are the legacy name aliases of the standards that
// legacyAliases leaves out, because Chromium reads them as properties of
// their own (see legacyAliases).
var heldBackAliases = map[string]bool{
	"font-stretch":                  true,
	"-webkit-mask-box-image":        true,
	"-webkit-mask-box-image-outset": true,
	"-webkit-mask-box-image-repeat": true,
	"-webkit-mask-box-image-slice":  true,
	"-webkit-mask-box-image-source": true,
	"-webkit-mask-box-image-width":  true,
}

// readWebrefAliases reads testdata/webref-legacy-aliases.txt: the legacy
// name aliases the CSS specifications define, as @webref/css lists them.
func readWebrefAliases(t *testing.T) map[string]string {
	t.Helper()
	f, err := os.Open("testdata/webref-legacy-aliases.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		alias, target, ok := strings.Cut(line, " ")
		if !ok {
			t.Fatalf("malformed line %q", line)
		}
		out[alias] = target
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestLegacyAliases_AreTheStandards locks the alias table to the legacy
// name aliases of the Compatibility Standard, 3.4.1, and of the CSS
// modules, read from @webref/css: every alias the data lists is in the
// table, naming the same property, or held back for a reason
// legacyAliases states. A property implemented later takes its alias with
// it, since an alias resolves to whatever its property is.
func TestLegacyAliases_AreTheStandards(t *testing.T) {
	t.Parallel()
	data := readWebrefAliases(t)
	for alias, target := range data {
		if heldBackAliases[alias] {
			if _, ok := legacyAliases[alias]; ok {
				t.Errorf("%s is held back and in the table", alias)
			}
			continue
		}
		if got, ok := legacyAliases[alias]; !ok || got != target {
			t.Errorf("legacyAliases[%q] = %q, %v; the standard names %q", alias, got, ok, target)
		}
	}
	for alias := range legacyAliases {
		if _, ok := data[alias]; !ok {
			t.Errorf("%s is in the table and no standard lists it", alias)
		}
	}
	for alias := range heldBackAliases {
		if _, ok := data[alias]; !ok {
			t.Errorf("%s is held back and no standard lists it", alias)
		}
	}
	for alias, target := range legacyAliases {
		if knownProperty(alias) {
			t.Errorf("%s resolves by itself; it must resolve through %s", alias, target)
		}
		if knownProperty(target) != knownProperty(propertyName(alias)) {
			t.Errorf("%s and %s are not known alike", alias, target)
		}
	}
}

// TestLegacyAliases_ParseAsTheirProperty checks that an alias declares
// what its property declares (CSS Cascade 5, 3.1) and that the later of
// the two names wins, as Chromium cascades them.
func TestLegacyAliases_ParseAsTheirProperty(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ alias, target, value string }{
		{"-webkit-transform", "transform", "translate(3px, 4px) rotate(5deg)"},
		{"-webkit-transform-origin", "transform-origin", "10% 20px"},
		{"-webkit-perspective", "perspective", "10px"},
		{"-webkit-perspective-origin", "perspective-origin", "10% 20px"},
		{"-webkit-transform-style", "transform-style", "preserve-3d"},
		{"-webkit-backface-visibility", "backface-visibility", "hidden"},
		{"-webkit-box-shadow", "box-shadow", "2px 3px 4px red, inset 1px 1px blue"},
		{"-webkit-box-sizing", "box-sizing", "border-box"},
		{"-webkit-border-radius", "border-radius", "1px 2px 3px 4px / 5px"},
		{"-webkit-border-top-left-radius", "border-top-left-radius", "5px 10px"},
		{"-webkit-background-size", "background-size", "cover"},
		{"-webkit-background-origin", "background-origin", "content-box"},
		{"-webkit-background-clip", "background-clip", "content-box"},
		{"-webkit-flex", "flex", "2"},
		{"-webkit-order", "order", "3"},
		{"grid-gap", "gap", "3px 4px"},
		{"word-wrap", "overflow-wrap", "anywhere"},
	} {
		got := styleOf(t, tc.alias+": "+tc.value)
		want := styleOf(t, tc.target+": "+tc.value)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %s computes otherwise than %s", tc.alias, tc.value, tc.target)
		}
	}
	if s := styleOf(t, "transform: none; -webkit-transform: rotate(5deg)"); len(s.Transform) != 1 {
		t.Errorf("a later -webkit-transform: transform = %v", s.Transform)
	}
	if s := styleOf(t, "-webkit-transform: rotate(5deg); transform: none"); s.Transform != nil {
		t.Errorf("a later transform: transform = %v", s.Transform)
	}
}

// TestLegacyAliases_SilentAsTheirProperty checks that an alias of a
// silent property is silent, and that an unknown vendor-prefixed name
// stays silent.
func TestLegacyAliases_SilentAsTheirProperty(t *testing.T) {
	t.Parallel()
	c := compute(t, `<body><div id=t></div></body>`,
		`#t { -webkit-transition: opacity 1s; -webkit-unknown-thing: 1 }`)
	ws := c.c.Warnings()
	if hasWarning(ws, warn.CSSUnsupported, "transition") || hasWarning(ws, warn.CSSUnsupported, "unknown-thing") {
		t.Errorf("unexpected warning: %v", ws)
	}
}

// TestChromiumAliases_ResolveToKnownProperties checks the prefixed names
// Chromium reads as standard properties: each resolves through the table
// to a property litho parses, none is also a legacy name alias, and none
// resolves by itself.
func TestChromiumAliases_ResolveToKnownProperties(t *testing.T) {
	t.Parallel()
	for alias, target := range chromiumAliases {
		if _, ok := legacyAliases[alias]; ok {
			t.Errorf("%s is in both alias tables", alias)
		}
		if knownProperty(alias) {
			t.Errorf("%s resolves by itself; it must resolve through %s", alias, target)
		}
		if !knownProperty(target) && !silentProperties[target] {
			t.Errorf("%s names %s, which litho does not parse", alias, target)
		}
	}
}

// TestChromiumAliases_ParseAsTheirProperty checks that a prefixed name
// Chromium reads as a standard property declares what that property
// declares, and that the later of the two names wins.
func TestChromiumAliases_ParseAsTheirProperty(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ alias, target, value string }{
		{"-webkit-column-count", "column-count", "2"},
		{"-webkit-columns", "columns", "3 10em"},
		{"-webkit-column-rule", "column-rule", "3px solid red"},
		{"-webkit-margin-before", "margin-block-start", "20px"},
		{"-webkit-padding-start", "padding-inline-start", "4em"},
		{"-webkit-border-end", "border-inline-end", "5px dashed green"},
		{"-webkit-border-before-width", "border-block-start-width", "4px"},
		{"-webkit-logical-width", "inline-size", "100px"},
		{"-webkit-max-logical-height", "max-block-size", "30px"},
		{"-webkit-text-emphasis", "text-emphasis", "filled dot red"},
		{"-webkit-text-emphasis-position", "text-emphasis-position", "under left"},
		{"-webkit-shape-outside", "shape-outside", "circle(50%)"},
		{"-webkit-clip-path", "clip-path", "circle(20px at 30px 20px)"},
		{"-webkit-opacity", "opacity", "0.3"},
		{"-webkit-font-feature-settings", "font-feature-settings", `"smcp"`},
		{"-webkit-hyphenate-character", "hyphenate-character", `"="`},
		{"-webkit-writing-mode", "writing-mode", "vertical-rl"},
		{"-webkit-text-orientation", "text-orientation", "upright"},
	} {
		got := styleOf(t, tc.alias+": "+tc.value)
		want := styleOf(t, tc.target+": "+tc.value)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %s computes otherwise than %s", tc.alias, tc.value, tc.target)
		}
		if reflect.DeepEqual(got, styleOf(t, "")) {
			t.Errorf("%s: %s declares nothing", tc.alias, tc.value)
		}
	}
	if s := styleOf(t, "column-count: 3; -webkit-column-count: 2"); s.Columns.Count != 2 {
		t.Errorf("a later -webkit-column-count: column-count = %v", s.Columns.Count)
	}
	if s := styleOf(t, "-webkit-column-count: 2; column-count: 3"); s.Columns.Count != 3 {
		t.Errorf("a later column-count: column-count = %v", s.Columns.Count)
	}
}
