package css

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// TestSelector_HasDirAndFormStates checks :has() with its four
// combinators (Selectors 4, 4.5), :dir() (7.1), and the form-state
// pseudo-classes as the attributes of a static document decide them (HTML
// Standard, 4.16.3).
func TestSelector_HasDirAndFormStates(t *testing.T) {
	t.Parallel()
	doc := parseHTML(t, `<body>
<div id=a><p id=a1><em>x</em></p><span id=a2></span></div>
<div id=b><span id=b1></span><p id=b2></p></div>
<section id=rt dir=rtl><p id=r1>x</p><p id=r2 dir=auto>abc</p><p id=r3 dir=auto>שלום</p><bdi id=r4>עב</bdi></section>
<form id=f>
<input id=req required><input id=opt value=v>
<input id=ro readonly><textarea id=ta></textarea><div id=ce contenteditable><span id=ce1>x</span></div>
<input id=ph placeholder=p><input id=phv placeholder=p value=v>
<input id=c1 type=checkbox checked><select><option id=o1>a<option id=o2 selected>b</select>
<input id=r5 type=radio name=g><input id=r6 type=radio name=g><input id=r7 type=radio name=h checked>
<progress id=pr></progress><progress id=pv value=1></progress>
<input id=em type=email value=nope><input id=em2 type=email value=a@b.c>
<input id=nu type=number min=1 max=5 value=9><input id=nu2 type=number min=1 max=5 value=3><input id=nu3 type=number min=1 value=1.5>
<button id=s1>go</button><button id=s2>again</button>
</form>
<form id=g><input id=gq required value=x></form>
<details id=d1 open><summary>s</summary></details><details id=d2></details>
</body>`)
	tests := []struct{ sel, want string }{
		{"div:has(p)", "a b"},
		{"div:has(> em)", ""},
		{"div:has(em)", "a"},
		{"p:has(+ span)", "a1"},
		{"span:has(~ p)", "b1"},
		{"div:has(> span + p)", "b"},
		{"div:has(span, em)", "a b ce"},
		{"p:dir(rtl)", "r1 r3"},
		{"p:dir(ltr)", "a1 b2 r2"},
		{"bdi:dir(rtl)", "r4"},
		{"input:required", "req gq"},
		{"input:optional", "opt ro ph phv c1 r5 r6 r7 em em2 nu nu2 nu3"},
		{":read-write", "req opt ta ce ce1 ph phv em em2 nu nu2 nu3 gq"},
		{"input:read-only", "ro c1 r5 r6 r7"},
		{":placeholder-shown", "ph"},
		{":default", "c1 o2 r7 s1"},
		{":indeterminate", "r5 r6 pr"},
		{"input:invalid", "req em nu nu3"},
		{"form:invalid", "f"},
		{"form:valid", "g"},
		{":in-range", "nu2 nu3"},
		{":out-of-range", "nu"},
		{"details:open", "d1"},
	}
	for _, tt := range tests {
		got := strings.Join(matching(t, doc, tt.sel), " ")
		if got != tt.want {
			t.Errorf("%s matches %q, want %q", tt.sel, got, tt.want)
		}
	}
}

// TestSelector_LangAsChromium checks that :lang() matches a language and
// its subtags alone, as Chromium matches it.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 matches
//     :lang(de) on de-CH, de-Latn-CH, de and DE-ch, :lang(de-CH) on de-CH
//     and DE-ch, and :lang(\*-CH), :lang(de-\*-CH) and :lang(\*) on
//     none.
func TestSelector_LangAsChromium(t *testing.T) {
	t.Parallel()
	doc := parseHTML(t, `<p id=a lang=de-CH></p><p id=b lang=de-Latn-CH></p><p id=c lang=de></p><p id=d lang=fr-CH></p><p id=e lang=DE-ch></p><p id=f></p>`)
	for _, tt := range []struct{ sel, want string }{
		{"p:lang(de)", "a b c e"}, {"p:lang(de-CH)", "a e"}, {`p:lang(\*-CH)`, ""}, {`p:lang(\*)`, ""},
	} {
		if got := strings.Join(matching(t, doc, tt.sel), " "); got != tt.want {
			t.Errorf("%s matches %q, want %q", tt.sel, got, tt.want)
		}
	}
}

// TestSelector_HasCost checks that :has() on every element of a deep tree
// finishes within the cascade's step budget, each subtree searched once
// per anchor.
func TestSelector_HasCost(t *testing.T) {
	t.Parallel()
	doc := parseHTML(t, strings.Repeat("<div>", 300)+"<p id=x>x</p>"+strings.Repeat("</div>", 300))
	m := newMatcher("", maxComputeSteps, false, nil)
	sel := mustSelectors(t, "div:has(p)")
	n := 0
	var walk func(*html.Node)
	walk = func(e *html.Node) {
		for c := e.FirstChild; c != nil; c = c.NextSibling {
			if isElement(c) && m.anyMatches(sel, c) {
				n++
			}
			walk(c)
		}
	}
	walk(doc)
	if m.over || n != 300 {
		t.Errorf("matched %d of 300 divs, over budget %v, %d steps", n, m.over, m.steps)
	}
}
