package pdf

import (
	"bytes"
	"context"
	"testing"
)

// TestWriterRedactBlanksAnImageAStructureElementNamesInPlace pins that a
// structure element's object reference to an image (ISO 32000-1, 14.7.4.3)
// is no place the image paints: the image its one placement covers is
// blanked in place and keeps its /StructParent, so the element and its
// parent tree entry name the blanked image. Counted as a use, the reference
// blanked the image in a copy and left its samples, intact, under the
// element.
func TestWriterRedactBlanksAnImageAStructureElementNamesInPlace(t *testing.T) {
	t.Parallel()
	tree := "/StructTreeRoot 32 0 R /MarkInfo << /Marked true >> "
	objects := blankEntriesObjects +
		"32 0 obj\n<< /Type /StructTreeRoot /K 33 0 R /ParentTree << /Nums [0 33 0 R] >> /ParentTreeNextKey 1 >>\nendobj\n" +
		"33 0 obj\n<< /Type /StructElem /S /Figure /P 32 0 R /Alt (A square) /K << /Type /OBJR /Obj 7 0 R >> >>\nendobj\n"
	src := imageDoc([]string{placeLeft}, tree, "", objects)
	src = bytes.Replace(src, []byte("/ColorSpace /DeviceGray /BitsPerComponent 8 "),
		[]byte("/ColorSpace /DeviceGray /BitsPerComponent 8 /StructParent 0 "), 1)

	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	report, err := w.Redact(ctx, []RedactRegion{{Page: 1, Rect: topLeft}})
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}
	if len(report.ImagesBlankedInCopy) != 0 {
		t.Errorf("ImagesBlankedInCopy = %v, want the image blanked in place", report.ImagesBlankedInCopy)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if got := imageSamples(t, out.Bytes(), 1); got[0] != 0 || got[15] != 0x80 {
		t.Errorf("samples = %v, want the top rows blanked and the rest kept", got)
	}
	if _, ok := pageXObject(t, out.Bytes(), 1, "Im1")["StructParent"]; !ok {
		t.Error("the image blanked in place lost its /StructParent")
	}
}
