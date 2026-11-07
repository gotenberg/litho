package office

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"testing"
)

type zipEntry struct {
	name   string
	data   []byte
	method uint16
}

func buildZip(t testing.TB, entries ...zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: e.name, Method: e.method})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(e.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPackage_Read(t *testing.T) {
	big := bytes.Repeat([]byte("<w:p/>"), 10000)
	data := buildZip(t,
		zipEntry{"[Content_Types].xml", []byte("<Types/>"), zip.Deflate},
		zipEntry{"word/document.xml", big, zip.Deflate},
		zipEntry{"encrypted", []byte("raw"), zip.Store},
	)
	p, err := OpenPackage(data)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if got := p.Names(); len(got) != 3 || got[1] != "word/document.xml" {
		t.Errorf("names %v", got)
	}
	if !p.Has("encrypted") || p.Has("Encrypted") {
		t.Error("Has is not exact")
	}
	for _, tc := range []struct {
		name    string
		fold    bool
		max     int64
		want    []byte
		wantErr error
	}{
		{"word/document.xml", false, 0, big, nil},
		{"WORD/Document.xml", true, 0, big, nil},
		{"WORD/Document.xml", false, 0, nil, ErrMalformed},
		{"missing", true, 0, nil, ErrMalformed},
		{"word/document.xml", false, 100, nil, ErrInputTooLarge},
	} {
		got, err := p.Read(ctx, tc.name, tc.fold, tc.max)
		if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && !bytes.Equal(got, tc.want)) {
			t.Errorf("Read(%q, fold %v, max %d): %d bytes, %v", tc.name, tc.fold, tc.max, len(got), err)
		}
	}
	if got, err := p.ReadStored("encrypted"); err != nil || string(got) != "raw" {
		t.Errorf("ReadStored: %q, %v", got, err)
	}
	if _, err := p.ReadStored("word/document.xml"); !errors.Is(err, ErrMalformed) {
		t.Errorf("ReadStored of a deflated entry: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := p.Read(cancelled, "word/document.xml", false, 0); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled Read: %v", err)
	}
}

func TestPackage_LyingSize(t *testing.T) {
	data := buildZip(t, zipEntry{"a.xml", bytes.Repeat([]byte("a"), 5000), zip.Deflate})
	// Shrink the declared uncompressed size in the central directory past
	// the size cap check: the entry still never inflates past its
	// declaration.
	i := bytes.LastIndex(data, []byte("PK\x01\x02"))
	copy(data[i+24:], []byte{10, 0, 0, 0})
	p, err := OpenPackage(data)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := p.Read(context.Background(), "a.xml", false, 100); !errors.Is(err, ErrMalformed) {
		t.Fatalf("got %d bytes, %v, want ErrMalformed", len(got), err)
	}
}

func TestOpenPackage_Refusals(t *testing.T) {
	if _, err := OpenPackage([]byte("not a zip")); !errors.Is(err, ErrMalformed) {
		t.Errorf("not a zip: %v", err)
	}
	dup := buildZip(t, zipEntry{"a", nil, zip.Store}, zipEntry{"a", nil, zip.Store})
	if _, err := OpenPackage(dup); !errors.Is(err, ErrMalformed) {
		t.Errorf("duplicate entry: %v", err)
	}
}

func FuzzOpenPackage(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := OpenPackage(data)
		if err != nil {
			return
		}
		for _, name := range p.Names() {
			if _, err := p.Read(context.Background(), name, true, 1<<20); err != nil {
				continue
			}
			if _, err := p.ReadStored(name); err != nil {
				continue
			}
		}
	})
}
