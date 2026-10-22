package mutations

import (
	"bytes"
	"compress/zlib"
	"context"
	"io"
	"os"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// inflatePending returns the decompressed data of a FlateDecode stream the
// allocator holds.
func inflatePending(t *testing.T, s ast.PdfStream) []byte {
	t.Helper()
	raw, err := s.Bytes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zlib.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// redactDataMaskImage draws pkg/pdf's JPEG 2000 file (96 x 64, transparent
// left of column 24 and opaque from column 72) over a 96 by 64 page under the
// image entries given, redacts region, and returns the blanked replacement
// of the image and the allocator holding it.
func redactDataMaskImage(t *testing.T, file, entries string, region [4]float64) (ast.PdfStream, *writer.ObjAllocator) {
	t.Helper()
	ctx := context.Background()
	jp2, err := os.ReadFile("../../pdf/testdata/jpx/" + file)
	if err != nil {
		t.Fatal(err)
	}
	body := "q 96 0 0 64 0 0 cm /Im1 Do Q"
	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"5 0 obj\n<< /XObject << /Im1 7 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 96 64] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"7 0 obj\n<< /Type /XObject /Subtype /Image /Width 96 /Height 64 /Filter /JPXDecode " + entries +
		" /Length " + strconv.Itoa(len(jp2)) + " >>\nstream\n" + string(jp2) + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 8 /Root 1 0 R >>\n%%EOF\n")
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}
	alloc := writer.NewObjAllocator(20)
	if _, err := RedactPageRegions(ctx, alloc, doc, pages[0], [][4]float64{region}); err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	replaced, ok := alloc.Pending()[7].(ast.PdfStream)
	if !ok {
		t.Fatalf("object 7 = %T, want the blanked image", alloc.Pending()[7])
	}
	if _, kept := replaced.Dict["SMaskInData"]; kept {
		t.Error("the FlateDecode replacement keeps /SMaskInData")
	}
	return replaced, alloc
}

// TestRedactPageRegions_KeepsTheSoftMaskOfJPXData covers a JPXDecode picture
// carrying its soft mask in its data (ISO 32000-1, Table 89 /SMaskInData).
// The blanked replacement is no JPXDecode image, so the data's opacity becomes
// an explicit /SMask, its covered samples opaque as for a blanked /SMask, and
// premultiplied colour is written straight. Dropped, the picture painted
// opaque around the region. The region covers columns 0 to 9.
func TestRedactPageRegions_KeepsTheSoftMaskOfJPXData(t *testing.T) {
	cases := []struct{ name, file, entries string }{
		{"straight, colour of the data", "rgba8.jp2", "/SMaskInData 1"},
		{"premultiplied, own colour space", "rgba8_premul.jp2", "/SMaskInData 2 /ColorSpace /DeviceRGB /BitsPerComponent 8"},
		{"premultiplied, colour of the data", "rgba8_premul.jp2", "/SMaskInData 2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			replaced, alloc := redactDataMaskImage(t, tc.file, tc.entries, [4]float64{0, 0, 10, 64})
			ref, ok := replaced.Dict["SMask"].(ast.PdfIndirectRef)
			if !ok {
				t.Fatalf("SMask = %v, want the data's opacity as an explicit mask", replaced.Dict["SMask"])
			}
			mask := inflatePending(t, alloc.Pending()[ref.ObjectNum].(ast.PdfStream))
			if len(mask) != 96*64 {
				t.Fatalf("mask of %d samples, want %d", len(mask), 96*64)
			}
			for _, c := range []struct{ x, want int }{{5, 255}, {20, 0}, {30, (30 - 24) * 255 / 47}, {80, 255}} {
				if got := int(mask[5*96+c.x]); got != c.want {
					t.Errorf("mask at column %d = %d, want %d", c.x, got, c.want)
				}
			}
			samples := inflatePending(t, replaced)
			if len(samples) != 96*64*3 {
				t.Fatalf("%d colour samples, want %d", len(samples), 96*64*3)
			}
			// Pixel (80, 5) is opaque: its straight colour is 80*255/95,
			// 5*255/63, 85*37 mod 256 whichever way the data held it.
			px := samples[(5*96+80)*3:]
			for i, want := range []int{214, 20, 73} {
				if d := int(px[i]) - want; d < -3 || d > 3 {
					t.Errorf("channel %d at (80, 5) = %d, want %d", i, px[i], want)
				}
			}
		})
	}
}

// TestRedactPageRegions_DropsAnOpaqueSoftMaskOfJPXData pins that a region
// covering every sample of such a picture writes no /SMask: blanking made the
// whole mask opaque, which paints as no mask does (ISO 32000-1, 11.6.5.3),
// and an /SMask would add the ISO 19005-1, 6.4 test 2 violation the source,
// whose /SMaskInData that clause does not name, lacks.
func TestRedactPageRegions_DropsAnOpaqueSoftMaskOfJPXData(t *testing.T) {
	for _, entries := range []string{"/SMaskInData 1", "/SMaskInData 1 /ColorSpace /DeviceRGB /BitsPerComponent 8"} {
		replaced, _ := redactDataMaskImage(t, "rgba8.jp2", entries, [4]float64{-10, -10, 200, 200})
		if sm, present := replaced.Dict["SMask"]; present {
			t.Errorf("%s: SMask = %v, want none over a wholly blanked picture", entries, sm)
		}
	}
}
