package build

import (
	"crypto/md5" //nolint:gosec // ISO 32000-1's standard security handler is built on MD5; no security role.
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"slices"
)

func init() { register("encperm_ua1", encpermUa1) }

// encpermUa1 builds encperm_ua1.pdf: an encrypted document that forbids accessible extraction.
//
// | Construct                                          | Rule       |
// | -------------------------------------------------- | ---------- |
// | `/P` with bit 10 clear in an encrypted document    | `7.16/1`   |
//
// ISO 14289-1, 7.16 test 1 reads `P != null && (P & 512) == 512`, so it judges the
// permissions an encrypted document grants: bit 10 is "extract for accessibility",
// and a document withholding it denies assistive technology the text.
//
// This is the one fixture written byte by byte rather than through pikepdf. qpdf
// never clears bit 10, whatever `pikepdf.Permissions(accessibility=False)` asks
// for: measured at revision 4 and revision 6, `/P` comes out -1028 either way, and
// `extract=False` clears bit 5 instead. The construct is therefore unreachable
// through pikepdf's encryption, so the standard security handler is written here
// directly (ISO 32000-1, Algorithms 2, 3 and 5) with V=4, R=4 and a V2 crypt
// filter.
//
// Writing it by hand buys back determinism the qpdf path cannot offer: qpdf
// refuses `deterministic_id` together with encryption, and `trailer_2b.pdf`
// documents what that costs. Here the `/ID` is fixed, so the whole file, keys
// included, is reproducible byte for byte.
//
// The metadata stream is encrypted like every other stream. Leaving it in the
// clear made the packet unreadable to BOTH engines, which cost the file its title
// and its `pdfuaid:part` and buried the permissions rule under three unrelated
// ones. That was measured, not assumed.
//
// The document is otherwise conformant apart from its unembedded font, so at
// PDF/UA-1 exactly two rules answer: the permissions and `7.21.4.1`. A checker
// reading the permissions wrongly shows up immediately rather than behind a
// neighbour.
//
// See ../FIXTURES.md for the authoritative expected firing set.
func encpermUa1() ([]byte, error) {
	// The 32-byte padding string of ISO 32000-1, 7.6.3.3.
	pad := []byte{
		0x28, 0xBF, 0x4E, 0x5E, 0x4E, 0x75, 0x8A, 0x41, 0x64, 0x00, 0x4E, 0x56,
		0xFF, 0xFA, 0x01, 0x08, 0x2E, 0x2E, 0x00, 0xB6, 0xD0, 0x68, 0x3E, 0x80,
		0x2F, 0x0C, 0xA9, 0xFE, 0x64, 0x53, 0x69, 0x7A,
	}

	// A fixed file identifier keeps every derived key, and so the whole file,
	// byte for byte reproducible.
	docID, err := hex.DecodeString("6c6974686f636f727075736964656e74")
	if err != nil {
		return nil, err
	}

	const keyLength = 16

	// -3904 grants every permission except bit 10 (512), extract for accessibility.
	const permissions = -3904

	// rc4 is RC4, the cipher the V2 crypt filter names.
	rc4 := func(key, data []byte) []byte {
		state := make([]int, 256)
		for i := range state {
			state[i] = i
		}
		j := 0
		for i := range 256 {
			j = (j + state[i] + int(key[i%len(key)])) & 0xFF
			state[i], state[j] = state[j], state[i]
		}
		out := make([]byte, 0, len(data))
		i := 0
		j = 0
		for _, b := range data {
			i = (i + 1) & 0xFF
			j = (j + state[i]) & 0xFF
			state[i], state[j] = state[j], state[i]
			out = append(out, b^byte(state[(state[i]+state[j])&0xFF])) //nolint:gosec // state values are bytes.
		}
		return out
	}

	padPassword := func(password []byte) []byte {
		return slices.Concat(password, pad)[:32]
	}

	md5Of := func(parts ...[]byte) []byte {
		sum := md5.Sum(slices.Concat(parts...)) //nolint:gosec // see the import.
		return sum[:]
	}

	xorKey := func(key []byte, i int) []byte {
		out := make([]byte, len(key))
		for k, b := range key {
			out[k] = b ^ byte(i) //nolint:gosec // i < 20.
		}
		return out
	}

	// ownerValue is ISO 32000-1, Algorithm 3.
	ownerValue := func(ownerPassword, userPassword []byte) []byte {
		pw := ownerPassword
		if len(pw) == 0 {
			pw = userPassword
		}
		digest := md5Of(padPassword(pw))
		for range 50 {
			digest = md5Of(digest)
		}
		key := digest[:keyLength]
		out := rc4(key, padPassword(userPassword))
		for i := 1; i < 20; i++ {
			out = rc4(xorKey(key, i), out)
		}
		return out
	}

	// fileKey is ISO 32000-1, Algorithm 2.
	fileKey := func(userPassword, oValue []byte, permissions int32) []byte {
		out := md5Of(
			padPassword(userPassword),
			oValue,
			binary.LittleEndian.AppendUint32(nil, uint32(permissions)), //nolint:gosec // struct's "<i" on purpose.
			docID,
		)
		for range 50 {
			out = md5Of(out[:keyLength])
		}
		return out[:keyLength]
	}

	// userValue is ISO 32000-1, Algorithm 5, for revision 3 and above.
	userValue := func(key []byte) []byte {
		out := rc4(key, md5Of(pad, docID))
		for i := 1; i < 20; i++ {
			out = rc4(xorKey(key, i), out)
		}
		return append(out, make([]byte, 16)...)
	}

	// objectKey is the per-object key of ISO 32000-1, Algorithm 1, at
	// generation zero.
	objectKey := func(key []byte, number int) []byte {
		digest := md5Of(key, []byte{byte(number & 0xFF), byte((number >> 8) & 0xFF), byte((number >> 16) & 0xFF)}, []byte{0, 0})
		return digest[:min(len(key)+5, 16)]
	}

	xmp := []byte("<?xpacket begin=\"\xef\xbb\xbf\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>\n" +
		"<x:xmpmeta xmlns:x=\"adobe:ns:meta/\">\n" +
		" <rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\">\n" +
		"  <rdf:Description rdf:about=\"\"\n" +
		"    xmlns:dc=\"http://purl.org/dc/elements/1.1/\"\n" +
		"    xmlns:pdfuaid=\"http://www.aiim.org/pdfua/ns/id/\">\n" +
		"   <dc:title><rdf:Alt><rdf:li xml:lang=\"x-default\">encrypted permissions</rdf:li></rdf:Alt></dc:title>\n" +
		"   <pdfuaid:part>1</pdfuaid:part>\n" +
		"  </rdf:Description>\n" +
		" </rdf:RDF>\n" +
		"</x:xmpmeta>\n" +
		"<?xpacket end=\"w\"?>")

	content := []byte("/P <</MCID 0>> BDC\nBT /F1 12 Tf 50 700 Td (encrypted permissions) Tj ET\nEMC\n")

	build := func(permissions int32) []byte {
		oValue := ownerValue([]byte("owner"), []byte(""))
		key := fileKey([]byte(""), oValue, permissions)
		uValue := userValue(key)

		encrypt := func(number int, data []byte) []byte {
			return rc4(objectKey(key, number), data)
		}

		// hexStr is a hex string, so an encrypted byte never needs escaping.
		hexStr := func(number int, raw []byte) string {
			return "<" + hex.EncodeToString(encrypt(number, raw)) + ">"
		}

		stream := func(number int, data []byte, extra string) string {
			body := encrypt(number, data)
			return "<< " + extra + fmt.Sprintf("/Length %d >>\nstream\n", len(body)) + string(body) + "\nendstream"
		}

		objects := map[int]string{
			1: "<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 5 0 R " +
				"/MarkInfo << /Marked true >> /Lang " + hexStr(1, []byte("en-US")) +
				" /ViewerPreferences << /DisplayDocTitle true >> /Metadata 9 0 R >>",
			2: "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			3: "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
				"/Resources << /Font << /F1 8 0 R >> >> /Contents 4 0 R " +
				"/StructParents 0 /Tabs /S >>",
			4: stream(4, content, ""),
			5: "<< /Type /StructTreeRoot /K [6 0 R] /ParentTree 7 0 R >>",
			6: "<< /Type /StructElem /S /Document /P 5 0 R /K [10 0 R] /Lang " +
				hexStr(6, []byte("en-US")) + " >>",
			7: "<< /Nums [0 [10 0 R]] >>",
			8: "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica " +
				"/Encoding /WinAnsiEncoding >>",
			9:  stream(9, xmp, "/Type /Metadata /Subtype /XML "),
			10: "<< /Type /StructElem /S /P /P 6 0 R /Pg 3 0 R /K 0 >>",
			11: "<< /Filter /Standard /V 4 /R 4 /Length 128 " +
				"/CF << /StdCF << /CFM /V2 /AuthEvent /DocOpen /Length 16 >> >> " +
				"/StmF /StdCF /StrF /StdCF " +
				"/O <" + hex.EncodeToString(oValue) + "> " +
				"/U <" + hex.EncodeToString(uValue) + "> " +
				fmt.Sprintf("/P %d >>", permissions),
		}

		out := []byte("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
		offsets := map[int]int{}
		for _, number := range sortedKeys(objects) {
			offsets[number] = len(out)
			out = fmt.Appendf(out, "%d 0 obj\n", number)
			out = append(out, objects[number]...)
			out = append(out, "\nendobj\n"...)
		}

		startxref := len(out)
		// Each entry is exactly 20 bytes, its two-byte end-of-line marker
		// included (ISO 32000-1, 7.5.4).
		out = fmt.Appendf(out, "xref\n0 %d\n0000000000 65535 f\r\n", len(objects)+1)
		for _, number := range sortedKeys(objects) {
			out = fmt.Appendf(out, "%010d 00000 n\r\n", offsets[number])
		}
		out = fmt.Appendf(out, "trailer\n<< /Size %d /Root 1 0 R /Encrypt 11 0 R /ID [<%s> <%s>] >>\n"+
			"startxref\n%d\n%%%%EOF\n",
			len(objects)+1, hex.EncodeToString(docID), hex.EncodeToString(docID), startxref)

		return out
	}

	return build(permissions), nil
}
