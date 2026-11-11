package ooxml

import (
	"context"
	"testing"
)

func TestDescribe(t *testing.T) {
	t.Parallel()
	const ns = `xmlns:wp="` + NSWP + `" xmlns:a="` + NSA + `" xmlns:adec="` + NSADec + `"`
	for _, tc := range []struct {
		name, xml  string
		alt        string
		decorative bool
	}{
		{"descr", `<wp:docPr ` + ns + ` id="1" name="Picture 1" descr=" A cat " title="Cat"/>`, "A cat", false},
		{"title", `<wp:docPr ` + ns + ` id="1" name="Picture 1" title="Cat"/>`, "Cat", false},
		{"none", `<wp:docPr ` + ns + ` id="1" name="Picture 1"/>`, "", false},
		{"decorative", `<wp:docPr ` + ns + ` id="1" name="Line"><a:extLst><a:ext uri="{C183D7F6-B498-43B3-948B-1728B52AA6E4}"><adec:decorative val="1"/></a:ext></a:extLst></wp:docPr>`, "", true},
		{"not decorative", `<wp:docPr ` + ns + ` id="1" name="Line"><a:extLst><a:ext uri="{C183D7F6-B498-43B3-948B-1728B52AA6E4}"><adec:decorative val="0"/></a:ext></a:extLst></wp:docPr>`, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e, err := Parse(context.Background(), []byte(tc.xml), nil)
			if err != nil {
				t.Fatal(err)
			}
			alt, dec := Describe(e)
			if alt != tc.alt || dec != tc.decorative {
				t.Errorf("Describe = %q, %v; want %q, %v", alt, dec, tc.alt, tc.decorative)
			}
		})
	}
	if alt, dec := Describe(nil); alt != "" || dec {
		t.Error("nil describes something")
	}
}
