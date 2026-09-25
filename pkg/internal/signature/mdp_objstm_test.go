package signature

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/md5" //nolint:gosec // ISO 32000-1, 7.6.3.3 defines the standard security handler key in terms of MD5.
	"encoding/binary"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// startXRef returns the offset the last startxref of data points at.
func startXRef(t *testing.T, data []byte) int {
	t.Helper()
	marker := []byte("startxref\n")
	at := bytes.LastIndex(data, marker)
	if at < 0 {
		t.Fatal("data has no startxref")
	}
	var off int
	if _, err := fmt.Sscanf(string(data[at+len(marker):]), "%d", &off); err != nil {
		t.Fatalf("reading startxref: %v", err)
	}
	return off
}

// packedUpdate appends an incremental update (ISO 32000-1, 7.5.6) that writes
// objs as ordinary objects and packs packed into the object stream stmNum
// (7.5.7), indexed by a cross-reference stream (7.5.8). compress selects a
// FlateDecode object stream. seal, when set, encrypts the object stream's bytes
// under its own object number, as 7.5.7 requires in an encrypted file.
// trailerExtra is added to the cross-reference stream dictionary.
//
// # Corpus evidence
//
//   - pdfjs xfa_issue13855.pdf, xfa_issue14071.pdf: end with an update of
//     this layout, an encrypted FlateDecode object stream holding the
//     interactive form dictionary beside a cross-reference stream.
func packedUpdate(t *testing.T, base []byte, stmNum int, packed, objs []updatedObject, compress bool, seal func(objNum int, data []byte) []byte, trailerExtra string) []byte {
	t.Helper()

	type entry struct{ num, typ, f2, f3 int }
	var entries []entry
	var buf bytes.Buffer
	buf.Write(base)
	for _, o := range objs {
		entries = append(entries, entry{o.num, 1, buf.Len(), 0})
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", o.num, o.body)
	}

	var header, body strings.Builder
	for i, o := range packed {
		fmt.Fprintf(&header, "%d %d ", o.num, body.Len())
		body.WriteString(o.body + "\n")
		entries = append(entries, entry{o.num, 2, stmNum, i})
	}
	data := []byte(header.String() + body.String())
	filter := ""
	if compress {
		var z bytes.Buffer
		zw := zlib.NewWriter(&z)
		if _, err := zw.Write(data); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		data = z.Bytes()
		filter = "/Filter /FlateDecode "
	}
	if seal != nil {
		data = seal(stmNum, data)
	}
	entries = append(entries, entry{stmNum, 1, buf.Len(), 0})
	fmt.Fprintf(&buf, "%d 0 obj\n<< /Type /ObjStm /N %d /First %d %s/Length %d >>\nstream\n",
		stmNum, len(packed), header.Len(), filter, len(data))
	buf.Write(data)
	buf.WriteString("\nendstream\nendobj\n")

	xrefNum := stmNum
	for _, e := range entries {
		xrefNum = max(xrefNum, e.num)
	}
	xrefNum++
	xrefOff := buf.Len()
	entries = append(entries, entry{xrefNum, 1, xrefOff, 0})
	slices.SortFunc(entries, func(a, b entry) int { return a.num - b.num })

	var index strings.Builder
	var table []byte
	for _, e := range entries {
		fmt.Fprintf(&index, "%d 1 ", e.num)
		table = append(table, byte(e.typ))
		table = binary.BigEndian.AppendUint32(table, uint32(e.f2)) //nolint:gosec // test offsets fit
		table = binary.BigEndian.AppendUint16(table, uint16(e.f3)) //nolint:gosec // test indices fit
	}
	fmt.Fprintf(&buf, "%d 0 obj\n<< /Type /XRef /Size %d /Root 1 0 R /Prev %d /Index [%s] /W [1 4 2] %s/Length %d >>\nstream\n",
		xrefNum, xrefNum+1, startXRef(t, base), index.String(), trailerExtra, len(table))
	buf.Write(table)
	fmt.Fprintf(&buf, "\nendstream\nendobj\nstartxref\n%d\n%%%%EOF\n", xrefOff)
	return buf.Bytes()
}

