package build

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("limits_2b", limits2b) }

// limits2b builds limits_2b.pdf: implementation limits from ISO 19005-2, 6.1.13.
//
// The coverage scan showed 6.1.13 as the second largest unreached cluster. Its
// tests are scattered across object types rather than gathered in one
// dictionary, which is what makes them worth planting together: each defect
// lives in a different part of the file, so a check that stops at the first
// limit breach reports one rule where four are due.
//
// Test 2 needs a real number beyond +/-3.403e38, which no writer emits directly:
// pikepdf renders such a value as a plain integer, and veraPDF then judges it
// under the integer limit t1 rather than the real one. The value is therefore
// planted as bytes after the save, replacing the last two digits of the emitted
// integer with ".0". That keeps the token the same length, so every cross
// reference offset stays valid, and turns it into a real that is still far above
// the limit.
//
// Not every test in the clause is reachable. `t7` needs more than 8388607
// indirect objects and `t10` needs a CMap with an out-of-range CID, neither of
// which belongs in a small fixture. `t5` needs a real closer to zero than
// 1.175e-38, and pikepdf normalises such a denormal to plain `0`, which the test
// explicitly permits; the same byte patch cannot help, because a denormal written
// long enough to patch is a different value. Those stay uncovered deliberately.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func limits2b() ([]byte, error) {
	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"),
		"Encoding", n("WinAnsiEncoding"),
	))

	// t4: a name longer than 127 bytes, used as a resource key so it is reachable
	// from the page rather than orphaned.
	longName := "L" + strings.Repeat("o", 200) + "ng"

	// t9: DeviceN admits at most 32 colorants; this one names 33.
	colorants := a()
	for i := range 33 {
		colorants.Append(n(fmt.Sprintf("/Ink%02d", i)))
	}
	domain := a()
	for range 33 {
		domain.Append(0, 1)
	}
	tint := pdf.Stream([]byte("{ "+strings.Repeat("pop ", 32)+"dup dup }"),
		"FunctionType", 4, "Domain", domain, "Range", a(0, 1, 0, 1, 0, 1))
	pdf.MakeIndirect(tint)
	devicen := a(n("DeviceN"), colorants, n("DeviceRGB"), tint)

	// t8: the graphics state stack may nest 28 deep; this content nests 30.
	nesting := strings.Repeat("q\n", 30) + "0 0 0 rg 50 700 40 20 re f\n" + strings.Repeat("Q\n", 30)

	content := nesting +
		"q /" + longName + " cs 0.5 sc 50 620 40 20 re f Q\n" +
		"BT /F1 12 Tf 50 580 Td (implementation limits) Tj ET\n"

	page := d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"Font", d("F1", font),
			"ColorSpace", d(longName, devicen),
		),
		"Contents", pdf.Stream([]byte(content)),
	)
	pdf.AddPage(pdf.Indirect(page))

	// t3: a string of 40000 bytes, well past the 32767 limit. It sits on an
	// annotation so it is reachable and is not confused with document metadata.
	pdf.Page(0).Set("Annots", a(pdf.Indirect(d(
		"Type", n("Annot"),
		"Subtype", n("Square"),
		"Rect", a(400, 700, 500, 750),
		"F", 4,
		"AP", d("N", pdf.Stream([]byte(""))),
		"Contents", text(strings.Repeat("s", 40000)),
	))))

	// The real that breaches 6.1.13 t2. pikepdf emits it as a 46 digit integer; the
	// patch below turns those digits into a real of the same byte length.
	// (Python passes Decimal("1E45"), which pikepdf converts through a double.)
	pdf.Root.Set("/LithoLargeReal", pdfgen.Float(1e45))

	// Deliberately no /Metadata and no /OutputIntents.
	raw, err := save(pdf, "1.7")
	if err != nil {
		return nil, err
	}

	m := regexp.MustCompile(`/LithoLargeReal (\d+)`).FindSubmatchIndex(raw)
	if m == nil {
		return nil, fmt.Errorf("limits_2b: /LithoLargeReal was not emitted as an integer")
	}
	digits := raw[m[2]:m[3]]
	real := append(bytes.Clone(digits[:len(digits)-2]), ".0"...)
	return append(append(bytes.Clone(raw[:m[2]]), real...), raw[m[3]:]...), nil
}
