package build

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("annothidden_2b", annothidden2b) }

// annothidden2b builds annothidden_2b.pdf: annotations that show neither on screen nor in print.
//
// ISO 19005-2, 6.3.2 requires every annotation's Print flag set and its Hidden,
// Invisible, NoView and ToggleNoView flags clear. ISO 32000-1, Table 165 keeps a
// Hidden annotation off the screen and off paper, and a NoView one off the
// screen, printing it only with Print set. PDF/A remediation removes an
// annotation that shows nowhere rather than make it visible, with what goes with
// it, and repairs the flags of every other one. The constructs no other corpus
// file carries sit here, each with its own verdict:
//
// Page 1, whose /Annots is an indirect array:
//
//   - "note", a Text annotation, /F 6 (Hidden, Print), with a pop-up that has no
//     /F (6.3.2 test 1 exempts a Popup) and a visible reply, a Text annotation
//     whose /IRT names "note" (ISO 32000-1, Table 170). The three go together.
//   - "toggle", a Square, /F 288 (NoView, ToggleNoView): it shows on screen when
//     the pointer rolls over it, so it stays with its flags repaired.
//   - "shown", a Square, /F 4: conforming, the control.
//   - "note2", a Text annotation, /F 4, whose pop-up is /F 2 (Hidden): the pop-up
//     goes and "note2" loses its /Popup.
//
// Page 2, whose /Annots is a direct array:
//
//   - a Square written inline in /Annots, /F 2 (Hidden).
//   - field "pair" with two widgets as /Kids, the first /F 2 (Hidden), the second
//     /F 4: the field keeps its second widget.
//   - field "lone" whose only widget is /F 32 (NoView without Print): the field
//     goes with it, and /CO loses it.
//   - field "group", a non-terminal field whose only child "item" is merged with
//     its widget, /F 6: "item" goes, and "group" with it.
//   - field "sig", a signed signature, /F 2 (Hidden): the signature is reached
//     through the field, so the widget stays with its flags repaired. Its
//     /Contents is the openssl CMS blob of sigcms_2b.pdf
//     (pkg/internal/compliance/testdata/cms_real.der), and its /ByteRange is
//     patched after the save to cover the file, so ISO 19005-2, 6.4.3 is silent.
//
// Every annotation but the pop-ups carries an empty /AP /N form, so 6.3.3 is
// silent, and the pages draw nothing. The XMP packet claims PDF/A-2b.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func annothidden2b() ([]byte, error) {
	cmsReal := "pkg/internal/compliance/testdata/cms_real.der"
	// A fixed-width placeholder the post-save patch overwrites in place.
	const placeholder = 9999999999

	pdf := pdfgen.New()

	// appearance is an empty /AP /N form: 6.3.3 asks for one, and it paints
	// nothing.
	appearance := func() *pdfgen.Dict {
		s := pdf.Stream([]byte{})
		s.Set("Type", n("XObject"))
		s.Set("Subtype", n("Form"))
		s.Set("BBox", a(0, 0, 20, 20))
		s.Set("Resources", d())
		return d("N", pdf.MakeIndirect(s))
	}

	// annot is an annotation at height y with an empty appearance, /F flags
	// or none (flags < 0).
	annot := func(subtype pdfgen.Name, flags, y int, extra ...any) *pdfgen.Dict {
		dd := d(
			"Type", n("Annot"),
			"Subtype", subtype,
			"Rect", a(50, y, 70, y+20),
			"AP", appearance(),
		)
		for i := 0; i < len(extra); i += 2 {
			dd.Set(extra[i].(string), extra[i+1])
		}
		if flags >= 0 {
			dd.Set("F", flags)
		}
		return dd
	}

	// page is a page drawing nothing.
	page := func(annots *pdfgen.Array) *pdfgen.Dict {
		p := pdf.Indirect(d(
			"Type", n("Page"),
			"MediaBox", a(0, 0, 612, 792),
			"Resources", d(),
			"Contents", pdf.MakeIndirect(pdf.Stream([]byte{})),
			"Annots", annots,
		))
		pdf.AddPage(p)
		return p
	}

	// ISO 32000-1, Table 165: bit 2 Hidden, bit 3 Print, bit 6 NoView, bit 9
	// ToggleNoView.
	const hidden, print, noView, toggle = 2, 4, 32, 256

	note := pdf.Indirect(annot(n("Text"), hidden|print, 700, "Contents", text("note")))
	popup := pdf.Indirect(
		d("Type", n("Annot"), "Subtype", n("Popup"), "Rect", a(80, 680, 200, 740), "Parent", note),
	)
	note.Set("Popup", popup)
	reply := pdf.Indirect(annot(n("Text"), print, 660, "Contents", text("reply"), "IRT", note))
	toggled := pdf.Indirect(annot(n("Square"), noView|toggle, 620))
	shown := pdf.Indirect(annot(n("Square"), print, 580))
	note2 := pdf.Indirect(annot(n("Text"), print, 540, "Contents", text("note2")))
	popup2 := pdf.Indirect(
		d(
			"Type", n("Annot"),
			"Subtype", n("Popup"),
			"Rect", a(80, 520, 200, 580),
			"F", hidden,
			"Parent", note2,
		),
	)
	note2.Set("Popup", popup2)
	page(pdf.MakeIndirect(a(note, popup, reply, toggled, shown, note2, popup2)).(*pdfgen.Array))

	inlineSquare := annot(n("Square"), hidden, 700)

	pair := pdf.Indirect(d("FT", n("Tx"), "T", text("pair")))
	pairHidden := pdf.Indirect(annot(n("Widget"), hidden, 660, "Parent", pair))
	pairShown := pdf.Indirect(annot(n("Widget"), print, 620, "Parent", pair))
	pair.Set("Kids", a(pairHidden, pairShown))

	lone := pdf.Indirect(d("FT", n("Tx"), "T", text("lone")))
	loneWidget := pdf.Indirect(annot(n("Widget"), noView, 580, "Parent", lone))
	lone.Set("Kids", a(loneWidget))

	group := pdf.Indirect(d("T", text("group")))
	item := pdf.Indirect(
		annot(n("Widget"), hidden|print, 540, "FT", n("Tx"), "T", text("item"), "V", text("x"), "Parent", group),
	)
	group.Set("Kids", a(item))

	der, err := repoFile(cmsReal)
	if err != nil {
		return nil, err
	}
	signature := pdf.Indirect(
		d(
			"Type", n("Sig"),
			"Filter", n("/Adobe.PPKLite"),
			"SubFilter", n("/adbe.pkcs7.detached"),
			"Name", text("sig"),
			"ByteRange", a(0, placeholder, placeholder, placeholder),
			"Contents", pdfgen.String(der),
		),
	)
	sig := pdf.Indirect(annot(n("Widget"), hidden, 500, "FT", n("Sig"), "T", text("sig"), "V", signature))

	page(a(inlineSquare, pairHidden, pairShown, loneWidget, item, sig))

	pdf.Root.Set("AcroForm", d(
		"Fields", a(pair, lone, group, sig),
		"CO", a(lone, pair),
		"SigFlags", 3,
		"DA", text("/Helv 0 Tf 0 g"),
	))
	attachXMP(pdf, xmpPacket(xmpOpts{Part: 2, Conformance: "B"}))

	data, err := save(pdf, "1.7")
	if err != nil {
		return nil, err
	}

	// Patch the /ByteRange against where the /Contents landed, as sigcms_2b.go
	// does: a signature cannot know its offset before the file is laid out.
	total := len(data)
	patched := 0
	brRE := regexp.MustCompile(fmt.Sprintf(`/ByteRange \[ 0 %d %d %d \]`, placeholder, placeholder, placeholder))
	contentsRE := regexp.MustCompile(`/Contents <[0-9a-fA-F]*>`)
	for _, obj := range regexp.MustCompile(`(?s)\d+ 0 obj(.*?)endobj`).FindAllSubmatchIndex(data, -1) {
		body := data[obj[2]:obj[3]]
		if !strings.Contains(string(body), "/Type /Sig") {
			continue
		}
		base := obj[2]
		br := brRE.FindIndex(body)
		contents := contentsRE.FindIndex(body)
		if br == nil || contents == nil {
			return nil, fmt.Errorf("the signature object is not in the expected shape")
		}
		start := base + contents[0] + len("/Contents ")
		end := base + contents[1]
		replacement := fmt.Sprintf("/ByteRange [ 0 %010d %010d %010d ]", start, end, total-end)
		if len(replacement) != br[1]-br[0] {
			return nil, fmt.Errorf("the patched /ByteRange changes width, which would move an xref offset")
		}
		copy(data[base+br[0]:base+br[1]], replacement)
		patched++
	}
	if patched != 1 {
		return nil, fmt.Errorf("patched %d signatures, expected 1", patched)
	}
	return data, nil
}
