package pdf

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

// translucentCyanPDF builds a 100 by 100 page filled with DeviceCMYK cyan at
// half opacity, which FlattenTransparency composites and RenderPage draws
// through the profile DeviceCMYK is read in.
func translucentCyanPDF() []byte {
	const body = "/GS0 gs 1 0 0 0 k 0 0 100 100 re f"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] " +
			"/Resources << /ExtGState << /GS0 << /ca 0.5 >> >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
	})
}

// hybridXFAFormPDF builds a hybrid form: one widget field and an /AcroForm
// /XFA array, the shape ISO 32000-1, 12.7.8 describes.
func hybridXFAFormPDF() []byte {
	const xfa = `<xdp:xdp xmlns:xdp="http://ns.adobe.com/xdp/"><config/></xdp:xdp>`
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 6 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (John) /Rect [100 700 300 720] >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(xfa), xfa),
		"<< /Fields [4 0 R] /XFA [(datasets) 5 0 R] >>",
	})
}

// helveticaPagePDF builds one Letter page drawing content in Helvetica as /F1,
// with catalogExtra appended to the catalog and contents as the body of the
// object /Contents names.
func helveticaPagePDF(catalogExtra, contents string) []byte {
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R " + catalogExtra + ">>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
			"/Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		contents,
	})
}

// streamBody is the body of an unfiltered stream object holding content.
func streamBody(content string) string {
	return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content)
}

// encryptBytes returns data encrypted under AES-256 with the user password pw
// and every permission granted.
func encryptBytes(t *testing.T, data []byte, pw string) []byte {
	t.Helper()
	ctx := context.Background()
	r := openColorPDF(t, data)
	w := NewWriter(r)
	defer w.Close()
	w.Encrypt(EncryptConfig{UserPassword: pw, OwnerPassword: pw + "-owner", Permissions: -1})
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("encrypting the fixture: %v", err)
	}
	return out.Bytes()
}

// opensWithoutAPassword reports whether data opens with no password and
// reads its page count, which is what a reader experiences.
func opensWithoutAPassword(t *testing.T, data []byte) bool {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return false
	}
	defer r.Close()
	_, err = r.PageCount(ctx)
	return err == nil
}

// renderCentre renders page 1 of data at 72 dpi and returns the 8-bit RGB
// levels of the pixel at (50, 50).
func renderCentre(t *testing.T, data []byte, opts ...OpenOption) [3]uint32 {
	t.Helper()
	img := renderRGBA(t, openColorPDF(t, data, opts...), 72)
	r, g, b, _ := img.At(50, 50).RGBA()
	return [3]uint32{r >> 8, g >> 8, b >> 8}
}

// writeAll writes w with opts and returns the bytes.
func writeAll(t *testing.T, w *Writer, opts ...WriteOption) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := w.WriteTo(context.Background(), &out, opts...); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return out.Bytes()
}

// runPipeline writes p with opts and returns the bytes.
func runPipeline(t *testing.T, p *Pipeline, opts ...WriteOption) []byte {
	t.Helper()
	var out bytes.Buffer
	if _, err := p.WriteTo(context.Background(), &out, opts...); err != nil {
		t.Fatalf("Pipeline.WriteTo: %v", err)
	}
	return out.Bytes()
}
