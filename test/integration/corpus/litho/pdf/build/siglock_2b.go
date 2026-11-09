package build

import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

func init() { register("siglock_2b", siglock2b) }

// siglock2b builds siglock_2b.pdf: unsigned signature fields carrying a /Lock or seed
// values, at PDF/A-2b.
//
// ISO 32000-1, 12.7.4.5 lets an unsigned signature field state what signing it
// does and asks. Its /Lock (Table 233) names the fields the signature turns
// read-only, and its seed values (/SV, Table 234) choose or require the
// subfilter, the reason and the digest. No corpus file holds an unsigned
// signature field with either, so the mutate-sign cell, which signs each
// unsigned signature field in a write of its own, reaches every path here:
//
// | Field         | Kind                   | Entry                                                     |
// | ------------- | ---------------------- | --------------------------------------------------------- |
// | name          | text                   | none                                                      |
// | group         | non-terminal           | none                                                      |
// | group.first   | text                   | none                                                      |
// | group.second  | text                   | none                                                      |
// | lockinclude   | signature, unsigned    | /Lock /Include [(name) (group)]                           |
// | lockexclude   | signature, unsigned    | /Lock /Exclude [(group.first)]                            |
// | lockall       | signature, unsigned    | /Lock /All                                                |
// | seedmet       | signature, unsigned    | /SV choosing ETSI.CAdES.detached, requiring a reason and  |
// |               |                        | a SHA-256 or SHA-512 digest                               |
// | seedunmet     | signature, unsigned    | /SV requiring adbe.x509.rsa_sha1, which litho cannot sign |
//
// seedmet lists adbe.x509.rsa_sha1 first, which litho does not write, so the
// subfilter Table 234 picks is the first one the signer supports. Every field is
// merged with its widget, printed, on the one page; the signature widgets are of
// zero size, the text widgets' appearances paint nothing, and the page shows
// nothing. The XMP packet claims PDF/A-2b.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func siglock2b() ([]byte, error) {
	pdf := pdfgen.New()

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"Parent", pdf.Root.Get("Pages"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d(),
		"Contents", pdf.MakeIndirect(pdf.Stream([]byte(""))),
	))
	pdf.Pages.Set("Kids", a(page))
	pdf.Pages.Set("Count", 1)

	// appearance is a 100 by 20 Form XObject appearance that paints nothing.
	appearance := func() obj {
		s := pdf.Stream([]byte("0 0 100 20 re n\n"))
		s.Set("Type", n("XObject"))
		s.Set("Subtype", n("Form"))
		s.Set("BBox", a(0, 0, 100, 20))
		return pdf.MakeIndirect(s)
	}

	// widget is a field merged with its widget: a text widget at y with an
	// appearance, or a signature widget of zero size when y is nil.
	widget := func(partial string, y *int, entries ...any) *pdfgen.Dict {
		dd := d("Type", n("Annot"), "Subtype", n("Widget"), "T", text(partial), "F", 4, "P", page)
		for i := 0; i < len(entries); i += 2 {
			dd.Set(entries[i].(string), entries[i+1])
		}
		if y == nil {
			dd.Set("Rect", a(0, 0, 0, 0))
		} else {
			dd.Set("Rect", a(72, *y, 172, *y+20))
			dd.Set("AP", d("N", appearance()))
		}
		return pdf.Indirect(dd)
	}
	at := func(y int) *int { return &y }

	name := widget("name", at(700), "FT", n("Tx"), "V", text("Ada"))
	group := pdf.Indirect(d("T", text("group"), "FT", n("Tx")))
	first := widget("first", at(650), "Parent", group, "V", text("one"))
	second := widget("second", at(600), "Parent", group, "V", text("two"))
	group.Set("Kids", a(first, second))

	// lock is a signature field lock dictionary (ISO 32000-1, Table 233).
	lock := func(action string, fields ...string) *pdfgen.Dict {
		dd := d("Type", n("SigFieldLock"), "Action", n("/"+action))
		if len(fields) > 0 {
			arr := a()
			for _, f := range fields {
				arr.Append(text(f))
			}
			dd.Set("Fields", arr)
		}
		return pdf.Indirect(dd)
	}

	// seed is a seed value dictionary (ISO 32000-1, Table 234) of /Ff ff.
	seed := func(ff int, entries ...any) *pdfgen.Dict {
		return pdf.Indirect(d(append([]any{"Type", n("SV"), "Ff", ff}, entries...)...))
	}

	// Table 234, /Ff: bit 2 SubFilter, bit 4 Reasons, bit 7 DigestMethod.
	const seedSubFilter, seedReasons, seedDigest = 1 << 1, 1 << 3, 1 << 6

	signatures := []any{
		widget("lockinclude", nil, "FT", n("Sig"), "Lock", lock("Include", "name", "group")),
		widget("lockexclude", nil, "FT", n("Sig"), "Lock", lock("Exclude", "group.first")),
		widget("lockall", nil, "FT", n("Sig"), "Lock", lock("All")),
		widget(
			"seedmet",
			nil,
			"FT", n("Sig"),
			"SV", seed(
				seedSubFilter|seedReasons|seedDigest,
				"SubFilter", a(
					n("/adbe.x509.rsa_sha1"), n("/ETSI.CAdES.detached"), n("/adbe.pkcs7.detached"),
				),
				"Reasons", a(text("I approve this document")),
				"DigestMethod", a(n("SHA256"), n("SHA512")),
			),
		),
		widget("seedunmet", nil, "FT", n("Sig"), "SV", seed(seedSubFilter, "SubFilter", a(n("/adbe.x509.rsa_sha1")))),
	}

	page.Set("Annots", a(append([]any{name, first, second}, signatures...)...))
	pdf.Root.Set("AcroForm", d("Fields", a(append([]any{name, group}, signatures...)...)))
	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))
	return save(pdf, "1.7")
}
