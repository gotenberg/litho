package build

import (
	"fmt"
	"regexp"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("filestruct_1b", filestruct1b) }

// filestruct1b builds filestruct_1b.pdf: file-level syntax and form rules under PDF/A-1b.
//
// The coverage scan showed ISO 19005-1 6.1.2, 6.1.3, 6.1.6 and 6.9 unreached
// between them. All four judge the file rather than any page: the header comment,
// the trailer identifier, the lexical form of hexadecimal strings, and the
// interactive form dictionary.
//
// Six defects sit in one file, on four independent axes, which is the masking
// probe: a check that reports the first syntax problem and stops reports one rule
// where four are due, and a check that condemns every hexadecimal string once it
// sees a malformed one fires on the conforming control beside them.
//
// | Construct                                          | Rule       |
// | -------------------------------------------------- | ---------- |
// | header comment bytes below 128                     | `6.1.2/2`  |
// | no `/ID` in the trailer                            | `6.1.3/1`  |
// | hexadecimal string with an odd digit count         | `6.1.6/1`  |
// | hexadecimal string containing a non-hex character  | `6.1.6/2`  |
// | `/NeedAppearances true` on the AcroForm            | `6.9/1`    |
// | a form field with no appearance dictionary         | `6.9/2`    |
//
// The two 6.1.6 defects are deliberately kept apart. veraPDF tests them
// separately, `hexCount % 2 == 0` against `containsOnlyHex == true`, so a single
// string breaking both would report one rule and leave the other unproven: the
// odd-count string uses only hex digits, and the non-hex string has an even count.
//
// Three of the six were beyond pikepdf, which first wrote the committed file: it
// emits a conforming header comment, always writes an `/ID`, and normalises every
// string it serialises. pdfgen reproduces its output, and the three are patched into the saved bytes instead, each replacement the same length as
// what it replaces, so every cross-reference offset stays valid. See
// `limits_2b.go` and `xref_1b.go` for the same technique.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func filestruct1b() ([]byte, error) {
	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	// appearance is a minimal appearance stream, the value a widget's /N takes.
	appearance := func() *pdfgen.Stream {
		ap := pdf.Stream([]byte("0 0 0 rg 0 0 60 20 re f\n"),
			"Type", n("XObject"), "Subtype", n("Form"), "BBox", a(0, 0, 60, 20))
		pdf.MakeIndirect(ap)
		return ap
	}

	// Placeholders patched into hexadecimal strings after saving. Each is exactly
	// the length its replacement needs, counting the enclosing delimiters, so the
	// patch never moves a byte. The save writes the string ABCDEF as "(ABCDEF)",
	// eight bytes, which is room for "<AB CDE>" or "<ABCDZZ>".
	const hexOddPlaceholder = "ABCDEF"
	const hexNonhexPlaceholder = "GHIJKL"
	// A well-formed hexadecimal string: even digit count, hex digits only. It is the
	// control, and it is written as a placeholder too so all three take the same
	// route into the file.
	const hexValidPlaceholder = "MNOPQR"

	widgetNoAP := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Widget"),
		"Rect", a(50, 600, 110, 620),
		"F", 4,
		"FT", n("Tx"),
		"T", text("noap"),
		// 6.9 t2: no /AP at all.
	))

	// The control for 6.9 t2: an otherwise identical widget that does carry one.
	widgetWithAP := pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Widget"),
		"Rect", a(50, 560, 110, 580),
		"F", 4,
		"FT", n("Tx"),
		"T", text("hasap"),
		"AP", d("N", appearance()),
	))

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.Stream([]byte("BT /F1 12 Tf 50 700 Td (file structure) Tj ET\n")),
		"Annots", a(widgetNoAP, widgetWithAP),
	))
	pdf.AddPage(page)

	// 6.9 t1: NeedAppearances present and true.
	pdf.Root.Set("AcroForm", d(
		"Fields", a(widgetNoAP, widgetWithAP),
		"NeedAppearances", true,
	))

	// The hexadecimal strings ride in the document information dictionary, which is
	// lexed like any other object. Their values carry no meaning for these tests.
	pdf.Trailer.Set("Info", pdf.Indirect(d(
		"Producer", text(hexOddPlaceholder),
		"Creator", text(hexNonhexPlaceholder),
		"Title", text(hexValidPlaceholder),
	)))

	// Deliberately no /Metadata and no /OutputIntents, both independent of the
	// clauses above.
	data, err := save(pdf, "1.4")
	if err != nil {
		return nil, err
	}

	// 6.1.2 t2: the four bytes after the second % must each exceed 127. The save
	// writes 0xBF 0xF7 0xA2 0xFE; four ASCII letters occupy the same four bytes
	// and every one of them is below 128.
	header := regexp.MustCompile(`(?s)%PDF-\d\.\d\n%(.{4})\n`).FindSubmatch(data)
	if header == nil {
		return nil, fmt.Errorf("header comment not found")
	}
	if data, err = patchOnce(data, []byte("%"+string(header[1])+"\n"), []byte("%LITH\n"), "6.1.2 t2"); err != nil {
		return nil, err
	}

	// 6.1.3 t1: the trailer must carry no /ID. Blanking the entry keeps the trailer
	// the same length, and startxref points at the cross-reference table that
	// precedes it, so nothing moves.
	trailerID := regexp.MustCompile(`/ID \[<[0-9a-fA-F]+><[0-9a-fA-F]+>\]`).Find(data)
	if trailerID == nil {
		return nil, fmt.Errorf("trailer /ID not found")
	}
	blank := make([]byte, len(trailerID))
	for i := range blank {
		blank[i] = ' '
	}
	if data, err = patchOnce(data, trailerID, blank, "6.1.3 t1"); err != nil {
		return nil, err
	}

	// 6.1.6 t1: an odd number of non-white-space characters. The space inside is
	// white space, so this string carries five hexadecimal digits.
	if data, err = patchOnce(data, []byte("("+hexOddPlaceholder+")"), []byte("<AB CDE>"), "6.1.6 t1"); err != nil {
		return nil, err
	}
	// 6.1.6 t2: an even count, but Z is not a hexadecimal digit.
	if data, err = patchOnce(data, []byte("("+hexNonhexPlaceholder+")"), []byte("<ABCDZZ>"), "6.1.6 t2"); err != nil {
		return nil, err
	}
	// The control: even count, hexadecimal digits only.
	return patchOnce(data, []byte("("+hexValidPlaceholder+")"), []byte("<ABCDEF>"), "6.1.6 control")
}
