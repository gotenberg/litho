package pdf

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// signedRangePattern matches the ByteRange litho writes, which pads each value
// to ten digits.
var signedRangePattern = regexp.MustCompile(`/ByteRange \[0 (\d{10}) (\d{10}) (\d{10})\]`)

// checkSignedOutput checks the signed output data against what ISO 32000-1,
// 12.8.1 asks of a signature, without trusting the signer's own bookkeeping:
// data opens as written, with no cross-reference repair and no bytes before its
// header, it holds exactly one signature, whose ByteRange covers the whole file
// but the /Contents hex string, and the digest the signer signed is the SHA-256
// of the bytes that ByteRange names, which litho's CMS verifier recomputes from
// the file.
func checkSignedOutput(t *testing.T, data []byte, signer *cmsTestSigner) {
	t.Helper()

	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Errorf("the output starts with %q, not at its header", data[:min(len(data), 16)])
	}

	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("reopening the signed output: %v", err)
	}
	defer r.Close()
	if r.doc.Rebuilt {
		t.Error("the signed output needed its cross-reference table rebuilt: the offsets it records are wrong")
	}
	if r.doc.HeaderOffset != 0 {
		t.Errorf("the signed output has %d bytes before its header", r.doc.HeaderOffset)
	}

	sigs, err := r.ExtractSignatures(ctx)
	if err != nil {
		t.Fatalf("ExtractSignatures: %v", err)
	}
	if len(sigs) != 1 {
		t.Fatalf("the output holds %d signatures, want 1", len(sigs))
	}
	br := sigs[0].ByteRange
	total := int64(len(data))
	if br[0] != 0 || br[1] <= 0 || br[2] <= br[1] || br[2]+br[3] != total {
		t.Fatalf("ByteRange %v does not cover the %d-byte file", br, total)
	}
	gap := string(data[br[1]:br[2]])
	if !regexp.MustCompile(`^<[0-9a-fA-F]+>$`).MatchString(gap) {
		t.Fatalf("the bytes ByteRange leaves out are not the /Contents hex string: %.40q", gap)
	}

	m := signedRangePattern.FindSubmatch(data)
	if m == nil {
		t.Fatal("no ByteRange in the output")
	}
	if string(m[1]) != fmt.Sprintf("%010d", br[1]) {
		t.Errorf("the ByteRange written in the file is %s, the reader reads %v", m[0], br)
	}

	pool := x509.NewCertPool()
	pool.AddCert(signer.cert)
	results, err := r.VerifySignatures(ctx, pool)
	if err != nil {
		t.Fatalf("VerifySignatures: %v", err)
	}
	if len(results) != 1 || !results[0].HashValid {
		t.Errorf("the signature does not verify against its ByteRange: %+v", results)
	}
}

// signedDigest returns the SHA-256 of the bytes the ByteRange written in data
// covers.
func signedDigest(t *testing.T, data []byte) []byte {
	t.Helper()

	m := signedRangePattern.FindSubmatch(data)
	if m == nil {
		t.Fatal("no ByteRange in the output")
	}
	var start2, len2, len1 int
	if _, err := fmt.Sscanf(string(m[1])+" "+string(m[2])+" "+string(m[3]), "%d %d %d", &len1, &start2, &len2); err != nil {
		t.Fatalf("parsing %s: %v", m[0], err)
	}
	h := sha256.New()
	h.Write(data[:len1])
	h.Write(data[start2 : start2+len2])
	return h.Sum(nil)
}

// TestWriterSignatureDigestCoversTheWrittenByteRange pins that the digest the
// signer signs is the one of the file it ends up in.
//
// The ByteRange array sits in the signature dictionary, inside the bytes the
// signature covers (ISO 32000-1, 12.8.1). The writer used to hash the file while
// the array still held its placeholder values and fill in the real offsets
// afterwards, so every signature it wrote failed verification: poppler's
// pdfsig reported a digest mismatch on a plain one-page document.
func TestWriterSignatureDigestCoversTheWrittenByteRange(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := writerTestPDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	signer := &mockSigner{}
	w := NewWriter(r)
	defer w.Close()
	w.Sign(signer)
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	if got := signedDigest(t, out.Bytes()); !bytes.Equal(got, signer.signedDigest) {
		t.Errorf("the signer signed %x, the file's ByteRange hashes to %x", signer.signedDigest, got)
	}
}

// damagedSignSource builds a one-page document with an exact cross-reference
// table, twenty bytes an entry (ISO 32000-1, 7.5.4), then damages it as kind
// says.
func damagedSignSource(t *testing.T, kind string) []byte {
	t.Helper()

	const content = "BT /F1 12 Tf 20 700 Td (page) Tj ET\n"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	build := func(prefix string) []byte {
		var buf bytes.Buffer
		buf.WriteString(prefix)
		buf.WriteString("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n")
		offs := make([]int, len(objs)+1)
		for i, o := range objs {
			offs[i+1] = buf.Len()
			fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
		}
		xref := buf.Len()
		fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
		for i := 1; i <= len(objs); i++ {
			fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
		}
		fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
		return buf.Bytes()
	}

	const junk = "HTTP/1.1 200 OK\r\n\r\n"
	switch kind {
	case "junk, offsets from the header":
		// What a file saved with the transport's header in front looks like:
		// every offset counts from %PDF-, as Acrobat reads them.
		return append([]byte(junk), build("")...)
	case "junk, offsets from the file start":
		return build(junk)
	case "a cross-reference table litho rebuilds":
		return bytes.Replace(build(""), []byte("xref\n0 6\n"), []byte("xref\n0 6\nGARBAGE"), 1)
	}
	t.Fatalf("unknown damage %q", kind)
	return nil
}

// TestWriterSignRewritesADamagedSource pins that signing a source litho had to
// repair on reading writes what the unsigned write does, a full rewrite, and
// signs that.
//
// A rebuilt cross-reference table means the offsets the file records are
// wrong, and bytes before the header leave readers disagreeing on what an
// offset counts from (ISO 32000-1, 7.5.2 puts the header first; Acrobat reads
// offsets from it, litho from the file start). The unsigned write rewrites
// such a file in full, but signing appended its update to the damaged bytes, so
// the signed file carried the source's broken offsets and qpdf had to
// reconstruct it.
func TestWriterSignRewritesADamagedSource(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{
		"junk, offsets from the header",
		"junk, offsets from the file start",
		"a cross-reference table litho rebuilds",
	} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			data := damagedSignSource(t, kind)
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			if !r.doc.Rebuilt && r.doc.HeaderOffset == 0 {
				t.Fatal("the source reads cleanly, so the test proves nothing")
			}

			signer := newCMSTestSigner(t)
			w := NewWriter(r)
			defer w.Close()
			w.Sign(signer)
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			checkSignedOutput(t, out.Bytes(), signer)
			if n := signer.calls.Load(); n != 1 {
				t.Errorf("the signer ran %d times, want once", n)
			}

			got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
			if err != nil {
				t.Fatalf("reopening the output: %v", err)
			}
			defer got.Close()
			text, err := got.ExtractText(ctx, 1)
			if err != nil {
				t.Fatalf("ExtractText: %v", err)
			}
			if !strings.Contains(text, "page") {
				t.Errorf("page 1 reads %q, want the source's text", text)
			}
		})
	}
}
