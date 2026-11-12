package ooxml

import (
	"context"
	"math"
	"testing"
)

func FuzzOpen(f *testing.F) {
	full := buildZip(f, minimalPackage())
	f.Add(full)
	f.Add(full[:len(full)/2])
	f.Fuzz(func(t *testing.T, data []byte) {
		ctx := t.Context()
		p, err := Open(ctx, data)
		if err != nil {
			return
		}
		main, err := p.MainPart(ctx)
		if err != nil {
			return
		}
		_, _ = p.ParseXML(ctx, main, []string{NSWPS})
		_, _ = p.CoreProperties(ctx) // a fuzz target checks it does not panic
		_, _ = p.Relationships(ctx, main)
	})
}

func FuzzParse(f *testing.F) {
	f.Add([]byte(`<a xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006"><mc:AlternateContent><mc:Choice Requires="mc"><b/></mc:Choice></mc:AlternateContent></a>`))
	f.Add([]byte(`<a><b>`))
	f.Fuzz(func(t *testing.T, data []byte) {
		root, err := Parse(context.Background(), data, []string{NSMC})
		if err == nil && root == nil {
			t.Fatal("nil root without error")
		}
	})
}

func FuzzParseVMLStyle(f *testing.F) {
	f.Add("position:absolute;margin-left:12pt;width:100px;height:2in;fillcolor:#396")
	f.Add("width:1")
	f.Fuzz(func(t *testing.T, s string) {
		st := ParseVMLStyle(s)
		for name := range st {
			if v, ok := st.Length(name, 0.75); ok && (math.IsNaN(v) || math.IsInf(v, 0)) {
				t.Fatalf("length %v", v)
			}
			_, _ = VMLColor(st[name])
			if v, ok := VMLFraction(st[name]); ok && (v < 0 || v > 1) {
				t.Fatalf("fraction %v", v)
			}
			x, y := VMLPair(st[name], 0)
			if math.IsNaN(x) || math.IsNaN(y) {
				t.Fatal("NaN pair")
			}
		}
	})
}
