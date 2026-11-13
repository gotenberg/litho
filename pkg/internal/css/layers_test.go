package css

import (
	"context"
	"reflect"
	"testing"
)

// TestLayers_OrderDeclarations locks CSS Cascade 5, 6.4: a later layer
// wins over an earlier one whatever the specificity, the rules outside
// every layer win over all, a layer's sublayers lose to its own rules,
// and !important reverses the order of layers; the style attribute wins
// over every layer.
func TestLayers_OrderDeclarations(t *testing.T) {
	t.Parallel()
	c := compute(t, `<p id=a class=a>a</p><p id=b class=b>b</p><p id=c class=c>c</p><p id=d class=d style="color: rgb(0 0 1) !important">d</p><p id=e class=e>e</p>`, `
@layer one, two;
@layer two { p:not(.e) { color: rgb(0 0 2) } p.c { color: rgb(0 0 7) !important } p.d { color: rgb(0 0 9) !important } }
@layer one { #a.a { color: rgb(0 0 1) } p.c { color: rgb(0 0 6) !important } }
@layer one.inner { p.e { color: rgb(0 0 3) } }
@layer one { p.e { color: rgb(0 0 4) } }
p.b { color: rgb(0 0 5) }
p.b { }
@layer two { #b.b { color: rgb(0 0 8) } }
`)
	for id, want := range map[string]uint8{"a": 2, "b": 5, "c": 6, "d": 1, "e": 4} {
		if got := c.of(id).Color; !colorEq(got, rgb(0, 0, want)) {
			t.Errorf("#%s color = %+v, want rgb(0 0 %d)", id, got, want)
		}
	}
}

// TestLayers_AtRules locks CSS Cascade 5, 6.4.3 for the at-rules: a later
// layer's @page declarations and @font-feature-values win over an
// earlier one's, whatever their order in the sheet.
func TestLayers_AtRules(t *testing.T) {
	t.Parallel()
	sheet, err := Parse(context.Background(), []byte(`
@layer one, two, three;
@layer three { @page { margin: 10px } @font-feature-values f { @styleset { foo: 1; bar: 1 } } }
@layer one { @page :first { margin: 20px } @font-feature-values f { @styleset { foo: 2; bar: 2; baz: 2 } } }
@layer two { @font-feature-values f { @styleset { baz: 3 } } }
`), ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	r := NewResolver(a4, []Sheet{{Sheet: sheet, Origin: Author}})
	if got := r.pageOf(t, PageSelector{First: true}).Margin[Top]; !dimEq(got, pts(7.5)) {
		t.Errorf("first page margin = %+v, want the later layer's 10px", got)
	}
	a := Alternates{Styleset: []string{"foo", "bar", "baz"}}
	want := []FeatureSetting{{"ss01", 1}, {"ss01", 1}, {"ss03", 1}}
	if got := a.Resolve(r.FontFeatureValues("f")); !reflect.DeepEqual(got, want) {
		t.Errorf("styleset = %v, want %v", got, want)
	}
}
