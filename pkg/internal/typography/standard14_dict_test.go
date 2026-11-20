package typography

import (
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// writtenStandard14 writes the standard font name and returns its font
// dictionary and font descriptor as the allocator holds them.
func writtenStandard14(t *testing.T, name string) (font, descriptor ast.PdfDict) {
	t.Helper()
	alloc := writer.NewObjAllocator(0)
	ref, err := WriteStandard14Font(alloc, name)
	if err != nil {
		t.Fatal(err)
	}
	font, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfDict)
	if !ok {
		t.Fatalf("%s: object %d is no dictionary", name, ref.ObjectNum)
	}
	fdRef, ok := font["FontDescriptor"].(ast.PdfIndirectRef)
	if !ok {
		t.Fatalf("%s: /FontDescriptor %v is no indirect reference (ISO 32000-1, Table 111)", name, font["FontDescriptor"])
	}
	descriptor, ok = alloc.Pending()[fdRef.ObjectNum].(ast.PdfDict)
	if !ok {
		t.Fatalf("%s: descriptor %d is no dictionary", name, fdRef.ObjectNum)
	}
	return font, descriptor
}

// TestWriteStandard14Font_StatesWhatPDF20Requires pins that every standard
// font litho writes carries /FirstChar, /LastChar, /Widths of one entry a
// code and a /FontDescriptor with every entry ISO 32000-1, Table 122 requires
// of a font that is not Type 3, which ISO 32000-2, 9.6.2.2 requires of the
// standard 14 fonts too.
func TestWriteStandard14Font_StatesWhatPDF20Requires(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"Courier", "Courier-Bold", "Courier-BoldOblique", "Courier-Oblique",
		"Helvetica", "Helvetica-Bold", "Helvetica-BoldOblique", "Helvetica-Oblique",
		"Symbol", "Times-Bold", "Times-BoldItalic", "Times-Italic", "Times-Roman", "ZapfDingbats",
	} {
		font, fd := writtenStandard14(t, name)
		first, ok1 := font["FirstChar"].(ast.PdfInt)
		last, ok2 := font["LastChar"].(ast.PdfInt)
		widths, ok3 := font["Widths"].(ast.PdfArray)
		if !ok1 || !ok2 || !ok3 || int(last-first+1) != len(widths) {
			t.Errorf("%s: FirstChar %v, LastChar %v and %d widths do not agree", name, font["FirstChar"], font["LastChar"], len(widths))
		}
		if font["BaseFont"] != ast.PdfName(name) || font["Subtype"] != ast.PdfName("Type1") {
			t.Errorf("%s: BaseFont %v, Subtype %v", name, font["BaseFont"], font["Subtype"])
		}
		for _, key := range []ast.PdfName{"FontName", "Flags", "FontBBox", "ItalicAngle", "Ascent", "Descent", "CapHeight", "StemV"} {
			if _, ok := fd[key]; !ok {
				t.Errorf("%s: descriptor lacks /%s", name, key)
			}
		}
		for _, key := range []ast.PdfName{"FontFile", "FontFile2", "FontFile3"} {
			if _, ok := fd[key]; ok {
				t.Errorf("%s: descriptor holds /%s for a font written without its program", name, key)
			}
		}
		symbolic := name == "Symbol" || name == "ZapfDingbats"
		if _, ok := font["Encoding"]; ok == symbolic {
			t.Errorf("%s: /Encoding %v", name, font["Encoding"])
		}
	}
}

// TestWriteStandard14Font_WidthsFollowTheEncoding pins the widths of codes
// WinAnsiEncoding and the built-in encodings select (ISO 32000-1, Annex D):
// a letter, the Euro at 0x80, no-break space and soft hyphen drawn as space
// and hyphen, and the bullet an unused code above octal 40 selects (Annex D.2,
// note 4).
func TestWriteStandard14Font_WidthsFollowTheEncoding(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		font        string
		first, last int
		code        int
		want        int
	}{
		{"Helvetica", 32, 255, 'A', 667},
		{"Helvetica", 32, 255, 0x80, 556},
		{"Helvetica", 32, 255, 0x7F, 350},
		{"Helvetica", 32, 255, 0xA0, 278},
		{"Helvetica", 32, 255, 0xAD, 333},
		{"Times-Italic", 32, 255, 'm', 722},
		{"Courier-Bold", 32, 255, 0x81, 600},
		{"Symbol", 32, 254, 'A', 722},
		{"Symbol", 32, 254, 0x7F, 0},
		{"ZapfDingbats", 32, 254, 0xE3, 924},
	} {
		font, _ := writtenStandard14(t, tc.font)
		if font["FirstChar"] != ast.PdfInt(tc.first) || font["LastChar"] != ast.PdfInt(tc.last) {
			t.Errorf("%s: FirstChar %v, LastChar %v, want %d, %d", tc.font, font["FirstChar"], font["LastChar"], tc.first, tc.last)
			continue
		}
		widths, _ := font["Widths"].(ast.PdfArray)
		if got := widths[tc.code-tc.first]; got != ast.PdfInt(tc.want) {
			t.Errorf("%s: code %#x is %v wide, want %d", tc.font, tc.code, got, tc.want)
		}
	}
}

// TestWriteStandard14Font_DescriptorFromTheAFM pins descriptor entries read
// from the AFM files, the flags of ISO 32000-1, Table 123 among them, and the
// ascent and descent of a font whose file states none.
func TestWriteStandard14Font_DescriptorFromTheAFM(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		font  string
		key   ast.PdfName
		want  ast.PdfObject
		label string
	}{
		{"Helvetica", "Flags", ast.PdfInt(32), "Nonsymbolic"},
		{"Times-Italic", "Flags", ast.PdfInt(2 | 32 | 64), "Serif, Nonsymbolic, Italic"},
		{"Courier-BoldOblique", "Flags", ast.PdfInt(1 | 2 | 32 | 64), "FixedPitch, Serif, Nonsymbolic, Italic"},
		{"Symbol", "Flags", ast.PdfInt(4), "Symbolic"},
		{"Times-Italic", "ItalicAngle", ast.PdfReal(-15.5), "ItalicAngle"},
		{"Helvetica-Bold", "StemV", ast.PdfInt(140), "StdVW"},
		{"Times-Roman", "CapHeight", ast.PdfInt(662), "CapHeight"},
		{"ZapfDingbats", "Ascent", ast.PdfInt(820), "FontBBox top"},
		{"ZapfDingbats", "Descent", ast.PdfInt(-143), "FontBBox bottom"},
		{"Symbol", "CapHeight", ast.PdfInt(1010), "the ascent"},
	} {
		_, fd := writtenStandard14(t, tc.font)
		if got := fd[tc.key]; got != tc.want {
			t.Errorf("%s: /%s = %v, want %v (%s)", tc.font, tc.key, got, tc.want, tc.label)
		}
	}
}

func TestWriteStandard14Font_RefusesAnotherName(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"Arial", "Helvetica,Bold", "ABCDEF+Helvetica", ""} {
		if _, err := WriteStandard14Font(writer.NewObjAllocator(0), name); !errors.Is(err, ErrNotStandard14) {
			t.Errorf("%q: err = %v, want %v", name, err, ErrNotStandard14)
		}
	}
}
