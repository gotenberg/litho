package mutations

import (
	"archive/zip"
	"bytes"
	"context"
	"testing"
)

// FuzzScanContainer feeds attacker-controlled attachment bytes to the
// container reader of the output scan, which opens zip archives, gzip members
// and PDFs it finds in them. It must never panic, whatever the bytes claim.
func FuzzScanContainer(f *testing.F) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	member, err := zw.Create("notes.txt")
	if err != nil {
		f.Fatal(err)
	}
	if _, err := member.Write(nestedText(nestedWord)); err != nil {
		f.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		f.Fatal(err)
	}
	zipped := buf.Bytes()
	f.Add(zipped)
	f.Add(zipped[:len(zipped)/2])
	f.Add([]byte("%PDF-1.7\n1 0 obj\n<< /Type /Catalog >>\nendobj\n"))
	f.Add([]byte{0x1f, 0x8b, 0x08, 0x00})
	f.Fuzz(func(t *testing.T, data []byte) {
		doc := copiesDoc(t, copiesObjects(""), nil, "")
		w := newScanWalk(doc, nil, []string{nestedWord})
		w.scanBlob(context.Background(), 6, nil, data)
		if w.err != nil {
			t.Fatalf("scan error: %v", w.err)
		}
	})
}