// passwordPad is the 32-byte padding string of ISO 32000-1, Table 21.
var passwordPad = []byte{
	0x28, 0xBF, 0x4E, 0x5E, 0x4E, 0x75, 0x8A, 0x41, 0x64, 0x00, 0x4E, 0x56,
	0xFF, 0xFA, 0x01, 0x08, 0x2E, 0x2E, 0x00, 0xB6, 0xD0, 0x68, 0x3E, 0x80,
	0x2F, 0x0C, 0xA9, 0xFE, 0x64, 0x53, 0x69, 0x7A,
}

func rc4Cipher(key, data []byte) []byte {
	var s [256]byte
	for i := range s {
		s[i] = byte(i)
	}
	j := 0
	for i := range 256 {
		j = (j + int(s[i]) + int(key[i%len(key)])) % 256
		s[i], s[j] = s[j], s[i]
	}
	out := make([]byte, len(data))
	i, j := 0, 0
	for k, c := range data {
		i = (i + 1) % 256
		j = (j + int(s[i])) % 256
		s[i], s[j] = s[j], s[i]
		out[k] = c ^ s[(int(s[i])+int(s[j]))%256]
	}
	return out
}

// rc4Security returns a /V 1, /R 2 standard security handler with empty owner
// and user passwords (ISO 32000-1, 7.6.3, Algorithms 2 to 4), the trailer
// entries naming it as object encNum, and a function that encrypts an
// object's bytes under that object's key (7.6.2).
func rc4Security(encNum int) (encDict, trailer string, seal func(objNum int, data []byte) []byte) {
	const docID = "0123456789abcdef"
	ownerDigest := md5.Sum(passwordPad) //nolint:gosec // See the import note.
	owner := rc4Cipher(ownerDigest[:5], passwordPad)
	h := md5.New() //nolint:gosec // See the import note.
	h.Write(passwordPad)
	h.Write(owner)
	h.Write([]byte{0xFC, 0xFF, 0xFF, 0xFF}) // /P -4, little endian.
	h.Write([]byte(docID))
	fileKey := h.Sum(nil)[:5]
	user := rc4Cipher(fileKey, passwordPad)

	encDict = fmt.Sprintf("<< /Filter /Standard /V 1 /R 2 /P -4 /O <%x> /U <%x> >>", owner, user)
	trailer = fmt.Sprintf("/Encrypt %d 0 R /ID [<%x> <%x>] ", encNum, docID, docID)
	seal = func(objNum int, data []byte) []byte {
		k := md5.New() //nolint:gosec // See the import note.
		k.Write(fileKey)
		k.Write([]byte{byte(objNum), byte(objNum >> 8), byte(objNum >> 16), 0, 0})
		return rc4Cipher(k.Sum(nil)[:10], data)
	}
	return encDict, trailer, seal
}

// mdpVerdict evaluates the policy of a signature covering the first signedEnd
// bytes of data.
func mdpVerdict(t *testing.T, data []byte, signedEnd, policy int) models.SignatureValidation {
	t.Helper()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	sig := models.Signature{
		MDPPolicy: policy,
		ByteRange: [4]int64{0, 1, 1, int64(signedEnd) - 1},
	}
	var result models.SignatureValidation
	evaluateMDP(ctx, doc, sig, &result)
	return result
}

// certifiedFormPDF is a document certified at the given DocMDP policy whose
// form holds a text field (6) and the certification signature field (4, value
// 5).
func certifiedFormPDF(policy int) []byte {
	return buildSigPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 7 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 6 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (cert) /Rect [0 0 0 0] /P 3 0 R /V 5 0 R >>",
		fmt.Sprintf("<< /Type /Sig /ByteRange [0 1 1 1] /Contents <30> /Reference [<< /Type /SigRef /TransformMethod /DocMDP /TransformParams << /Type /TransformParams /P %d /V /1.2 >> >>] >>", policy),
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /Rect [20 20 120 40] /P 3 0 R >>",
		"<< /Fields [4 0 R 6 0 R] /SigFlags 3 >>",
	}, "")
}

