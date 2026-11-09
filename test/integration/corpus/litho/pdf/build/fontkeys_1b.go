package build

import (
	"fmt"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("fontkeys_1b", fontkeys1b) }

// fontkeys1b builds fontkeys_1b.pdf: font dictionary required keys under PDF/A-1b.
//
// `fontdict_2b.pdf` covers the part-2 spelling of this requirement, ISO 19005-2,
// 6.2.11.2, which has four tests. Part 1 states it as ISO 19005-1, 6.3.2 and
// splits it into seven, so two of them are reachable from no other fixture: the
// `/Type` key of the font dictionary itself and the subtype of an embedded font
// file.
//
// Five sibling fonts carry one defect each and are all drawn on the page, which
// is the masking probe: a check that reports the first offending font and stops
// reports one rule where five are due.
//
// Tests 2 and 3 are deliberately absent, and neither can be reached from any
// fixture. veraPDF builds a font object only for the five subtypes test 2
// permits, so a font naming any other reaches no test at all. And a font with no
// `/BaseFont`, which is what test 3 judges, makes veraPDF throw at PDF/A-1b: the
// identical file validates at PDF/A-2b and reports the part-2 spelling of the
// same requirement. litho reports both. See ../FIXTURES.md.
//
// The clause splits by object, which decides which font can carry which defect.
// Tests 1 and 7 are scoped to `PDFont` and apply to every font; tests 4, 5
// and 6 are scoped to `PDSimpleFont` and exempt a standard font, so the fonts
// carrying them name a face that is not one of the standard 14. That exemption is
// itself planted as a control: a Helvetica with no `/FirstChar`, `/LastChar` or
// `/Widths` conforms, and a check that ignores `isStandard` fires on it.
//
// A stream carrying an `/F` key sits beside the fonts. ISO 19005-1, 6.1.7
// forbids it, it belongs to no font clause, and it gives the file a defect from a
// different part of the specification entirely.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func fontkeys1b() ([]byte, error) {
	pdf := pdfgen.New()

	// descriptor is a font descriptor, optionally carrying an embedded font file.
	descriptor := func(name string, embedded *pdfgen.Stream) *pdfgen.Dict {
		dd := d(
			"Type", n("FontDescriptor"),
			"FontName", n(name),
			"Flags", 32,
			"ItalicAngle", 0,
			"Ascent", 718,
			"Descent", -207,
			"CapHeight", 718,
			"StemV", 88,
			"FontBBox", a(-166, -225, 1000, 931),
		)
		if embedded != nil {
			dd.Set("FontFile3", embedded)
		}
		return pdf.Indirect(dd)
	}

	type fontOpts struct {
		noType              bool
		firstChar, lastChar any
		widths              []any
		noWidths            bool
		embedded            *pdfgen.Stream
	}

	// font is one simple font dictionary, conforming unless an argument says otherwise.
	font := func(name string, o fontOpts) *pdfgen.Dict {
		dd := d()
		if !o.noType {
			dd.Set("Type", n("Font"))
		}
		dd.Set("Subtype", n("Type1"))
		dd.Set("BaseFont", n(name))
		if o.firstChar != nil {
			dd.Set("FirstChar", o.firstChar)
		}
		if o.lastChar != nil {
			dd.Set("LastChar", o.lastChar)
		}
		if !o.noWidths {
			w := o.widths
			if w == nil {
				w = []any{600, 600}
			}
			dd.Set("Widths", a(w...))
		}
		dd.Set("FontDescriptor", descriptor(name, o.embedded))
		return pdf.Indirect(dd)
	}

	// A font file whose /Subtype is neither Type1C nor CIDFontType0C. The test reads
	// the key rather than the bytes, so the stream carries a placeholder. The
	// subtype is /Type1 rather than /OpenType because veraPDF parses an OpenType
	// font file eagerly and throws on a placeholder, which aborts the whole run.
	badFontfile := pdf.Stream([]byte("\x01\x00\x04\x01placeholder"))
	badFontfile.Set("Subtype", n("Type1"))
	pdf.MakeIndirect(badFontfile)

	keys := []string{"F1", "F4", "F5", "F6", "F7", "F8", "F9"}
	fonts := map[string]*pdfgen.Dict{
		// F1 omits /Type, which the clause requires on every font dictionary.
		"F1": font("LithoNoType", fontOpts{noType: true, firstChar: 65, lastChar: 66}),
		// F4, F5 and F6 name a face outside the standard 14, so the exemption that
		// protects a standard font does not apply to them.
		"F4": font("LithoNoFirst", fontOpts{lastChar: 66}),
		"F5": font("LithoNoLast", fontOpts{firstChar: 65}),
		// Two codes are declared and three widths supplied.
		"F6": font("LithoBadWidths", fontOpts{firstChar: 65, lastChar: 66, widths: []any{600, 600, 600}}),
		// F7 embeds a font file whose subtype the clause does not permit.
		"F7": font("LithoBadFile", fontOpts{firstChar: 65, lastChar: 66, embedded: badFontfile}),
		// F8 is a standard font with none of /FirstChar, /LastChar or /Widths,
		// which tests 4, 5 and 6 exempt. A firing here is over-suppression of the
		// isStandard escape.
		"F8": font("Helvetica", fontOpts{noWidths: true}),
		// F9 conforms in every respect.
		"F9": font("LithoConforming", fontOpts{firstChar: 65, lastChar: 66}),
	}

	var content []byte
	fontRes := d()
	for i, key := range keys {
		content = fmt.Appendf(content, "BT /%s 12 Tf 50 %d Td (AB) Tj ET\n", key, 720-i*20)
		fontRes.Set(key, fonts[key])
	}

	// A stream dictionary carrying /F, which ISO 19005-1, 6.1.7 forbids outright.
	// It is an XObject rather than a font resource, so it belongs to no font clause.
	stray := pdf.Stream([]byte("0 0 0 rg 0 0 10 10 re f\n"),
		"Type", n("XObject"), "Subtype", n("Form"), "BBox", a(0, 0, 10, 10), "F", text("external.bin"))

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(
			"Font", fontRes,
			"XObject", d("Fm0", pdf.MakeIndirect(stray)),
		),
		"Contents", pdf.Stream(append(content, "q 1 0 0 1 400 700 cm /Fm0 Do Q\n"...)),
	))
	pdf.AddPage(page)

	// Deliberately no /Metadata and no /OutputIntents.
	return save(pdf, "1.4")
}
