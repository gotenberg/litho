package build

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	"github.com/gotenberg/litho/test/integration/fixture/pdfgen"
)

func init() { register("sigcms_2b", sigcms2b) }

// sigcms2b builds sigcms_2b.pdf: one signature per PDF/A-2b digital-signature verdict.
//
// | Field      | `/Contents`                   | `/ByteRange`    | Rule      |
// | ---------- | ----------------------------- | --------------- | --------- |
// | `sig0`     | one signer, certificate       | covers the file | silent    |
// | `sig1`     | one signer, certificate       | 64 bytes short  | `6.4.3/1` |
// | `sig2`     | one signer, no certificate    | covers the file | `6.4.3/2` |
// | `sig3`     | two signers, certificate      | covers the file | `6.4.3/3` |
// | `sigempty` | no `/V` at all                | n/a             | `6.3.3/1` |
// | `text`     | a text field, not a signature | n/a             | `6.3.3/1` |
//
// ISO 19005-2, 6.4.3 splits into three tests over two objects. Test 1 asks the
// PDSignature whether its `/ByteRange` covers the whole document. Tests 2 and 3
// ask the CMS SignedData in `/Contents` whether a signing certificate rides along
// and whether there is exactly one SignerInfo. Nothing about the first is visible
// from the second, so a checker that reads only `/ByteRange` reports `sig1` and
// misses `sig2` and `sig3` entirely.
//
// Each of the three defects sits on its own field with the other two conformant,
// and `sig0` is conformant on all three. A verdict attached to the wrong field, or
// one derived from the document rather than the signature it belongs to, therefore
// answers on `sig0` too and shows up immediately.
//
// The blobs are genuine `openssl cms -sign` output, embedded here rather than
// generated, because openssl stamps a signingTime and its output is not
// reproducible across runs. They are the same bytes as
// `pkg/internal/compliance/testdata/cms_{real,nocert,two}.der`, which is what
// veraPDF 1.30.2 was measured against.
//
// `/ByteRange` is written as a ten-digit zero-padded placeholder and patched after
// the save against the offset the signature's own `/Contents` landed at. Padding
// to a fixed width is what keeps the patch from moving an xref offset. An
// unparseable blob would answer tests 2 and 3 together and mask the split, so
// every blob here is real CMS.
//
// `sigempty` and `text` carry no `/AP`, which answers 6.3.3 test 1 twice. They are
// the fixture's second purpose: a widget annotation is normally merged with its
// form field (ISO 32000-1, 12.7.4.1), so it is reached by the page `/Annots` walk
// and the `/AcroForm` `/Fields` walk alike, and a checker running both states the
// same missing appearance twice. The four signature widgets carry an `/AP` so this
// lands on two objects rather than six.
//
// Beyond the forms the file has no `/Metadata` and paints text in an unembedded
// Helvetica with no OutputIntent, three further independent defects that keep the
// signature verdicts visibly separate from the rest of the document.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func sigcms2b() ([]byte, error) {
	// A ten-digit placeholder leaves the patched value the same width as the written
	// one, so no xref offset moves.
	const placeholder = 1000000000

	// How far sig1's covered range falls short of the end of the file.
	const shortfall = 64

	// openssl cms -sign -binary -in data -signer c1.pem -inkey k1.pem \
	//     -outform DER -nodetach -md sha256
	const cmsRealB64 = `
MIIFtwYJKoZIhvcNAQcCoIIFqDCCBaQCAQExDTALBglghkgBZQMEAgEwKgYJKoZIhvcNAQcBoB0E
G2xpdGhvIGNvcnB1cyBzaWduZWQgY29udGVudKCCAxswggMXMIIB/6ADAgECAhRo6ToXVQ9CV4YY
y8g/o1LKj79q0DANBgkqhkiG9w0BAQsFADAbMRkwFwYDVQQDDBBsaXRobyBjb3JwdXMgb25lMB4X
DTI2MDgwMzE3NTIxOFoXDTM2MDczMTE3NTIxOFowGzEZMBcGA1UEAwwQbGl0aG8gY29ycHVzIG9u
ZTCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBANanVSvizJ/dUXKF+I6Bszg144PFfvpz
fz7QW2T0aJhp9Lc/YpOGn2ZfqYixDk46B9Qgb9hMWe5vDw1SHXbtxp59SVPElIT63PnBc4aGAZcs
dL66iN7Wajj7dc5j37/6gSb8wkJCL0/UWgA12kQcBLDvYhvjbTxZpbVoiuQ7jn83v0YNa2e1V37c
bcRLIci5rhmbCqy0ostvI4tFW8cIggtkOXjDwUwZL/QaNyhCQNkdgOi//APwkGx8YqvdpzSHyLH3
JlTK4WSMiwdKyfpmBnUnCJZKx8X6jmGjMZkv0VuwM497g0O5xw7DoOWVJmi5cpJJbudHFANebdqA
JnmhOHUCAwEAAaNTMFEwHQYDVR0OBBYEFJPDSf1ExwFLMh92cOdgd/2pgdnlMB8GA1UdIwQYMBaA
FJPDSf1ExwFLMh92cOdgd/2pgdnlMA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcNAQELBQADggEB
AEqC87rbfgXJ6m3xGnK3jHbEGwVEqr4zpBVMbVVbhZA9O1uAyzTzL4hP5CTSUj8N+3Qoi+OMjkhQ
ZKr9evMcUgCt4zQVP35DqrdfQKCcZr094gMONegEIdMc8mJvTgywZVjgxAicV9G02BEHazlHPp3n
Feb+MjdzshQp7uaGpxL+uH6+rkc5YIBFIy7VCjQMtKScV6tMhCnyHLz3Jt+gc3i7YwMPn/5cQDrO
nE/0YuE4eF8LmQgySuSSXf5wFdHm79ayBQl4D4r7kPGLAPCg61gYPZbWntp12ENCaR/PdW6RNGVL
wBs13xaZDqFCSkFzKGF97G7jWxbh38toEhmMEC4xggJDMIICPwIBATAzMBsxGTAXBgNVBAMMEGxp
dGhvIGNvcnB1cyBvbmUCFGjpOhdVD0JXhhjLyD+jUsqPv2rQMAsGCWCGSAFlAwQCAaCB5DAYBgkq
hkiG9w0BCQMxCwYJKoZIhvcNAQcBMBwGCSqGSIb3DQEJBTEPFw0yNjA4MDMxNzUyMThaMC8GCSqG
SIb3DQEJBDEiBCBgc0WS1bme80WXqfhcwsqTnNYz6CuxvfCaFWZy5u7xGDB5BgkqhkiG9w0BCQ8x
bDBqMAsGCWCGSAFlAwQBKjALBglghkgBZQMEARYwCwYJYIZIAWUDBAECMAoGCCqGSIb3DQMHMA4G
CCqGSIb3DQMCAgIAgDANBggqhkiG9w0DAgIBQDAHBgUrDgMCBzANBggqhkiG9w0DAgIBKDANBgkq
hkiG9w0BAQEFAASCAQCtXPtE2WbW6RElaOjv0xxpjVPkx7RNiKl9r2vn9KqHXOrsWXiEZi8uoUon
X69Glg0SDtfAijPKd8PHnhh6BHjIhc43hq+PhrXZlWOPgiLGooaPc1vXaI4bto5KvlPlpzXsR6bS
PGxnjUpXnaWcGRjeFWkHTFUJXAQL1by3xEDE9odSUrAou3eF6Q501HbN4gCvjgd+hERviWu/ReP5
EQd05+c+Sho67c2eCeU+Ru5zG0+jg9lxbZMHoELFTt8clHhJdCmEiwKRqn5vyPQ9ELexN3cyjxnY
cOSc915k1Halr7E6dsoslFMImJc9W7/aT1DL3kkSXgMzVZmOX70ozg1A
`

	// The same, with -nocerts.
	const cmsNocertB64 = `
MIICmAYJKoZIhvcNAQcCoIICiTCCAoUCAQExDTALBglghkgBZQMEAgEwKgYJKoZIhvcNAQcBoB0E
G2xpdGhvIGNvcnB1cyBzaWduZWQgY29udGVudDGCAkMwggI/AgEBMDMwGzEZMBcGA1UEAwwQbGl0
aG8gY29ycHVzIG9uZQIUaOk6F1UPQleGGMvIP6NSyo+/atAwCwYJYIZIAWUDBAIBoIHkMBgGCSqG
SIb3DQEJAzELBgkqhkiG9w0BBwEwHAYJKoZIhvcNAQkFMQ8XDTI2MDgwMzE3NTIxOFowLwYJKoZI
hvcNAQkEMSIEIGBzRZLVuZ7zRZep+FzCypOc1jPoK7G98JoVZnLm7vEYMHkGCSqGSIb3DQEJDzFs
MGowCwYJYIZIAWUDBAEqMAsGCWCGSAFlAwQBFjALBglghkgBZQMEAQIwCgYIKoZIhvcNAwcwDgYI
KoZIhvcNAwICAgCAMA0GCCqGSIb3DQMCAgFAMAcGBSsOAwIHMA0GCCqGSIb3DQMCAgEoMA0GCSqG
SIb3DQEBAQUABIIBAK1c+0TZZtbpESVo6O/THGmNU+THtE2IqX2va+f0qodc6uxZeIRmLy6hSidf
r0aWDRIO18CKM8p3w8eeGHoEeMiFzjeGr4+GtdmVY4+CIsaiho9zW9dojhu2jkq+U+WnNexHptI8
bGeNSledpZwZGN4VaQdMVQlcBAvVvLfEQMT2h1JSsCi7d4XpDnTUds3iAK+OB36ERG+Ja79F4/kR
B3Tn5z5KGjrtzZ4J5T5G7nMbT6OD2XFtkwegQsVO3xyUeEl0KYSLApGqfm/I9D0Qt7E3dzKPGdhw
5Jz3XmTUdqWvsTp2yiyUUwiYlz1bv9pPUMveSRJeAzNVmY5fvSjODUA=
`

	// The same, with a second -signer/-inkey pair.
	const cmsTwoB64 = `
MIILFQYJKoZIhvcNAQcCoIILBjCCCwICAQExDTALBglghkgBZQMEAgEwKgYJKoZIhvcNAQcBoB0E
G2xpdGhvIGNvcnB1cyBzaWduZWQgY29udGVudKCCBjYwggMXMIIB/6ADAgECAhQG66FjF5XetGSV
iR/mZrvl329SKzANBgkqhkiG9w0BAQsFADAbMRkwFwYDVQQDDBBsaXRobyBjb3JwdXMgdHdvMB4X
DTI2MDgwMzE3NTIxOFoXDTM2MDczMTE3NTIxOFowGzEZMBcGA1UEAwwQbGl0aG8gY29ycHVzIHR3
bzCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAKZ5jgEBDqBBiEfHRBWRBY5zQvVmyl6y
doBL7R49mqL14T6EjU85RdOH9EaM1W3K28wvCLPqfaRmY81vZ77i0q1gyYBHyMFxAVBoOTe/sGYl
uGS33Gg6djZf+iUyeNKcgl+9AkEH7tnVA91+zbxqNcl8O15Q/MSIyIArj3pH16F7Xxflqn5K6CBQ
bkMpdwqKpi+Jr3Iq4gnw92WStI2vUjOhfVswNetQCJ8pIPIviC28RGtzpiVbK09hapv35y1PsVE4
XR8nUx2jNaenSz+lx8XEjoWV5mKvS9nsdn2+MRUP26IpaWOR0ppBKx3e+hasnWVDMqqunPdVOUhF
6w3Ljx8CAwEAAaNTMFEwHQYDVR0OBBYEFMNYWLBiGLrHP/DHKse4GxUsedITMB8GA1UdIwQYMBaA
FMNYWLBiGLrHP/DHKse4GxUsedITMA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcNAQELBQADggEB
AI/jHFeDeLVjj1wfwE+VDEkkALvFaElXt3JSOVXPEWS8NetvY1z+4Z19TMosXs8farrEI3rRGGPd
YU4n5m9KRrAHdeyCKNlkZP+khMRYdVytpk9aX6jCNq0z8uVINxKKdLh+aHXLfb0gmGtg345Go+uE
41uXibagabam8lGUrMQaWjRRCX3+5AGS4SlaES7S2DDgCVYGy+cVmVcK7AZyU0zKvZnsJFQOoRnU
NMeuspmnblOvS0SalhbvxNNSAAIn1pBTGcQ5quEes+yvCT7T3Oa/PwNJ38Eqt2FcVaOXKg1dmjlg
jCJ1DUpU/+o/GtmJS0n/vXm811Hgg97BVM0dvqQwggMXMIIB/6ADAgECAhRo6ToXVQ9CV4YYy8g/
o1LKj79q0DANBgkqhkiG9w0BAQsFADAbMRkwFwYDVQQDDBBsaXRobyBjb3JwdXMgb25lMB4XDTI2
MDgwMzE3NTIxOFoXDTM2MDczMTE3NTIxOFowGzEZMBcGA1UEAwwQbGl0aG8gY29ycHVzIG9uZTCC
ASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBANanVSvizJ/dUXKF+I6Bszg144PFfvpzfz7Q
W2T0aJhp9Lc/YpOGn2ZfqYixDk46B9Qgb9hMWe5vDw1SHXbtxp59SVPElIT63PnBc4aGAZcsdL66
iN7Wajj7dc5j37/6gSb8wkJCL0/UWgA12kQcBLDvYhvjbTxZpbVoiuQ7jn83v0YNa2e1V37cbcRL
Ici5rhmbCqy0ostvI4tFW8cIggtkOXjDwUwZL/QaNyhCQNkdgOi//APwkGx8YqvdpzSHyLH3JlTK
4WSMiwdKyfpmBnUnCJZKx8X6jmGjMZkv0VuwM497g0O5xw7DoOWVJmi5cpJJbudHFANebdqAJnmh
OHUCAwEAAaNTMFEwHQYDVR0OBBYEFJPDSf1ExwFLMh92cOdgd/2pgdnlMB8GA1UdIwQYMBaAFJPD
Sf1ExwFLMh92cOdgd/2pgdnlMA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcNAQELBQADggEBAEqC
87rbfgXJ6m3xGnK3jHbEGwVEqr4zpBVMbVVbhZA9O1uAyzTzL4hP5CTSUj8N+3Qoi+OMjkhQZKr9
evMcUgCt4zQVP35DqrdfQKCcZr094gMONegEIdMc8mJvTgywZVjgxAicV9G02BEHazlHPp3nFeb+
MjdzshQp7uaGpxL+uH6+rkc5YIBFIy7VCjQMtKScV6tMhCnyHLz3Jt+gc3i7YwMPn/5cQDrOnE/0
YuE4eF8LmQgySuSSXf5wFdHm79ayBQl4D4r7kPGLAPCg61gYPZbWntp12ENCaR/PdW6RNGVLwBs1
3xaZDqFCSkFzKGF97G7jWxbh38toEhmMEC4xggSGMIICPwIBATAzMBsxGTAXBgNVBAMMEGxpdGhv
IGNvcnB1cyBvbmUCFGjpOhdVD0JXhhjLyD+jUsqPv2rQMAsGCWCGSAFlAwQCAaCB5DAYBgkqhkiG
9w0BCQMxCwYJKoZIhvcNAQcBMBwGCSqGSIb3DQEJBTEPFw0yNjA4MDMxNzUyMThaMC8GCSqGSIb3
DQEJBDEiBCBgc0WS1bme80WXqfhcwsqTnNYz6CuxvfCaFWZy5u7xGDB5BgkqhkiG9w0BCQ8xbDBq
MAsGCWCGSAFlAwQBKjALBglghkgBZQMEARYwCwYJYIZIAWUDBAECMAoGCCqGSIb3DQMHMA4GCCqG
SIb3DQMCAgIAgDANBggqhkiG9w0DAgIBQDAHBgUrDgMCBzANBggqhkiG9w0DAgIBKDANBgkqhkiG
9w0BAQEFAASCAQCtXPtE2WbW6RElaOjv0xxpjVPkx7RNiKl9r2vn9KqHXOrsWXiEZi8uoUonX69G
lg0SDtfAijPKd8PHnhh6BHjIhc43hq+PhrXZlWOPgiLGooaPc1vXaI4bto5KvlPlpzXsR6bSPGxn
jUpXnaWcGRjeFWkHTFUJXAQL1by3xEDE9odSUrAou3eF6Q501HbN4gCvjgd+hERviWu/ReP5EQd0
5+c+Sho67c2eCeU+Ru5zG0+jg9lxbZMHoELFTt8clHhJdCmEiwKRqn5vyPQ9ELexN3cyjxnYcOSc
915k1Halr7E6dsoslFMImJc9W7/aT1DL3kkSXgMzVZmOX70ozg1AMIICPwIBATAzMBsxGTAXBgNV
BAMMEGxpdGhvIGNvcnB1cyB0d28CFAbroWMXld60ZJWJH+Zmu+Xfb1IrMAsGCWCGSAFlAwQCAaCB
5DAYBgkqhkiG9w0BCQMxCwYJKoZIhvcNAQcBMBwGCSqGSIb3DQEJBTEPFw0yNjA4MDMxNzUyMTha
MC8GCSqGSIb3DQEJBDEiBCBgc0WS1bme80WXqfhcwsqTnNYz6CuxvfCaFWZy5u7xGDB5BgkqhkiG
9w0BCQ8xbDBqMAsGCWCGSAFlAwQBKjALBglghkgBZQMEARYwCwYJYIZIAWUDBAECMAoGCCqGSIb3
DQMHMA4GCCqGSIb3DQMCAgIAgDANBggqhkiG9w0DAgIBQDAHBgUrDgMCBzANBggqhkiG9w0DAgIB
KDANBgkqhkiG9w0BAQEFAASCAQAtJMIvfGBO8cQjDFUvz+yM54eS7JkqgrbWGAUwODgleYXNYNad
giRH/mqy8vEOQ8daR5jL5Hv6En/lKRB86DLXueE5GsY1nfN9NxLSMdH3LnLHrnk9gz9ETkCsOslT
qnZsH9EDQUmNr9eUNkiPo5qlzQpNGgJMFFyTxn55YgUsiWeKacXQxG+YzGvFSJt06Rw7GsbEoWME
AFXzsHzmCEBxZ5CgeXPCsEYltI9aGvFSNsKEWQJ59mplZuREhKLIr+9mUIe1SsVTz5LxJ9/9lKZR
FZUtNLLwnGgewhYRsrws/OmYabePpmWXUKGuHO0adoRQ/whbKMgVc1lusmB1IUTb
`

	// blob decodes one of the embedded CMS SignedData blobs.
	blob := func(text string) []byte {
		b, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(text), ""))
		if err != nil {
			panic(err)
		}
		return b
	}

	pdf := pdfgen.New()

	font := pdf.Indirect(d(
		"Type", n("Font"), "Subtype", n("Type1"),
		"BaseFont", n("/Helvetica"), "Encoding", n("WinAnsiEncoding")))

	// appearance is an empty /AP /N form, so 6.3.3 answers only for the two
	// fields without one.
	appearance := func() *pdfgen.Dict {
		stream := pdf.Stream([]byte(""))
		stream.Set("Type", n("XObject"))
		stream.Set("Subtype", n("Form"))
		stream.Set("BBox", a(0, 0, 150, 20))
		stream.Set("Resources", d())
		return d("N", pdf.MakeIndirect(stream))
	}

	// widget is one merged field/widget dictionary, per ISO 32000-1, 12.7.4.1.
	widget := func(rect []int, label string) *pdfgen.Dict {
		return d("Type", n("Annot"), "Subtype", n("Widget"),
			"Rect", rect, "F", 132, "T", text(label))
	}

	// signature is a signature value carrying a real CMS blob and a patchable
	// /ByteRange.
	signature := func(label string, der []byte) *pdfgen.Dict {
		return pdf.Indirect(d(
			"Type", n("Sig"),
			"Filter", n("/Adobe.PPKLite"),
			"SubFilter", n("/adbe.pkcs7.detached"),
			"Name", text(label),
			"ByteRange", a(0, placeholder, placeholder, placeholder),
			"Contents", pdfgen.String(der),
		))
	}

	// short marks the field whose covered range stops before the end of the file.
	type signed struct {
		label  string
		derB64 string
		short  bool
	}
	signedFields := []signed{
		{"sig0", cmsRealB64, false},
		{"sig1", cmsRealB64, true},
		{"sig2", cmsNocertB64, false},
		{"sig3", cmsTwoB64, false},
	}

	fields := make([]any, 0, len(signedFields)+2)
	y := 700
	for _, s := range signedFields {
		w := widget([]int{50, y, 200, y + 20}, s.label)
		w.Set("FT", n("Sig"))
		w.Set("V", signature(s.label, blob(s.derB64)))
		w.Set("AP", appearance())
		fields = append(fields, pdf.Indirect(w))
		y -= 40
	}

	// A signature field with no /V: there is no CMS blob to read, so neither
	// 6.4.3 test 2 nor test 3 has an object to answer for.
	w := widget([]int{50, y, 200, y + 20}, "sigempty")
	w.Set("FT", n("Sig"))
	fields = append(fields, pdf.Indirect(w))
	y -= 40

	// A text field, so a checker reading every /AcroForm field as a signature
	// reports a fourth signature verdict that has no signature behind it.
	w = widget([]int{50, y, 200, y + 20}, "text")
	w.Set("FT", n("Tx"))
	w.Set("V", text("not a signature"))
	fields = append(fields, pdf.Indirect(w))

	page := pdf.Indirect(d(
		"Type", n("Page"),
		"MediaBox", a(0, 0, 612, 792),
		"Resources", d("Font", d("F1", font)),
		"Contents", pdf.Stream([]byte("BT /F1 12 Tf 50 750 Td (four signatures) Tj ET\n")),
		"Annots", a(fields...)))
	pdf.AddPage(page)

	pdf.Root.Set("AcroForm", d(
		"Fields", a(fields...), "SigFlags", 3,
		"DA", text("/Helv 0 Tf 0 g"), "DR", d("Font", d("Helv", font))))

	// Deliberately no /Metadata.
	data, err := save(pdf, "1.7")
	if err != nil {
		return nil, err
	}

	// Patch each /ByteRange against where its own /Contents landed. A signature
	// cannot know its offset before the file is laid out, so this is the only
	// order that yields exact arithmetic.
	total := len(data)
	short := map[string]bool{}
	for _, s := range signedFields {
		short[s.label] = s.short
	}
	patchedCount := 0

	objRe := regexp.MustCompile(`(?s)\d+ 0 obj(.*?)endobj`)
	brRe := regexp.MustCompile(fmt.Sprintf(`/ByteRange \[ 0 %d %d %d \]`, placeholder, placeholder, placeholder))
	contentsRe := regexp.MustCompile(`/Contents <[0-9a-fA-F]*>`)
	nameRe := regexp.MustCompile(`/Name \(([^)]*)\)`)
	original := bytes.Clone(data)
	for _, obj := range objRe.FindAllSubmatchIndex(original, -1) {
		body := original[obj[2]:obj[3]]
		if !bytes.Contains(body, []byte("/Type /Sig")) {
			continue
		}
		base := obj[2]
		br := brRe.FindIndex(body)
		contents := contentsRe.FindIndex(body)
		name := nameRe.FindSubmatch(body)
		if br == nil || contents == nil || name == nil {
			return nil, fmt.Errorf("signature object %q is not in the expected shape", original[obj[0]:obj[0]+20])
		}
		start := base + contents[0] + len("/Contents ")
		end := base + contents[1]
		tail := total - end
		if short[string(name[1])] {
			tail -= shortfall
		}
		replacement := fmt.Sprintf("/ByteRange [ 0 %010d %010d %010d ]", start, end, tail)
		if len(replacement) != br[1]-br[0] {
			return nil, fmt.Errorf("patched /ByteRange changes width, which would move an xref offset")
		}
		copy(data[base+br[0]:base+br[1]], replacement)
		patchedCount++
	}

	if patchedCount != len(signedFields) {
		return nil, fmt.Errorf("patched %d signatures, expected %d", patchedCount, len(signedFields))
	}
	return data, nil
}