// TestEvaluateMDP_AppendedObjectStream covers ISO 32000-1, 7.5.7: the objects
// an appended object stream holds are classified as they would be outside it,
// and the stream that holds them is not itself a change.
func TestEvaluateMDP_AppendedObjectStream(t *testing.T) {
	filledWidget := updatedObject{6, "<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /Rect [20 20 120 40] /P 3 0 R /V (Jane) >>"}
	textNote := updatedObject{8, "<< /Type /Annot /Subtype /Text /Rect [200 200 220 220] /P 3 0 R /Contents (note) >>"}
	page := updatedObject{3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 6 0 R 8 0 R] >>"}

	tests := []struct {
		name     string
		policy   int
		packed   []updatedObject
		compress bool
		want     bool
		reported string // what MDPInfo names when the verdict is a violation
	}{
		{"P=2 filled widget", 2, []updatedObject{filledWidget}, true, true, ""},
		{"P=2 filled widget, unfiltered stream", 2, []updatedObject{filledWidget}, false, true, ""},
		{"P=2 text annotation", 2, []updatedObject{textNote, page}, true, false, "object 8 (Annot:Text)"},
		{"P=3 text annotation", 3, []updatedObject{textNote, page}, true, true, ""},
		{"P=1 filled widget", 1, []updatedObject{filledWidget}, true, false, "object 6 (Annot:Widget)"},
		{"P=1 empty stream", 1, nil, true, true, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := certifiedFormPDF(tc.policy)
			data := packedUpdate(t, base, 9, tc.packed, nil, tc.compress, nil, "")
			result := mdpVerdict(t, data, len(base), tc.policy)
			if result.MDPValid != tc.want {
				t.Fatalf("MDPValid = %v, want %v (MDPInfo %q)", result.MDPValid, tc.want, result.MDPInfo)
			}
			if strings.Contains(result.MDPInfo, "ObjStm") || strings.Contains(result.MDPInfo, "object 9 ") {
				t.Errorf("MDPInfo %q reports the object stream instead of what it holds", result.MDPInfo)
			}
			if tc.reported != "" && !strings.Contains(result.MDPInfo, tc.reported) {
				t.Errorf("MDPInfo %q does not name %s", result.MDPInfo, tc.reported)
			}
		})
	}
}

// TestEvaluateMDP_UnreadableObjectStream pins that an appended object stream
// whose bytes do not decode is reported: the objects it holds are unknown, so
// the classifier cannot vouch for them.
func TestEvaluateMDP_UnreadableObjectStream(t *testing.T) {
	base := certifiedFormPDF(2)
	garbage := updatedObject{9, "<< /Type /ObjStm /N 1 /First 4 /Filter /FlateDecode /Length 8 >>\nstream\nnot zlib\nendstream"}
	data := appendUpdate(t, base, garbage)
	result := mdpVerdict(t, data, len(base), 2)
	if result.MDPValid || !strings.Contains(result.MDPInfo, "could not be read") {
		t.Errorf("MDPValid = %v, MDPInfo %q: want a violation naming the unread object stream", result.MDPValid, result.MDPInfo)
	}
}

