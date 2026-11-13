package varfont

import (
	"context"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// fuzzTable returns the table tagged tag of testdata/varfont.ttf, the
// valid seed of the fuzz targets, whose truncated half is the other seed.
func fuzzTable(f *testing.F, tag string) []byte {
	f.Helper()
	font, err := os.ReadFile("testdata/varfont.ttf")
	if err != nil {
		f.Fatal(err)
	}
	if tag == "" {
		return font
	}
	records, err := truetype.ParseTableDirectory(font)
	if err != nil {
		f.Fatal(err)
	}
	data, err := truetype.TableData(font, records, tag)
	if err != nil {
		f.Fatal(err)
	}
	return data
}

// seed adds data and its first half to f.
func seed(f *testing.F, data []byte) {
	f.Helper()
	f.Add(data)
	f.Add(data[:len(data)/2])
}

func FuzzParseFvar(f *testing.F) {
	seed(f, fuzzTable(f, "fvar"))
	f.Fuzz(func(t *testing.T, data []byte) {
		a, err := ParseFvar(data)
		if err != nil {
			return
		}
		Normalize(a, nil, []float64{-1e9, 1e9, 400})
		for i := range a.Axes {
			a.Clamp(i, 1e300)
		}
	})
}

func FuzzParseAvar(f *testing.F) {
	seed(f, fuzzTable(f, "avar"))
	axes, err := ParseFvar(fuzzTable(f, "fvar"))
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		avar, err := ParseAvar(data, len(axes.Axes))
		if err != nil {
			return
		}
		for _, v := range []float64{-1e9, 100, 250, 400, 650, 899.99, 1e9} {
			for _, n := range Normalize(axes, avar, []float64{v, v / 8, v / 9}) {
				if n < -16384 || n > 16384 {
					t.Fatalf("normalized coordinate %d out of [-1, 1]", n)
				}
			}
		}
	})
}

func FuzzGvarDeltas(f *testing.F) {
	seed(f, fuzzTable(f, "gvar"))
	coords := []int16{9000, -5000, -8000, 4000, 16384}
	f.Fuzz(func(t *testing.T, data []byte) {
		g, err := parseGvar(data, len(coords), 8)
		if err != nil {
			return
		}
		orig := make([]point, 24)
		for i := range orig {
			orig[i] = point{float64(i * 37 % 11), float64(i * 13 % 7)}
		}
		work := 0
		for gid := range 8 {
			if _, _, err := g.glyphDeltas(gid, coords, orig, []int{4, 9, 19}, &work); err != nil {
				continue
			}
			if _, _, err := g.glyphDeltas(gid, coords, orig[:6], nil, &work); err != nil {
				continue
			}
		}
	})
}

func FuzzAdvanceVariations(f *testing.F) {
	seed(f, fuzzTable(f, "HVAR"))
	coords := []int16{12000, -16384, -3000, 16384, 3000}
	f.Fuzz(func(t *testing.T, data []byte) {
		v, err := parseAdvanceVariations(data, coords, false)
		if err != nil {
			return
		}
		for gid := range 300 {
			v.delta(gid)
		}
	})
}

func FuzzMVAR(f *testing.F) {
	seed(f, fuzzTable(f, "MVAR"))
	coords := []int16{-16384, 8000, -16384, 2000, 16384}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = parseMVAR(data, coords) //nolint:errcheck // the target checks only that malformed data neither panics nor hangs
	})
}

func FuzzRenamePostScript(f *testing.F) {
	seed(f, fuzzTable(f, "name"))
	f.Fuzz(func(t *testing.T, data []byte) {
		out, err := renamePostScript(data, "Fuzz_400wght")
		if err != nil {
			return
		}
		if got := nameString(out, namePostScript); nameString(data, namePostScript) != "" && got != "Fuzz_400wght" {
			t.Fatalf("renamed PostScript name reads %q", got)
		}
	})
}

func FuzzInstance(f *testing.F) {
	seed(f, fuzzTable(f, ""))
	f.Fuzz(func(t *testing.T, data []byte) {
		font, err := Parse(data)
		if err != nil {
			return
		}
		user := font.Axes().Defaults()
		for i := range user {
			user[i] = font.Axes().Axes[i].Max
		}
		_, _ = font.Instance(context.Background(), user) //nolint:errcheck // the target checks only that malformed data neither panics nor hangs
	})
}

// FuzzInstanceCFF2 instances arbitrary bytes as an OpenType program with
// CFF2 outlines, seeded with testdata/varfont_cff2.otf.
func FuzzInstanceCFF2(f *testing.F) {
	font, err := os.ReadFile("testdata/varfont_cff2.otf")
	if err != nil {
		f.Fatal(err)
	}
	seed(f, font)
	f.Fuzz(func(t *testing.T, data []byte) {
		vf, err := Parse(data)
		if err != nil {
			return
		}
		user := vf.Axes().Defaults()
		for i := range user {
			user[i] = vf.Axes().Axes[i].Max
		}
		_, _ = vf.Instance(context.Background(), user) //nolint:errcheck // the target checks only that malformed data neither panics nor hangs
	})
}

// FuzzDefaultCFF converts arbitrary bytes as a CFF2 program at its
// default instance, seeded with testdata/varfont_cff2.otf and the same
// program without its variation tables, a static CFF2 font.
func FuzzDefaultCFF(f *testing.F) {
	font, err := os.ReadFile("testdata/varfont_cff2.otf")
	if err != nil {
		f.Fatal(err)
	}
	seed(f, font)
	records, err := truetype.ParseTableDirectory(font)
	if err != nil {
		f.Fatal(err)
	}
	tables := map[string][]byte{}
	var tags []string
	for _, r := range records {
		if r.Tag == "fvar" || r.Tag == "avar" || r.Tag == "HVAR" || r.Tag == "MVAR" || r.Tag == "STAT" {
			continue
		}
		if tables[r.Tag], err = truetype.TableData(font, records, r.Tag); err != nil {
			f.Fatal(err)
		}
		tags = append(tags, r.Tag)
	}
	seed(f, truetype.AssembleTables(cffSfntVersion, tags, tables))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = DefaultCFF(context.Background(), data) //nolint:errcheck // the target checks only that malformed data neither panics nor hangs
	})
}
