package offcrypto

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

func TestReadCFB_Streams(t *testing.T) {
	rnd := testRand(7)
	small, big, empty := rnd(100), rnd(9000), []byte{}
	for _, major := range []uint16{3, 4} {
		doc := buildCFB(t, major, []cfbStream{{"Small", small}, {"Big", big}, {"Empty", empty}})
		f, err := readCFB(context.Background(), doc)
		if err != nil {
			t.Fatalf("version %d: %v", major, err)
		}
		for _, tc := range []struct {
			name string
			want []byte
		}{{"small", small}, {"BIG", big}, {"Empty", empty}} {
			got, ok, err := f.stream(tc.name)
			if err != nil || !ok || !bytes.Equal(got, tc.want) {
				t.Errorf("version %d, stream %s: %d bytes, %v, %v", major, tc.name, len(got), ok, err)
			}
		}
		if _, ok, _ := f.stream("Missing"); ok {
			t.Errorf("version %d: a missing stream was found", major)
		}
	}
}

func TestReadCFB_TruncatedLastSector(t *testing.T) {
	big := testRand(8)(5000)
	doc := buildCFB(t, 3, []cfbStream{{"Big", big}})
	// buildCFB puts the FAT last; move nothing, drop the zero tail of the
	// last FAT sector the way some writers end a file early.
	f, err := readCFB(context.Background(), doc[:len(doc)-100])
	if err != nil {
		t.Fatal(err)
	}
	if got, _, err := f.stream("Big"); err != nil || !bytes.Equal(got, big) {
		t.Fatalf("stream after a short last sector: %v", err)
	}
}

func TestReadCFB_DIFAT(t *testing.T) {
	data := testRand(9)(5000)
	doc := buildCFB(t, 3, []cfbStream{{"Big", data}})
	le := binary.LittleEndian
	// Point all 109 header DIFAT slots and one DIFAT sector at the single FAT
	// sector: 110 FAT sectors, read through the DIFAT chain (MS-CFB, 2.5).
	// Free sectors pad the file to more sectors than FAT sectors.
	doc = append(doc, make([]byte, 110*512)...)
	fatSector := le.Uint32(doc[0x4C:])
	difat := make([]byte, 512)
	for i := range 127 {
		le.PutUint32(difat[4*i:], cfbFreeSect)
	}
	le.PutUint32(difat[0:], fatSector)
	le.PutUint32(difat[4*127:], cfbEndOfChain)
	difatSector := uint32((len(doc) - 512) / 512)
	doc = append(doc, difat...)
	for i := range cfbHeaderDIFAT {
		le.PutUint32(doc[0x4C+4*i:], fatSector)
	}
	le.PutUint32(doc[0x2C:], 110)
	le.PutUint32(doc[0x44:], difatSector)
	le.PutUint32(doc[0x48:], 1)
	f, err := readCFB(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.fat) != 110*128 {
		t.Errorf("FAT of %d entries, want %d", len(f.fat), 110*128)
	}
	if got, _, err := f.stream("Big"); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("stream through the DIFAT: %v", err)
	}
	le.PutUint32(doc[0x48:], 0)
	if _, err := readCFB(context.Background(), doc); !errors.Is(err, office.ErrMalformed) {
		t.Errorf("DIFAT count short of the FAT: %v, want ErrMalformed", err)
	}
}

func TestReadCFB_Malformed(t *testing.T) {
	le := binary.LittleEndian
	base := buildCFB(t, 3, []cfbStream{{"Big", testRand(10)(5000)}, {"Small", []byte("abc")}})
	f, err := readCFB(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	big, _ := f.rootChild("Big")
	fatOff := (int(le.Uint32(base[0x4C:])) + 1) * 512
	dirOff := (int(le.Uint32(base[0x30:])) + 1) * 512
	edit := func(fn func(b []byte)) []byte {
		b := bytes.Clone(base)
		fn(b)
		return b
	}
	for _, tc := range []struct {
		name   string
		doc    []byte
		stream string
	}{
		{"short", base[:100], ""},
		{"byte order", edit(func(b []byte) { le.PutUint16(b[0x1C:], 0xFEFF) }), ""},
		{"sector shift", edit(func(b []byte) { le.PutUint16(b[0x1E:], 10) }), ""},
		{"mini shift", edit(func(b []byte) { le.PutUint16(b[0x20:], 7) }), ""},
		{"cutoff", edit(func(b []byte) { le.PutUint32(b[0x38:], 8192) }), ""},
		{"FAT count", edit(func(b []byte) { le.PutUint32(b[0x2C:], 1<<20) }), ""},
		{"FAT sector", edit(func(b []byte) { le.PutUint32(b[0x4C:], 1<<20) }), ""},
		{"header only", base[:512], ""},
		{"directory sector", edit(func(b []byte) { le.PutUint32(b[0x30:], 1<<20) }), ""},
		{"root type", edit(func(b []byte) { b[dirOff+0x42] = cfbTypeStream }), ""},
		{"FAT cycle", edit(func(b []byte) { le.PutUint32(b[fatOff+4*int(big.start):], big.start) }), "Big"},
		{"FAT out of range", edit(func(b []byte) { le.PutUint32(b[fatOff+4*int(big.start):], 0xFFFFFF00) }), "Big"},
		{"short chain", edit(func(b []byte) { le.PutUint32(b[fatOff+4*int(big.start):], cfbEndOfChain) }), "Big"},
		{"stream size", edit(func(b []byte) { le.PutUint32(b[dirOff+cfbDirEntryBytes+0x78:], 1<<30) }), "Big"},
		{"mini sector", edit(func(b []byte) { le.PutUint32(b[dirOff+2*cfbDirEntryBytes+0x74:], 1000) }), "Small"},
		{"mini stream size", edit(func(b []byte) { le.PutUint64(b[dirOff+0x78:], 1<<20) }), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, err := readCFB(context.Background(), tc.doc)
			if err == nil && tc.stream != "" {
				_, _, err = f.stream(tc.stream)
			}
			if !errors.Is(err, office.ErrMalformed) {
				t.Fatalf("got %v, want ErrMalformed", err)
			}
		})
	}
}

func TestReadCFB_DirectoryCycle(t *testing.T) {
	le := binary.LittleEndian
	doc := buildCFB(t, 3, []cfbStream{{"A", []byte("a")}, {"B", []byte("b")}})
	dirOff := (int(le.Uint32(doc[0x30:])) + 1) * 512
	// B's right sibling points back at A: the walk visits each entry once.
	le.PutUint32(doc[dirOff+2*cfbDirEntryBytes+0x48:], 1)
	f, err := readCFB(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := f.stream("Missing"); ok {
		t.Fatal("found a missing stream")
	}
	if got, ok, err := f.stream("B"); !ok || err != nil || string(got) != "b" {
		t.Fatalf("stream B in a looping tree: %q %v %v", got, ok, err)
	}
}

func TestReadCFB_Version3IgnoresHighSize(t *testing.T) {
	le := binary.LittleEndian
	doc := buildCFB(t, 3, []cfbStream{{"A", []byte("abc")}})
	dirOff := (int(le.Uint32(doc[0x30:])) + 1) * 512
	le.PutUint32(doc[dirOff+cfbDirEntryBytes+0x7C:], 0xDEADBEEF)
	f, err := readCFB(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if got, _, err := f.stream("A"); err != nil || string(got) != "abc" {
		t.Fatalf("got %q, %v", got, err)
	}
}
