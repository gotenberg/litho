package build

import (
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("namereads_1b", namereads1b) }

// namereads1b builds namereads_1b.pdf: the names veraPDF reads again at every operator.
//
// ISO 19005-1, 6.1.12 test 4 bounds every name to 127 bytes. veraPDF judges a
// name once as a COS object, and again each time a typed object of its model
// wraps it in a new COS object: the colorant name of a Separation or a DeviceN at
// every link an operator makes to the colour space, the /RI and /BM of a graphics
// state at every gs that builds it, and the /Intent of an image once per image.
// A colour space or a graphics state written directly has no identity of its
// own, so it is read again at every link; one written as an indirect object is
// read once for the whole document. No file of any corpus carries a name past the
// limit in one of these places, so no count reached the re-reads.
//
// Every long name below is distinct, so each row counts on its own:
//
// | Name  | Where it sits                                  | Reached by            | 6.1.12/4 |
// | ----- | ---------------------------------------------- | --------------------- | -------- |
// | `A…`  | direct Separation, page `/ColorSpace`          | `cs`, `scn`, `f`      | 5        |
// | `B…`  | indirect Separation, page `/ColorSpace`        | `cs`, `scn`, `f`      | 3        |
// | `C…`  | colorant of a direct DeviceN                   | `cs`, `scn`, `f`      | 5        |
// | `D…`  | Separation of a direct DeviceN's `/Colorants`  | `cs`, `scn`, `f`      | 4        |
// | `E…`  | base of a direct `/Indexed` space              | `cs`, `f`             | 3        |
// | `F…`  | direct Separation of an image painted twice    | the first `Do`        | 2        |
// | `G…`  | `/Intent` of an image painted twice            | the first `Do`        | 2        |
// | `H…`  | `/Intent` of that image's `/SMask`             | the first `Do`        | 2        |
// | `I…`  | direct Separation of a form painted twice      | `cs`, `scn`, `f` once | 5        |
// | `J…`  | `/BM` of a direct graphics state               | two `gs`              | 3        |
// | `K…`  | `/RI` of an indirect graphics state            | two `gs`              | 2        |
// | `L…`  | `/RI` of a direct graphics state               | one `gs`              | 2        |
// | `M…`  | `/Intent` of an inline image                   | its `BI`              | 2        |
// | `N…`  | direct Separation a glyph selects past `d1`    | nothing               | 2        |
//
// The column counts the object sweep's one failure, the resource dictionary's
// own reading of a Separation or DeviceN colorant (GFPDResources), and the
// re-reads, 42 in all. `N…` is the control: a Type 3 glyph past its `d1` ignores
// its colour operators (ISO 32000-1, 9.6.5), so veraPDF makes no link to the
// Separation it selects.
//
// Every construct also answers the clause it breaks on its own, a DeviceCMYK
// alternate, an invalid rendering intent, a blend mode other than Normal, a soft
// mask, and none of them is gated by what this file probes, so a checker that
// stops at the first verdict reports one rule where several are due.
//
// The graphics state whose `/RI` is written directly is set by one `gs` alone:
// ISO 19005-1, 6.2.9 t1 judges the same `/RI`, and litho judges it once per
// graphics state its resources list, where veraPDF judges it at every `gs`.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func namereads1b() ([]byte, error) {
	pdf := pdfgen.New()

	// longName is a 130-byte name, past the 127 bytes ISO 19005-1, 6.1.12 t4
	// allows.
	longName := func(letter string) pdfgen.Name {
		return n(strings.Repeat(letter, 130))
	}

	// tint is a type 2 tint transform onto DeviceCMYK.
	tint := func(components int) *pdfgen.Dict {
		domain := a()
		for range components {
			domain.Append(0, 1)
		}
		return d(
			"FunctionType", 2,
			"Domain", domain,
			"C0", a(0, 0, 0, 0),
			"C1", a(1, 0, 0, 0),
			"N", 1,
		)
	}

	// separation is a Separation over DeviceCMYK naming the long colorant
	// letter.
	separation := func(letter string) *pdfgen.Array {
		return a(n("Separation"), longName(letter), n("DeviceCMYK"), tint(1))
	}

	// image is a one-pixel image XObject.
	image := func(colourSpace obj, keys ...any) *pdfgen.Stream {
		img := pdf.Stream([]byte("\x00"))
		img.Set("Type", n("XObject"))
		img.Set("Subtype", n("Image"))
		img.Set("Width", 1)
		img.Set("Height", 1)
		img.Set("ColorSpace", colourSpace)
		img.Set("BitsPerComponent", 8)
		for i := 0; i < len(keys); i += 2 {
			img.Set(keys[i].(string), keys[i+1])
		}
		pdf.MakeIndirect(img)
		return img
	}

	const fill = "0 0 10 10 re f\n"

	form := pdf.Stream([]byte("/SepI cs 1 scn " + fill))
	form.Set("Type", n("XObject"))
	form.Set("Subtype", n("Form"))
	form.Set("BBox", a(0, 0, 10, 10))
	form.Set("Resources", d("ColorSpace", d("SepI", separation("I"))))

	glyph := pdf.Stream([]byte("10 0 0 0 10 10 d1 /SepN cs 1 scn " + fill))
	type3 := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("Type3"),
		"FontBBox", a(0, 0, 10, 10),
		"FontMatrix", a(0.1, 0, 0, 0.1, 0, 0),
		"CharProcs", d("a", pdf.MakeIndirect(glyph)),
		"Encoding", d("Type", n("Encoding"), "Differences", a(97, n("/a"))),
		"FirstChar", 97,
		"LastChar", 97,
		"Widths", a(10),
		"Resources", d(),
	))

	colorants := a(n("Cyan"), n("/Spot"))
	resources := d(
		"ColorSpace", d(
			"SepA", separation("A"),
			"SepB", pdf.MakeIndirect(separation("B")),
			"DevC", a(n("DeviceN"), a(n("Cyan"), longName("C")), n("DeviceCMYK"), tint(2)),
			"DevD", a(
				n("DeviceN"),
				colorants,
				n("DeviceCMYK"),
				tint(2),
				d("Colorants", d("Spot", separation("D"))),
			),
			"IdxE", a(n("Indexed"), separation("E"), 1, str("\x00\xff")),
			"SepN", separation("N"),
		),
		"XObject", d(
			"ImF", image(separation("F")),
			"ImG", image(n("DeviceGray"), "Intent", longName("G"), "SMask", image(n("DeviceGray"), "Intent", longName("H"))),
			"Fm", pdf.MakeIndirect(form),
		),
		"ExtGState", d(
			"GJ", d("BM", longName("J")),
			"GK", pdf.Indirect(d("RI", longName("K"))),
			"GL", d("RI", longName("L")),
		),
		"Font", d("T3", type3),
	)

	content := "q /SepA cs 1 scn " + fill + "Q\n" +
		"q /SepB cs 1 scn " + fill + "Q\n" +
		"q /DevC cs 1 1 scn " + fill + "Q\n" +
		"q /DevD cs 1 1 scn " + fill + "Q\n" +
		"q /IdxE cs 0 sc " + fill + "Q\n" +
		"q /ImF Do /ImF Do Q\n" +
		"q /ImG Do /ImG Do Q\n" +
		"q /Fm Do /Fm Do Q\n" +
		"q /GJ gs /GJ gs /GK gs /GK gs /GL gs Q\n" +
		"q BI /W 1 /H 1 /CS /G /BPC 8 /Intent /" + strings.Repeat("M", 130) + " ID \x00 EI Q\n" +
		"q BT /T3 10 Tf (a) Tj ET Q\n"

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 200, 200),
		"Resources", resources,
		"Contents", pdf.Stream([]byte(content)),
	))
	pdf.AddPage(page)

	// Deliberately no /Metadata and no output intent.
	return save(pdf, "1.4")
}
