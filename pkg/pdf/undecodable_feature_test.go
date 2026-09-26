package pdf

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	goimage "image"
	"image/jpeg"
	"slices"
	"testing"
)

// arithmeticJPEG returns an 8x8 grey JPEG from the standard library's
// encoder with its frame marker turned into SOF9, a sequential frame coded
// arithmetically (ITU-T T.81, Table B.1), which neither DCTDecode decoder
// reads.
func arithmeticJPEG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, goimage.NewGray(goimage.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	sof := bytes.Index(data, []byte{0xFF, 0xC0})
	if sof < 0 {
		t.Fatal("the encoder wrote no SOF0 marker")
	}
	data[sof+1] = 0xC9
	return data
}

// unsupportedFeaturePDF builds a one-page document showing five 8x8 images,
// each encoded with a feature its built-in decoder lacks, and a sixth that
// decodes: /Jpx carries a JPEG 2000 codestream whose Rsiz signals ISO/IEC
// 15444-2, /Jbig2 a JBIG2 extension segment flagged necessary, /Fax a CCITT
// row in uncompressed mode, /Crypt a Crypt filter naming a crypt filter the
// document does not apply, /Jpeg an arithmetic-coded JPEG, and /Plain
// uncompressed samples. It returns the object number of each image by name.
func unsupportedFeaturePDF(t *testing.T) ([]byte, map[string]int) {
	jpx := []byte{
		0xFF, 0x4F, 0xFF, 0x51, 0x00, 0x29, 0x80, 0x00,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x09,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x09,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x01, 0x07, 0x01, 0x01,
	}
	jbig2 := binary.BigEndian.AppendUint32(nil, 0)
	jbig2 = append(jbig2, 48, 0, 1)
	jbig2 = binary.BigEndian.AppendUint32(jbig2, 19)
	jbig2 = append(jbig2, 0, 0, 0, 8, 0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	jbig2 = binary.BigEndian.AppendUint32(jbig2, 1)
	jbig2 = append(jbig2, 62, 0, 1)
	jbig2 = binary.BigEndian.AppendUint32(jbig2, 4)
	jbig2 = append(jbig2, 0xA0, 0, 0, 1)

	jpegData := arithmeticJPEG(t)

	image := func(entries string, data []byte) string {
		return fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 8 /Height 8 /ColorSpace /DeviceGray"+
			" /BitsPerComponent 1 %s /Length %d >>\nstream\n%s\nendstream", entries, len(data), data)
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /XObject" +
			" << /Jpx 4 0 R /Jbig2 5 0 R /Fax 6 0 R /Crypt 7 0 R /Plain 8 0 R /Jpeg 9 0 R >> >> >>",
		image("/Filter /JPXDecode", jpx),
		image("/Filter /JBIG2Decode", jbig2),
		image("/Filter /CCITTFaxDecode /DecodeParms << /K -1 /Columns 8 /Rows 8 >>", []byte{0x03, 0xC0, 0, 0, 0, 0, 0, 0}),
		image("/Filter /Crypt /DecodeParms << /Name /OtherCF >>", []byte("still encrypted")),
		image("", bytes.Repeat([]byte{0xAA}, 8)),
		fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width 8 /Height 8 /ColorSpace /DeviceGray"+
			" /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n%s\nendstream", len(jpegData), jpegData),
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
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
	return buf.Bytes(), map[string]int{"Jpx": 4, "Jbig2": 5, "Fax": 6, "Crypt": 7, "Plain": 8, "Jpeg": 9}
}

// TestUnsupportedFeature_DiagnosticsAgreeWithImageDecoding pins, through the
// public API, that an image whose built-in decoder refuses a feature it lacks
// is one thing everywhere: [Reader.UndecodableStreams] lists its stream, and
// [Reader.DecodeImage] and the Decode of its [Reader.ExtractImages] entry
// return [ErrImageNotDecodable] instead of pixels read from the encoded bytes.
// The image that decodes is neither listed nor refused.
func TestUnsupportedFeature_DiagnosticsAgreeWithImageDecoding(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	data, objects := unsupportedFeaturePDF(t)
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Logf("closing reader: %v", err)
		}
	})

	diags, err := r.UndecodableStreams(ctx)
	if err != nil {
		t.Fatalf("UndecodableStreams: %v", err)
	}
	listed := map[int][]string{}
	for _, d := range diags {
		listed[d.ObjectNum] = d.Filters
	}
	wantFilter := map[string]string{"Jpx": "JPXDecode", "Jbig2": "JBIG2Decode", "Fax": "CCITTFaxDecode", "Crypt": "Crypt", "Jpeg": "DCTDecode"}

	metas, err := r.ExtractImages(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractImages: %v", err)
	}
	decode := map[string]func(context.Context) error{}
	for _, m := range metas {
		decode[m.Filename] = func(c context.Context) error {
			_, err := m.Decode(c)
			return err
		}
	}

	for name, num := range objects {
		_, direct := r.DecodeImage(ctx, 1, name)
		if decode[name] == nil {
			t.Fatalf("ExtractImages does not list %s", name)
		}
		listedErr := decode[name](ctx)
		if name == "Plain" {
			if len(listed[num]) != 0 || direct != nil || listedErr != nil {
				t.Errorf("Plain: listed %v, DecodeImage %v, Decode %v; want it decodable", listed[num], direct, listedErr)
			}
			continue
		}
		if !slices.Equal(listed[num], []string{wantFilter[name]}) {
			t.Errorf("%s: UndecodableStreams lists object %d as %v, want [%s]", name, num, listed[num], wantFilter[name])
		}
		if !errors.Is(direct, ErrImageNotDecodable) || !errors.Is(listedErr, ErrImageNotDecodable) {
			t.Errorf("%s: DecodeImage %v, Decode %v; want %v", name, direct, listedErr, ErrImageNotDecodable)
		}
	}
	if len(diags) != len(wantFilter) {
		t.Errorf("UndecodableStreams = %+v, want the %d refused images alone", diags, len(wantFilter))
	}
}
