package compliance

import (
	"encoding/binary"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// testOTTO returns an "OTTO" program holding the tables of the TrueType sfnt
// beside a 'CFF ' table holding cff, the shape a CFF font reads as OpenType.
// The search fields of the directory and the table checksums are left at
// zero, which no reader the tests exercise checks.
func testOTTO(t *testing.T, sfnt, cff []byte) []byte {
	t.Helper()
	records, err := truetype.ParseTableDirectory(sfnt)
	if err != nil {
		t.Fatalf("reading the sfnt directory: %v", err)
	}
	tables := map[string][]byte{"CFF ": cff}
	for _, r := range records {
		data, err := truetype.TableData(sfnt, records, r.Tag)
		if err != nil {
			t.Fatalf("reading table %q: %v", r.Tag, err)
		}
		tables[r.Tag] = data
	}
	tags := make([]string, 0, len(tables))
	for tag := range tables {
		tags = append(tags, tag)
	}
	slices.Sort(tags)
	out := []byte("OTTO")
	out = binary.BigEndian.AppendUint16(out, uint16(len(tags))) //nolint:gosec // G115: a test program holds a handful of tables.
	out = append(out, make([]byte, 6)...)
	offset := 12 + 16*len(tags)
	var body []byte
	for _, tag := range tags {
		data := tables[tag]
		out = append(out, tag...)
		out = binary.BigEndian.AppendUint32(out, 0)
		out = binary.BigEndian.AppendUint32(out, uint32(offset+len(body))) //nolint:gosec // G115: a test program is far below 4 GiB.
		out = binary.BigEndian.AppendUint32(out, uint32(len(data)))        //nolint:gosec // G115: a test table is far below 4 GiB.
		body = append(body, data...)
		body = append(body, make([]byte, -len(data)&3)...)
	}
	return append(out, body...)
}

// TestOpenTypeProgramReadByTheFont pins that a /FontFile3 /OpenType program is
// read as the font that embeds it reads it, under ISO 19005-2, 6.2.11.4.1 t1:
// through its 'CFF ' table for a Type 1 or CIDFontType0 font, through its
// 'hhea' and 'hmtx' tables for a TrueType or CIDFontType2 font. The litho
// fixture fontcff2_2b.pdf plants an "OTTO" with 'CFF2' outlines under each
// (F1 to F4), a TrueType-outline sfnt under the CFF fonts (M1, M2), and an
// "OTTO" with 'CFF ' outlines and no metrics under the TrueType fonts (M3,
// M4). veraPDF 1.30.2 reports F1, F2 and M1 to M4 as not embedded, and fails
// F5, the TrueType reading of the CFF2 program under /Widths 500, for its
// widths alone.
func TestOpenTypeProgramReadByTheFont(t *testing.T) {
	t.Parallel()
	unembedded := []string{"F1", "F2", "M1", "M2", "M3", "M4"}
	for _, tt := range []struct {
		profile string
		rule    Rule
	}{
		{"PDF/A-2b", Rule_19005_2_6_2_11_4_1_t01},
		{"PDF/A-4", Rule_19005_4_6_2_10_4_1_t01},
		{"PDF/UA-1", Rule_14289_1_7_21_4_1_t01},
		{"PDF/UA-2", Rule_14289_2_8_4_5_5_1_t01},
	} {
		t.Run(tt.profile, func(t *testing.T) {
			t.Parallel()
			if got := fixtureFontsFailing(t, "fontcff2_2b.pdf", tt.profile, tt.rule); !slices.Equal(got, unembedded) {
				t.Errorf("fonts not embedded = %v, want %v", got, unembedded)
			}
		})
	}
	t.Run("width", func(t *testing.T) {
		t.Parallel()
		want := []string{"F5", "F5"}
		if got := fixtureFontsFailing(t, "fontcff2_2b.pdf", "PDF/A-2b", Rule_19005_2_6_2_11_5_t01); !slices.Equal(got, want) {
			t.Errorf("width rule fired for %v, want %v", got, want)
		}
	})
}