// TestEvaluateMDP_EncryptedXFAFormFill reproduces the update that fills in an
// encrypted, certified XFA form: the catalog gains a usage rights signature
// (ISO 32000-1, 12.8.2.3), the XFA datasets packet holding the form data is
// rewritten, and the interactive form dictionary is packed into an object
// stream encrypted under its own key (7.5.7).
//
// The object stream is read, so the form dictionary inside it passes. The
// datasets stream passes too, although its producer types it /EmbeddedFile:
// the /XFA array names it as the datasets packet, which holds form data.
//
// # Corpus evidence
//
//   - pdfjs xfa_issue13855.pdf, xfa_issue14071.pdf: have this shape, the
//     datasets stream typed /EmbeddedFile and the object stream holding only
//     the interactive form dictionary.
func TestEvaluateMDP_EncryptedXFAFormFill(t *testing.T) {
	encDict, trailer, seal := rc4Security(11)
	base := buildSigPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 7 0 R /Perms << /DocMDP 5 0 R >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (cert) /Rect [0 0 0 0] /P 3 0 R /V 5 0 R >>",
		"<< /Type /Sig /ByteRange [0 1 1 1] /Contents <30> /Reference [<< /Type /SigRef /TransformMethod /DocMDP /TransformParams << /Type /TransformParams /P 2 /V /1.2 >> >>] >>",
		"<< /Type /EmbeddedFile /Length 4 >>\nstream\n<a/>\nendstream",
		// An encrypted file encrypts its strings under their object's key
		// (ISO 32000-1, 7.6.2), so the packet name is written sealed.
		fmt.Sprintf("<< /Fields [4 0 R] /SigFlags 3 /XFA [<%x> 6 0 R] >>", seal(7, []byte("datasets"))),
		"null", "null", "null",
		encDict,
	}, trailer)

	catalog := updatedObject{1, "<< /Type /Catalog /Pages 2 0 R /AcroForm 7 0 R /Perms << /DocMDP 5 0 R /UR3 12 0 R >> >>"}
	rights := updatedObject{12, "<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /adbe.pkcs7.detached /Contents <30> /ByteRange [0 1 1 1] /Reference [<< /Type /SigRef /TransformMethod /UR3 /TransformParams << /Type /TransformParams /V /2.2 /Form [/FillIn] >> >>] >>"}
	datasets := updatedObject{6, "<< /Type /EmbeddedFile /Length 11 >>\nstream\n<a>Jane</a>\nendstream"}
	form := updatedObject{7, "<< /Fields [4 0 R] /SigFlags 3 /NeedAppearances false /XFA [(datasets) 6 0 R] >>"}

	t.Run("form dictionary in the object stream passes", func(t *testing.T) {
		data := packedUpdate(t, base, 13, []updatedObject{form}, []updatedObject{catalog, rights}, true, seal, trailer)
		result := mdpVerdict(t, data, len(base), 2)
		if !result.MDPValid {
			t.Errorf("MDPValid = false (MDPInfo %q), want the packed form dictionary read and passed", result.MDPInfo)
		}
	})

	t.Run("datasets typed EmbeddedFile passes as form data", func(t *testing.T) {
		data := packedUpdate(t, base, 13, []updatedObject{form}, []updatedObject{catalog, datasets, rights}, true, seal, trailer)
		result := mdpVerdict(t, data, len(base), 2)
		if !result.MDPValid {
			t.Errorf("MDPValid = false (MDPInfo %q), want the datasets stream, object 6, passed as form data", result.MDPInfo)
		}
	})
}

// TestEvaluateMDP_RewrittenAfterSigning covers a file rewritten after signing,
// so the signature's /ByteRange ends inside the rewritten revision and the
// rest of that revision reads as appended. Its unfiltered object stream holds
// the signature's own widget, which passes, and the fonts and pages written
// after the signed bytes are reported.
//
// # Corpus evidence
//
//   - pdfjs xfa_filled_imm1344e.pdf: has this shape, its /ByteRange ending
//     at byte 481146 of a 2542069-byte first revision whose streams, object
//     streams included, carry no filter.
func TestEvaluateMDP_RewrittenAfterSigning(t *testing.T) {
	stream := func(body string) string {
		return fmt.Sprintf("<< /Type /ObjStm /N 1 /First 4 /Length %d >>\nstream\n9 0 %s\nendstream", len(body)+4, body)
	}
	widget := "<< /Type /Annot /Subtype /Widget /FT /Sig /T (cert) /Rect [0 0 0 0] /P 3 0 R /V 5 0 R >>"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [9 0 R] /SigFlags 3 >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [9 0 R] >>",
		"null",
		"<< /Type /Sig /ByteRange [0 1 1 1] /Contents <30> /Reference [<< /Type /SigRef /TransformMethod /DocMDP /TransformParams << /Type /TransformParams /P 2 /V /1.2 >> >>] >>",
		stream(widget),
	}

	t.Run("widget in the object stream passes", func(t *testing.T) {
		data := buildSigPDF(objs, "")
		signedEnd := bytes.Index(data, []byte("6 0 obj"))
		result := mdpVerdict(t, data, signedEnd, 2)
		if !result.MDPValid {
			t.Errorf("MDPValid = false (MDPInfo %q), want the packed widget read and passed", result.MDPInfo)
		}
	})

	t.Run("font written after the signed bytes fails", func(t *testing.T) {
		data := buildSigPDF(append(slices.Clone(objs), "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"), "")
		signedEnd := bytes.Index(data, []byte("6 0 obj"))
		result := mdpVerdict(t, data, signedEnd, 2)
		if result.MDPValid || !strings.Contains(result.MDPInfo, "object 7 (Font)") {
			t.Errorf("MDPValid = %v, MDPInfo %q: want the font, object 7, reported", result.MDPValid, result.MDPInfo)
		}
	})
}
