package compliance

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/ascii85"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// j2kWithComponents builds a raw J2K codestream whose SIZ marker declares n
// colour channels. ISO 19005-2, 6.2.8.3 t1 allows only 1, 3 or 4, so any other
// count is a violation the checks must report.
//
// The segment is laid out at its full length (Csiz at offset 38, then three
// bytes per component) rather than the truncated form the fuzz seeds use,
// because this codestream is meant to parse rather than to probe the walker.
func j2kWithComponents(n uint16) []byte {
	siz := make([]byte, 40+3*int(n))
	binary.BigEndian.PutUint16(siz[0:], 0xFF51) // SIZ marker
	binary.BigEndian.PutUint16(siz[2:], uint16(len(siz)-2))
	binary.BigEndian.PutUint32(siz[6:], 1)  // Xsiz
	binary.BigEndian.PutUint32(siz[10:], 1) // Ysiz
	binary.BigEndian.PutUint16(siz[38:], n) // Csiz
	for i := 0; i < int(n); i++ {
		siz[40+3*i] = 7 // Ssiz: 8-bit unsigned
		siz[41+3*i] = 1 // XRsiz
		siz[42+3*i] = 1 // YRsiz
	}
	return append([]byte{0xFF, 0x4F}, siz...) // SOC
}

// jpxImageDoc builds a one-page document whose object 4 is a JPEG2000 image
// XObject carrying data behind filterEntry, and returns that stream.
func jpxImageDoc(t *testing.T, filterEntry string, data []byte) (*document.Document, ast.PdfStream) {
	t.Helper()

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 10 10] /Resources << /XObject << /Im0 4 0 R >> >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Filter %s /Length %d >>\nstream\n%s\nendstream",
			filterEntry, len(data), data),
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)

	raw := buf.Bytes()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(raw), int64(len(raw)), nil)
	if err != nil {
		t.Fatalf("building the document: %v", err)
	}
	stream, err := document.ResolveStream(context.Background(), doc, ast.PdfIndirectRef{ObjectNum: 4})
	if err != nil {
		t.Fatalf("resolving the image stream: %v", err)
	}
	return doc, stream
}

// flateEncoded compresses data the way a PDF /FlateDecode stream stores it.
func flateEncoded(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		t.Fatalf("compressing the codestream: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing the compressor: %v", err)
	}
	return buf.Bytes()
}

// ascii85Encoded encodes data the way a PDF /ASCII85Decode stream stores it,
// including the ~> end-of-data marker.
func ascii85Encoded(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := ascii85.NewEncoder(&buf)
	if _, err := enc.Write(data); err != nil {
		t.Fatalf("encoding the codestream: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("closing the encoder: %v", err)
	}
	buf.WriteString("~>")
	return buf.Bytes()
}

// TestCheckJPXImageBehindOuterFilter pins that a JPEG2000 violation is
// reported whatever precedes JPXDecode in the filter chain.
//
// The checks read what JPXDecode consumes, and the raw stream bytes are that
// input only when JPXDecode is the whole chain. An image behind
// [/FlateDecode /JPXDecode] therefore reached them compressed, matched neither
// the JP2 signature nor the raw J2K marker, and every violation went
// unreported: pdfjs issue12579.pdf writes exactly that chain and
// issue11004.pdf writes [/ASCII85Decode /JPXDecode]. Measured against the
// veraPDF test suite fixture for 6.2.8.3 t1, re-wrapping its image in
// /FlateDecode turned a reported 5-channel violation into "compliant".
func TestCheckJPXImageBehindOuterFilter(t *testing.T) {
	t.Parallel()

	codestream := j2kWithComponents(5)

	for _, tc := range []struct {
		name   string
		filter string
		data   []byte
	}{
		{
			name:   "JPXDecode alone",
			filter: "/JPXDecode",
			data:   codestream,
		},
		{
			name:   "behind FlateDecode",
			filter: "[/FlateDecode /JPXDecode]",
			data:   flateEncoded(t, codestream),
		},
		{
			// The comment above names this chain as issue11004.pdf's and the
			// table did not carry it, so the case the file documents was the
			// one case left unexercised. Measured when added: it already
			// passed, so this is protection rather than a fix.
			name:   "behind ASCII85Decode",
			filter: "[/ASCII85Decode /JPXDecode]",
			data:   ascii85Encoded(t, codestream),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc, stream := jpxImageDoc(t, tc.filter, tc.data)
			got := checkJPXImage(context.Background(), doc, stream, "Im0", 1, 2)
			if !hasRule(got, Rule_19005_2_6_2_8_3_t01) {
				t.Fatalf("want an ISO 19005-2 6.2.8.3 t1 violation for a 5-channel codestream, got %v", got)
			}
		})
	}
}
