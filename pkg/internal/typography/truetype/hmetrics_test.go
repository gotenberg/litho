package truetype

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func TestReadHMetrics(t *testing.T) {
	t.Parallel()
	hhea := make([]byte, hheaSize)
	binary.BigEndian.PutUint16(hhea[hheaNumberOfHMetrics:], 2)
	// Two long metrics, advances 600 and 500, then one left side bearing.
	hmtx := []byte{0x02, 0x58, 0, 0, 0x01, 0xF4, 0, 0, 0, 0}
	m, err := ReadHMetrics(buildTableDirectory(map[string][]byte{"hhea": hhea, "hmtx": hmtx}))
	if err != nil {
		t.Fatal(err)
	}
	for gid, want := range map[int]float64{0: 600, 1: 500, 2: 500, 9: 500, -1: 0} {
		if got := m.Advance(gid); got != want {
			t.Errorf("Advance(%d) = %v, want %v", gid, got, want)
		}
	}
	// hhea states more long metrics than hmtx holds.
	binary.BigEndian.PutUint16(hhea[hheaNumberOfHMetrics:], 9)
	m, err = ReadHMetrics(buildTableDirectory(map[string][]byte{"hhea": hhea, "hmtx": hmtx[:4]}))
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Advance(5); got != 600 {
		t.Errorf("short hmtx: Advance(5) = %v, want 600", got)
	}
	if _, err := ReadHMetrics(buildTableDirectory(map[string][]byte{"hhea": hhea[:10], "hmtx": hmtx})); !errors.Is(err, ErrInvalidFont) {
		t.Errorf("short hhea: err = %v, want ErrInvalidFont", err)
	}
	if _, err := ReadHMetrics(buildTableDirectory(map[string][]byte{"hhea": hhea})); !errors.Is(err, ErrTableNotFound) {
		t.Errorf("no hmtx: err = %v, want ErrTableNotFound", err)
	}
}

func TestCFF2Table(t *testing.T) {
	t.Parallel()
	table := []byte{2, 0, 5, 0, 0}
	data := buildTableDirectory(map[string][]byte{"CFF2": table})
	got, err := CFF2Table(data)
	if err != nil || !bytes.Equal(got, table) {
		t.Errorf("CFF2Table = %v, %v, want the table", got, err)
	}
	if _, err := CFFTable(data); !errors.Is(err, ErrTableNotFound) {
		t.Errorf("CFFTable of a CFF2 program: err = %v, want ErrTableNotFound", err)
	}
}
