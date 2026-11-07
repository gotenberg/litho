package ooxml

import (
	"context"
	"testing"
)

func FuzzOpen(f *testing.F) {
	full := buildZip(f, minimalPackage())
	f.Add(full)
	f.Add(full[:len(full)/2])
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := Open(data)
		if err != nil {
			return
		}
		main, err := p.MainPart()
		if err != nil {
			return
		}
		_, _ = p.ParseXML(context.Background(), main, []string{NSWPS})
		_ = p.CoreProperties(context.Background())
		_, _ = p.Relationships(main)
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
