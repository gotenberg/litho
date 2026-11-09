package build

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("docmdp_2b", docmdp2b) }

// docmdp2b builds docmdp_2b.pdf: a certifying signature beside a denormal real.
//
// A coverage scan showed two clusters no fixture reached, neither of them blocked
// by anything but authorship:
//
// | Construct                                                            | A-2b spelling |
// | -------------------------------------------------------------------- | ------------- |
// | a `/Reference` entry carrying `/DigestMethod` under `/Perms /DocMDP` | `6.1.12/2`    |
// | a real number too small to be represented                            | `6.1.13/5`    |
//
// The two are independent, which is the masking probe: one lives in the signature
// dictionary reached from the catalog `/Perms`, the other in the page content
// stream, so a checker that abandons a document after its first structural
// verdict reports one rule where two are due.
//
// `6.1.12/2` reads `permsContainDocMDP == false || entries.split('&').filter(elem
// => elem == 'DigestLocation' || elem == 'DigestMethod' || elem == 'DigestValue')
// .length == 0`, so it only applies once the catalog carries `/Perms /DocMDP`.
// Two `/Reference` entries sit under that DocMDP signature: the first names a
// `/DigestMethod`, the second is the control and carries only `/TransformMethod`
// and `/TransformParams`. A check that condemns the array rather than the entry
// fires on both.
//
// The signature itself is conformant, which is the point: its `/Contents` is the
// same genuine `openssl cms -sign` blob `sigcms_2b.pdf` uses, one signer with its
// certificate, and its `/ByteRange` is patched after the save to cover the file
// exactly. All three tests of ISO 19005-2, 6.4.3 therefore stay silent and the
// `6.1.12` verdict is cleanly attributable to the `/Reference` entry.
//
// `6.1.13/5` reads `realValue == 0.0 || (realValue <= -1.175e-38) || (realValue >=
// 1.175e-38)`, the smallest normal single-precision magnitude. The page paints a
// rectangle whose width is `0.000000000000000000000000000000000000000001`, a
// denormal, beside one of `1` which is the control. Writing it as a literal
// rather than in exponent form matters: ISO 32000-1, 7.3.3 gives PDF reals no
// exponent syntax at all.
//
// A third cluster, `6.2.11.2/2` (a font `/Subtype` naming no font type), was
// dropped after measurement: veraPDF builds no font object for an unrecognised
// subtype, so it reaches neither that rule nor the embedding rules behind it, and
// carrying the construct here would bake a permanent rule-set divergence into the
// corpus.
//
// The blob is embedded rather than generated: openssl stamps a signingTime, so
// shelling out would cost the fixture its byte-for-byte rebuild.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func docmdp2b() ([]byte, error) {
	// A ten-digit placeholder leaves the patched value the same width as the written
	// one, so no xref offset moves.
	const placeholder = 1000000000

	// openssl cms -sign -binary -in data -signer c1.pem -inkey k1.pem \
	//     -outform DER -nodetach -md sha256
	const cmsRealB64 = "" +
		"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"

	// blob decodes the embedded CMS SignedData blob.
	blob := func() ([]byte, error) {
		return base64.StdEncoding.DecodeString(strings.Join(strings.Fields(cmsRealB64), ""))
	}

	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"), "Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"), "Encoding", n("WinAnsiEncoding")))

	// The denormal beside its control. PDF reals have no exponent syntax
	// (ISO 32000-1, 7.3.3), so the literal carries every digit.
	const denormal = "0.000000000000000000000000000000000000000001"
	content := "BT /F1 12 Tf 50 700 Td (certifying signature) Tj ET\n" +
		"0 0 0 rg 50 600 " + denormal + " 20 re f\n" +
		"50 560 1 20 re f\n"

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.Stream([]byte(content))))
	pdf.AddPage(page)

	// The offending /Reference entry beside its control. Only the first carries one
	// of the three forbidden digest keys.
	transformParams := d("Type", n("TransformParams"), "P", 2, "V", text("1.2"))
	badRef := d(
		"Type", n("SigRef"),
		"TransformMethod", n("DocMDP"),
		"TransformParams", transformParams,
		"DigestMethod", n("/SHA256"))
	goodRef := d(
		"Type", n("SigRef"),
		"TransformMethod", n("DocMDP"),
		"TransformParams", transformParams)

	cms, err := blob()
	if err != nil {
		return nil, err
	}
	sig := pdf.Indirect(d(
		"Type", n("Sig"),
		"Filter", n("/Adobe.PPKLite"),
		"SubFilter", n("/adbe.pkcs7.detached"),
		"Name", text("docmdp"),
		"Reference", a(badRef, goodRef),
		"ByteRange", a(0, placeholder, placeholder, placeholder),
		"Contents", pdfgen.String(cms)))

	widget := pdf.Indirect(d(
		"Type", n("Annot"), "Subtype", n("Widget"), "Rect", a(50, 500, 200, 520),
		"F", 132, "T", text("docmdp"), "FT", n("Sig"), "V", sig,
		"AP", d("N", pdf.MakeIndirect(pdf.Stream([]byte{})))))
	page.Set("Annots", a(widget))
	pdf.Root.Set("AcroForm", d("Fields", a(widget), "SigFlags", 3))

	// The clause only bites once the catalog certifies the document.
	pdf.Root.Set("Perms", d("DocMDP", sig))

	// Deliberately no /Metadata.
	data, err := save(pdf, "1.7")
	if err != nil {
		return nil, err
	}

	// Patch /ByteRange against where /Contents landed: a signature cannot know its
	// own offset before the file is laid out.
	total := len(data)
	patched := 0
	brRe := regexp.MustCompile(fmt.Sprintf(`/ByteRange \[ 0 %d %d %d \]`, placeholder, placeholder, placeholder))
	contentsRe := regexp.MustCompile(`/Contents <[0-9a-fA-F]*>`)
	for _, obj := range regexp.MustCompile(`(?s)\d+ 0 obj(.*?)endobj`).FindAllSubmatchIndex(data, -1) {
		body := data[obj[2]:obj[3]]
		if !bytes.Contains(body, []byte("/Type /Sig")) {
			continue
		}
		base := obj[2]
		br := brRe.FindIndex(body)
		contents := contentsRe.FindIndex(body)
		if br == nil || contents == nil {
			return nil, fmt.Errorf("signature object is not in the expected shape")
		}
		start := base + contents[0] + len("/Contents ")
		end := base + contents[1]
		replacement := fmt.Sprintf("/ByteRange [ 0 %010d %010d %010d ]", start, end, total-end)
		if len(replacement) != br[1]-br[0] {
			return nil, fmt.Errorf("patched /ByteRange changes width, which would move an xref offset")
		}
		copy(data[base+br[0]:base+br[1]], replacement)
		patched++
	}

	if patched != 1 {
		return nil, fmt.Errorf("patched %d signatures, expected 1", patched)
	}
	return data, nil
}
