package css

import (
	"reflect"
	"testing"
)

func TestPseudo_ComputesFromRules(t *testing.T) {
	t.Parallel()
	c := compute(t, `<div id=d style="color: rgb(0, 0, 255)">x</div><p id=p>y</p><ol><li id=li>z</ol><q id=q>w</q>`,
		`div::before { content: "a" attr(title) counter(c, upper-roman) counters(c, ".") open-quote; font-size: 20px }
		 div:after { content: none }
		 li::marker { content: "*"; color: rgb(255, 0, 0) }
		 :root { --v: "var" } p::after { content: var(--v) }`)
	d := byID(c.doc, "d")
	before := c.c.Pseudo(d, PseudoBefore)
	if before == nil {
		t.Fatal("no ::before style")
	}
	want := []ContentItem{
		{Kind: ItemString, Text: "a"},
		{Kind: ItemAttr, Text: "title"},
		{Kind: ItemCounter, Text: "c", Style: UpperRoman},
		{Kind: ItemCounters, Text: "c", Separator: ".", Style: Decimal},
		{Kind: ItemOpenQuote},
	}
	if before.Content.Kind != ContentList || !reflect.DeepEqual(before.Content.Items, want) {
		t.Errorf("::before content %+v", before.Content)
	}
	if !colorEq(before.Color, rgb(0, 0, 255)) || !near(before.FontSize, 15) {
		t.Errorf("::before inherits colour %v and sets font size %v", before.Color, before.FontSize)
	}
	if after := c.c.Pseudo(d, PseudoAfter); after == nil || after.Content.Kind != ContentNone {
		t.Errorf("::after %+v, want content none", after)
	}
	if m := c.c.Pseudo(byID(c.doc, "li"), PseudoMarker); m == nil || !colorEq(m.Color, rgb(255, 0, 0)) || len(m.Content.Items) != 1 {
		t.Errorf("::marker %+v", m)
	}
	if a := c.c.Pseudo(byID(c.doc, "p"), PseudoAfter); a == nil || len(a.Content.Items) != 1 || a.Content.Items[0].Text != "var" {
		t.Errorf("::after with var() %+v", a)
	}
	if b := c.c.Pseudo(byID(c.doc, "q"), PseudoBefore); b == nil || b.Content.Items[0].Kind != ItemOpenQuote {
		t.Errorf("q::before from the UA sheet %+v", b)
	}
	if c.c.Pseudo(byID(c.doc, "p"), PseudoBefore) != nil || c.of("d").Content.Kind != ContentNormal {
		t.Errorf("a pseudo-element without a rule, or the element itself, has content")
	}
}

func TestContent_Parses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value string
		want  Content
		ok    bool
	}{
		{`normal`, Content{}, true},
		{`none`, Content{Kind: ContentNone}, true},
		{`"x" / "alt"`, Content{Kind: ContentList, Items: []ContentItem{{Kind: ItemString, Text: "x"}}, Alt: "alt"}, true},
		{`url(a.png)`, Content{Kind: ContentList, Items: []ContentItem{{Kind: ItemImage, Text: "a.png"}}}, true},
		{`attr(data-x, "fb")`, Content{Kind: ContentList, Items: []ContentItem{{Kind: ItemAttr, Text: "data-x", Fallback: "fb"}}}, true},
		{`no-close-quote close-quote`, Content{Kind: ContentList, Items: []ContentItem{{Kind: ItemNoCloseQuote}, {Kind: ItemCloseQuote}}}, true},
		{`counter(x, foo)`, Content{Kind: ContentList, Items: []ContentItem{{Kind: ItemCounter, Text: "x", Style: Decimal}}}, true},
		{`leader(".")`, Content{}, false},
		{`counter(none)`, Content{}, false},
		{`counters(x)`, Content{}, false},
		{`bogus`, Content{}, false},
		{`"a" none`, Content{}, false},
	}
	for _, tt := range tests {
		ds := ParseDeclarations("content: "+tt.value, ParseOptions{})
		if len(ds) != 1 {
			t.Fatalf("%s: %d declarations", tt.value, len(ds))
		}
		if !tt.ok {
			if len(ds[0].entries) != 0 {
				t.Errorf("%s: parsed as %+v, want invalid", tt.value, ds[0].entries)
			}
			continue
		}
		if len(ds[0].entries) != 1 || !reflect.DeepEqual(ds[0].entries[0].v, tt.want) {
			t.Errorf("%s: %+v, want %+v", tt.value, ds[0].entries, tt.want)
		}
	}
}

func TestCounters_Parse(t *testing.T) {
	t.Parallel()
	c := compute(t, `<div id=a><p id=b>x</p></div><ol id=ol></ol>`,
		`#a { counter-reset: x 3 y reversed(z); counter-increment: x -2 y; counter-set: x; quotes: "<" ">" "[" "]" }
		 #b { counter-reset: none; counter-increment: 7 }`)
	a := c.of("a")
	if want := []CounterOp{{Name: "x", Value: 3}, {Name: "y"}, {Name: "z", Reversed: true, Implicit: true}}; !reflect.DeepEqual(a.CounterReset, want) {
		t.Errorf("counter-reset %+v", a.CounterReset)
	}
	if want := []CounterOp{{Name: "x", Value: -2}, {Name: "y", Value: 1}}; !reflect.DeepEqual(a.CounterIncrement, want) {
		t.Errorf("counter-increment %+v", a.CounterIncrement)
	}
	if want := []CounterOp{{Name: "x"}}; !reflect.DeepEqual(a.CounterSet, want) {
		t.Errorf("counter-set %+v", a.CounterSet)
	}
	b := c.of("b")
	if b.CounterReset != nil || b.CounterIncrement != nil || b.CounterSet != nil {
		t.Errorf("counters inherited or invalid value kept: %+v %+v", b.CounterReset, b.CounterIncrement)
	}
	if want := [][2]string{{"<", ">"}, {"[", "]"}}; !reflect.DeepEqual(b.Quotes.Pairs, want) {
		t.Errorf("quotes not inherited: %+v", b.Quotes)
	}
	if ol := c.of("ol"); len(ol.CounterReset) != 1 || ol.CounterReset[0].Name != "list-item" {
		t.Errorf("ol resets %+v, want list-item from the UA sheet", ol.CounterReset)
	}
}

func TestListStyleType_String(t *testing.T) {
	t.Parallel()
	c := compute(t, `<ul><li id=a>x<li id=b>y</ul>`, `#a { list-style-type: "-> " } #b { list-style: "" inside }`)
	if a := c.of("a"); a.ListStyleType != ListString || a.ListStyleString != "-> " {
		t.Errorf("list-style-type string: %v %q", a.ListStyleType, a.ListStyleString)
	}
	if b := c.of("b"); b.ListStyleType != ListString || b.ListStyleString != "" || b.ListStylePosition != Inside {
		t.Errorf("list-style string: %v %q", b.ListStyleType, b.ListStyleString)
	}
}

func TestDisplay_ListItemKeywords(t *testing.T) {
	t.Parallel()
	c := compute(t, `<span id=a>a</span><span id=b>b</span><span id=c>c</span><span id=d>d</span><span id=e>e</span>`,
		`#a { display: inline list-item } #b { display: list-item block flow } #c { display: inline list-item; float: left }
		 #d { display: list-item table } #e { display: list-item inline block }`)
	for id, want := range map[string]Display{"a": InlineListItem, "b": ListItem, "c": ListItem, "d": Inline, "e": Inline} {
		if got := c.of(id).Display; got != want {
			t.Errorf("#%s display %v, want %v", id, got, want)
		}
	}
}
