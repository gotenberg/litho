package typography

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
)

// FuzzMappedEncodingCodes pins that [Font.MappedEncodingCodes] never panics on
// a shown string, consumes every byte of it, and gives no code a CID its
// encoding CMap does not map.
func FuzzMappedEncodingCodes(f *testing.F) {
	program := []byte("/CIDInit /ProcSet findresource begin 12 dict begin begincmap\n" +
		"2 begincodespacerange <00> <7F> <8000> <FFFF> endcodespacerange\n" +
		"1 begincidchar <20> 1 endcidchar\n" +
		"1 begincidrange <8000> <80FF> 100 endcidrange\n" +
		"endcmap CMapName currentdict /CMap defineresource pop end end")
	cids := cmap.ParseEmbeddedCIDMap(context.Background(), program)
	if cids == nil {
		f.Fatal("the seed CMap did not parse")
	}
	font := &Font{Subtype: "Type0", encodingCIDs: &encodingCIDs{cids: cids}}

	f.Add([]byte{0x20, 0x80, 0x10})
	f.Add([]byte{0x80})
	f.Fuzz(func(t *testing.T, data []byte) {
		codes, ok := font.MappedEncodingCodes(data)
		if !ok {
			t.Fatal("a font with an encoding CMap read no codes")
		}
		total := 0
		for _, c := range codes {
			if len(c.Code) == 0 {
				t.Fatal("a code of no bytes")
			}
			total += len(c.Code)
			if c.Mapped && (c.CID < 0 || c.CID > 0xFFFF) {
				t.Fatalf("code %X mapped to CID %d", c.Code, c.CID)
			}
		}
		if total != len(data) {
			t.Fatalf("the codes hold %d bytes of %d", total, len(data))
		}
	})
}
