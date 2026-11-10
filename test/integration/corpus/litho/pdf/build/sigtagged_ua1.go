package build

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("sigtagged_ua1", sigtaggedUA1) }

// sigtaggedUa1 builds sigtagged_ua1.pdf: a certified, tagged form under the mutations.
//
// ISO 32000-1, 12.8.1: a signature covers the bytes its /ByteRange names. An
// incremental update appends after them and keeps it; a full rewrite moves every
// one of them, so litho draws each signed widget's appearance into its page and
// drops the signature, its field and the /Perms entry naming it. No fixture was
// both tagged and signed, so the gate never saw a signature drawn under the Form
// element that enclosed its widget, nor a certification dropped from /Perms.
//
// One tagged page shows "AB" as a paragraph and presents three fields, each merged
// with its widget, the only kid of its own Form element and carrying a /TU:
//
//   - "approval", a signature field signed by a genuine CMS signature whose
//     /ByteRange covers the file, which /Perms /DocMDP names (P 2: filling in
//     forms and signing stay permitted), drawing a box and a stroke;
//   - "name", a text field whose appearance paints a box;
//   - "cosign", an unsigned signature field whose appearance paints a box.
//
// No appearance draws text, so drawing one into the page changes no page text.
// The page's text shows only A and B in the conforming embedded TrueType of
// ttcmap_2b.pdf. The XMP packet carries dc:title and pdfuaid:part 1.
//
// The signature is made here, deterministically: an RSA PKCS #1 v1.5 signature
// over the SHA-256 of the covered bytes, in a detached CMS SignedData with no
// signed attributes (RFC 5652, 5.4), so no signing time varies between builds.
// The key and its self-signed certificate are throwaway test material generated
// once for this fixture. poppler's pdfsig reports the signature valid, its
// certificate untrusted.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func sigtaggedUA1() ([]byte, error) {
	// A throwaway RSA-2048 key and its self-signed certificate, CN "litho corpus
	// signer", serial 7. Test material only: it signs this fixture and nothing else.
	rsaN, _ := new(big.Int).SetString(
		"eedf10fc2d7718a2f2c17c918e50dc01eb34480ddde2e37c25d976bf11c7e572"+
			"6804fe3421a2190a4334e031652ec1914506d0a7dbd96170a18c26e0ed7e27a8"+
			"9e4288ca1e0098d30d8c52d9e4b8e24a2fa52e2a3c9186780c363bec11133b2c"+
			"e143df2718ae75a6f11eec097afade2389b958aeca09b2e71b3cdca2da86adb2"+
			"e2b89f6a06a57b21af9d8985642c76391ce4d4fe3570d65c9b0591b9360a588d"+
			"d3c8f051948931a290dd9dff6cebf87de73f876188694efafd51ef54c9382c38"+
			"d1d9947a5c6854bc21c30596ee77f2e21a8227d1ac82c6e1cbc6521d66021ade"+
			"06807268d57e9fe36dae8e05dfe6015a0bfe5db85c5d643e35e7b270766310d7",
		16,
	)
	rsaD, _ := new(big.Int).SetString(
		"2d87108aaff3b21b649f6229aff1536d7e31d1c5ead392be17ccb1e84af04998"+
			"1ba14ffe09a70694c12873ce96556a095119b26d3be6dbbe86dbda912652d426"+
			"3c3bbc2ca1b945677c7e914ffc5b0cae4d14738f134a3d51e629f413d3515207"+
			"04cc55dcf2c455a242f4bcd235c158d3772d7f6c990f6e2cc4313e703fac49b0"+
			"9f6615dfbb99f5c16427cd4db48fbe4d47f87cb795e85e59d6eece025a67f94d"+
			"4ddfd6f28e80426798a3b7910842665d9998e09fe8f80fe55e37669a63bb5b95"+
			"99fba74daa1a9493c93b176cc221dbfe0b004dd35a8dfd11f34c609b91e50123"+
			"344f8ee7d82f0b559a483a1c839251a54310d9344506ee4185bc0e76a39e8581",
		16,
	)
	const certB64 = `
MIIDCjCCAfKgAwIBAgIBBzANBgkqhkiG9w0BAQsFADAeMRwwGgYDVQQDDBNsaXRobyBjb3JwdXMg
c2lnbmVyMB4XDTI2MDkyNzA5NTc1NFoXDTM2MDkyNDA5NTc1NFowHjEcMBoGA1UEAwwTbGl0aG8g
Y29ycHVzIHNpZ25lcjCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAO7fEPwtdxii8sF8
kY5Q3AHrNEgN3eLjfCXZdr8Rx+VyaAT+NCGiGQpDNOAxZS7BkUUG0Kfb2WFwoYwm4O1+J6ieQojK
HgCY0w2MUtnkuOJKL6UuKjyRhngMNjvsERM7LOFD3ycYrnWm8R7sCXr63iOJuViuygmy5xs83KLa
hq2y4rifagaleyGvnYmFZCx2ORzk1P41cNZcmwWRuTYKWI3TyPBRlIkxopDdnf9s6/h95z+HYYhp
Tvr9Ue9UyTgsONHZlHpcaFS8IcMFlu538uIagifRrILG4cvGUh1mAhreBoByaNV+n+Ntro4F3+YB
Wgv+XbhcXWQ+NeeycHZjENcCAwEAAaNTMFEwHQYDVR0OBBYEFAt9nfgKsgLU8+94cjIEJuVTkYr5
MB8GA1UdIwQYMBaAFAt9nfgKsgLU8+94cjIEJuVTkYr5MA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZI
hvcNAQELBQADggEBAOxAN5reF10xTWxk2SviBZdCx+CWckajjrnHIq5Gt/g/ZyXGXjQTzjUQi3WB
uMzKTvht8WKvuK4IjrRrOOetjnZz7Pm8uDrf1tpeLl2N8Gx5nOkAwaLNJd3tpVP7bsnBgGiDHZHq
XAhbWuK6CZzjIO3VIQFJJmUUHtT2S4oRHOdOHoxgRfdRupZ8DTWcHOYOBH9vbKcUvQkM+joc8lj4
AhOMVXg/I+AR3nf963OmmsbtENk1QKaLK+Tr3nqT7a+UVXVK7rYWDARkIB7V2XbYFBdPt3lji/ne
YiiYtNyf4jaR19gNs5XDnuwZFDFlbtb2jSuHEGGElLYfXPydwTeEYmc=
`
	const issuerB64 = "MB4xHDAaBgNVBAMME2xpdGhvIGNvcnB1cyBzaWduZXI="
	const serial = 7

	// The bytes reserved for the CMS blob in /Contents. The blob is about 1.3 KB.
	const signatureBytes = 4096

	// A ten-digit placeholder leaves the patched value the same width as the written
	// one, so no xref offset moves.
	const placeholder = 1000000000

	// der is one DER element: tag, definite length, body (X.690, 8.1).
	der := func(tag byte, body []byte) []byte {
		n := len(body)
		var length []byte
		if n < 0x80 {
			length = []byte{byte(n)}
		} else {
			octets := big.NewInt(int64(n)).Bytes()
			length = append([]byte{0x80 | byte(len(octets))}, octets...) //nolint:gosec // a length fits in a few octets.
		}
		out := append([]byte{tag}, length...)
		return append(out, body...)
	}

	// derInt is a DER INTEGER holding a non-negative value.
	derInt := func(value *big.Int) []byte {
		body := value.FillBytes(make([]byte, value.BitLen()/8+1))
		return der(0x02, body)
	}

	// derOID is a DER OBJECT IDENTIFIER.
	derOID := func(dotted string) []byte {
		parts := strings.Split(dotted, ".")
		arcs := make([]int, 0, len(parts))
		for _, s := range parts {
			v, err := strconv.Atoi(s)
			if err != nil {
				panic(err)
			}
			arcs = append(arcs, v)
		}
		body := []byte{byte(40*arcs[0] + arcs[1])} //nolint:gosec // the first two arcs of the OIDs used.
		for _, arc := range arcs[2:] {
			chunk := []byte{byte(arc & 0x7F)}
			arc >>= 7
			for arc != 0 {
				chunk = append(chunk, 0x80|byte(arc&0x7F))
				arc >>= 7
			}
			for _, c := range slices.Backward(chunk) {
				body = append(body, c)
			}
		}
		return der(0x06, body)
	}

	seq := func(items ...[]byte) []byte {
		return der(0x30, bytes.Join(items, nil))
	}

	setOf := func(items ...[]byte) []byte {
		return der(0x31, bytes.Join(items, nil))
	}

	sha256Algorithm := seq(derOID("2.16.840.1.101.3.4.2.1"))
	rsaEncryption := seq(derOID("1.2.840.113549.1.1.1"), []byte("\x05\x00"))

	// rsaSign is RSASSA-PKCS1-v1_5 over a SHA-256 digest (RFC 8017, 8.2.1 and
	// 9.2).
	rsaSign := func(digest []byte) []byte {
		k := (rsaN.BitLen() + 7) / 8
		// DigestInfo, whose algorithm carries NULL parameters (RFC 8017, 9.2 note 1).
		t := seq(seq(derOID("2.16.840.1.101.3.4.2.1"), []byte("\x05\x00")), der(0x04, digest))
		em := append([]byte("\x00\x01"), bytes.Repeat([]byte{0xff}, k-len(t)-3)...)
		em = append(em, 0x00)
		em = append(em, t...)
		sig := new(big.Int).Exp(new(big.Int).SetBytes(em), rsaD, rsaN)
		return sig.FillBytes(make([]byte, k))
	}

	// cmsDetached is a detached CMS SignedData with one signer and its
	// certificate, no signed attributes (RFC 5652, 5.1 and 5.3).
	cmsDetached := func(digest []byte) []byte {
		cert, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(certB64), ""))
		if err != nil {
			panic(err)
		}
		issuer, err := base64.StdEncoding.DecodeString(issuerB64)
		if err != nil {
			panic(err)
		}
		signer := seq(
			derInt(big.NewInt(1)),
			seq(issuer, derInt(big.NewInt(serial))),
			sha256Algorithm,
			rsaEncryption,
			der(0x04, rsaSign(digest)),
		)
		signedData := seq(
			derInt(big.NewInt(1)),
			setOf(sha256Algorithm),
			seq(derOID("1.2.840.113549.1.7.1")),
			der(0xA0, cert),
			setOf(signer),
		)
		return seq(derOID("1.2.840.113549.1.7.2"), der(0xA0, signedData))
	}

	pdf := pdfgen.New()

	latin := map[int]string{0x41: "A", 0x42: "B"}
	program := ttBuild([]ttSubtable{{3, 1, latin}, {1, 0, latin}}, nil)
	fontFile := pdf.Stream(program)
	fontFile.Set("Length1", len(program))
	descriptor := d(
		"Type", n("FontDescriptor"),
		"FontName", n("/LithoSignedSans"),
		"Flags", 32, // non-symbolic, ISO 32000-1, Table 123
		"ItalicAngle", 0,
		"Ascent", 800,
		"Descent", -200,
		"CapHeight", 700,
		"StemV", 80,
		"FontBBox", a(0, -200, 550, 700),
		"FontFile2", pdf.MakeIndirect(fontFile),
	)
	const first, last = 0x41, 0x42
	widths := a()
	for i := first; i <= last; i++ {
		widths.Append(ttAdvanceWidth)
	}
	font := pdf.Indirect(d(
		"Type", n("Font"),
		"Subtype", n("TrueType"),
		"BaseFont", n("/LithoSignedSans"),
		"FirstChar", first,
		"LastChar", last,
		"Widths", widths,
		"FontDescriptor", pdf.Indirect(descriptor),
		"Encoding", n("WinAnsiEncoding"),
	))

	// appearance is a Form XObject appearance of the given size painting
	// content.
	appearance := func(width, height int, content string) obj {
		s := pdf.Stream([]byte(content))
		s.Set("Type", n("XObject"))
		s.Set("Subtype", n("Form"))
		s.Set("BBox", a(0, 0, width, height))
		return pdf.MakeIndirect(s)
	}

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.Stream([]byte("/P <</MCID 0>> BDC\nBT /F1 12 Tf 50 740 Td (AB) Tj ET\nEMC\n")),
		"StructParents", 0,
		"Tabs", n("S"),
	))
	pdf.AddPage(page)

	signature := pdf.Indirect(d(
		"Type", n("Sig"),
		"Filter", n("/Adobe.PPKLite"),
		"SubFilter", n("/adbe.pkcs7.detached"),
		"Name", text("litho corpus signer"),
		"Reference", a(
			d(
				"Type", n("SigRef"),
				"TransformMethod", n("DocMDP"),
				"TransformParams", d("Type", n("TransformParams"), "P", 2, "V", n("/1.2")),
			),
		),
		"ByteRange", a(0, placeholder, placeholder, placeholder),
		"Contents", pdfgen.String(make([]byte, signatureBytes)),
	))

	// field is a field merged with its widget, printed, carrying a /TU.
	field := func(name, ft string, rect []int, ap obj, structParent int, value obj) *pdfgen.Dict {
		dd := d(
			"Type", n("Annot"),
			"Subtype", n("Widget"),
			"Rect", rect,
			"F", 4,
			"P", page,
			"FT", n(ft),
			"T", text(name),
			"TU", text(strings.ToUpper(name[:1])+name[1:]),
			"AP", d("N", ap),
			"StructParent", structParent,
		)
		if value != nil {
			dd.Set("V", value)
		}
		return pdf.Indirect(dd)
	}

	approval := field(
		"approval",
		"/Sig",
		[]int{300, 700, 500, 750},
		appearance(
			200,
			50,
			"0.95 g 0 0 200 50 re f\n0 0 0.5 RG 1.5 w 10 15 m 40 45 60 5 90 30 c 120 50 140 10 180 25 c S\n",
		),
		1,
		signature,
	)
	name := field("name", "/Tx", []int{50, 650, 150, 670}, appearance(100, 20, "0.9 g 0 0 100 20 re f\n"), 2, text("Ada"))
	cosign := field("cosign", "/Sig", []int{300, 600, 500, 650}, appearance(200, 50, "0.8 g 0 0 200 50 re f\n"), 3, nil)
	widgets := []any{approval, name, cosign}
	page.Set("Annots", a(widgets...))
	pdf.Root.Set("AcroForm", d("Fields", a(widgets...), "SigFlags", 3))
	pdf.Root.Set("Perms", d("DocMDP", signature))

	structRoot := pdf.Indirect(d("Type", n("StructTreeRoot")))
	document := pdf.Indirect(d("Type", n("StructElem"), "S", n("Document"), "P", structRoot))
	paragraph := pdf.Indirect(d("Type", n("StructElem"), "S", n("P"), "P", document, "Pg", page, "K", 0))

	// form is a Form element whose only kid is an object reference to widget.
	form := func(widget *pdfgen.Dict) *pdfgen.Dict {
		return pdf.Indirect(d(
			"Type", n("StructElem"),
			"S", n("Form"),
			"P", document,
			"Pg", page,
			"K", d("Type", n("OBJR"), "Obj", widget),
		))
	}

	formApproval, formName, formCosign := form(approval), form(name), form(cosign)
	document.Set("K", a(paragraph, formApproval, formName, formCosign))
	structRoot.Set("K", document)
	structRoot.Set("ParentTree", pdf.Indirect(
		d("Nums", a(0, a(paragraph), 1, formApproval, 2, formName, 3, formCosign)),
	))
	structRoot.Set("ParentTreeNextKey", 4)

	pdf.Root.Set("StructTreeRoot", structRoot)
	pdf.Root.Set("MarkInfo", d("Marked", true))
	pdf.Root.Set("Lang", text("en-US"))
	pdf.Root.Set("ViewerPreferences", d("DisplayDocTitle", true))
	attachXMP(pdf, xmpPacket(xmpOpts{UAPart: 1}))

	data, err := save(pdf, "1.7")
	if err != nil {
		return nil, err
	}

	// The signature covers the file but its own /Contents, so it is made last: the
	// /ByteRange is patched against where /Contents landed, then the covered bytes
	// are hashed and signed, and the blob fills the hole. Padding both to a fixed
	// width keeps every xref offset where the save put it.
	var match []int
	for _, obj := range regexp.MustCompile(`(?s)\d+ 0 obj(.*?)endobj`).FindAllSubmatchIndex(data, -1) {
		if bytes.Contains(data[obj[2]:obj[3]], []byte("/Type /Sig")) {
			if match != nil {
				return nil, fmt.Errorf("more than one signature dictionary")
			}
			match = obj
		}
	}
	if match == nil {
		return nil, fmt.Errorf("no signature dictionary")
	}
	body, base := data[match[2]:match[3]], match[2]
	br := regexp.MustCompile(fmt.Sprintf(`/ByteRange \[ 0 %d %d %d \]`, placeholder, placeholder, placeholder)).FindIndex(body)
	contents := regexp.MustCompile(`/Contents <0+>`).FindIndex(body)
	if br == nil || contents == nil {
		return nil, fmt.Errorf("the signature dictionary is not in the expected shape")
	}
	start := base + contents[0] + len("/Contents ")
	end := base + contents[1]
	replacement := fmt.Sprintf("/ByteRange [ 0 %010d %010d %010d ]", start, end, len(data)-end)
	if len(replacement) != br[1]-br[0] {
		return nil, fmt.Errorf("the patched /ByteRange changes width, which would move an xref offset")
	}
	copy(data[base+br[0]:base+br[1]], replacement)

	h := sha256.New()
	h.Write(data[:start])
	h.Write(data[end:])
	blob := hex.EncodeToString(cmsDetached(h.Sum(nil)))
	if len(blob) > 2*signatureBytes {
		return nil, fmt.Errorf("the CMS blob does not fit its placeholder")
	}
	copy(data[start+1:end-1], blob+strings.Repeat("0", 2*signatureBytes-len(blob)))
	return data, nil
}
