package varfont

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// golden is one location of outlines_golden.json: fontTools' instance of
// testdata/varfont.ttf there.
type golden struct {
	Location   map[string]float64 `json:"location"`
	Normalized []int16            `json:"normalized"`
	Glyphs     []struct {
		Name     string       `json:"name"`
		Points   [][2]float64 `json:"points"`
		Advance  int          `json:"advance"`
		LSB      int          `json:"lsb"`
		Box      [4]int       `json:"box"`
		VAdvance int          `json:"vadvance"`
	} `json:"glyphs"`
	Tables map[string]map[string]int `json:"tables"`
}

func readGoldens(t *testing.T) ([]byte, []golden) {
	t.Helper()
	font, err := os.ReadFile("testdata/varfont.ttf")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/outlines_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g []golden
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatal(err)
	}
	return font, g
}

// userCoords returns the location of g as user coordinates in the axis
// order of a.
func userCoords(a *Axes, g golden) []float64 {
	out := make([]float64, len(a.Axes))
	for i, ax := range a.Axes {
		out[i] = g.Location[ax.Tag]
	}
	return out
}

// staticGlyphs reads back an instance: each glyph's resolved points, its
// horizontal metrics, its box and its vertical advance.
func staticGlyphs(t *testing.T, program []byte) (pts [][]point, h []glyphMetrics, boxes [][4]int16, v []glyphMetrics) {
	t.Helper()
	records, err := truetype.ParseTableDirectory(program)
	if err != nil {
		t.Fatal(err)
	}
	table := func(tag string) []byte {
		d, err := truetype.TableData(program, records, tag)
		if err != nil {
			t.Fatalf("%s: %v", tag, err)
		}
		return d
	}
	for _, tag := range droppedTables {
		if _, err := truetype.TableData(program, records, tag); err == nil {
			t.Errorf("the instance keeps %q", tag)
		}
	}
	head, maxp, hhea, vhea := table("head"), table("maxp"), table("hhea"), table("vhea")
	in := &instancer{n: int(binary.BigEndian.Uint16(maxp[maxpNumGlyphsAt:])), glyf: table("glyf"), memo: map[int][]point{}}
	if in.loca, err = readLoca(table("loca"), len(in.glyf), in.n, binary.BigEndian.Uint16(head[headIndexToLocAt:]) != 0); err != nil {
		t.Fatal(err)
	}
	if h, err = readMetrics(table("hmtx"), int(binary.BigEndian.Uint16(hhea[hheaNumberOfMetricsAt:])), in.n); err != nil {
		t.Fatal(err)
	}
	if v, err = readMetrics(table("vmtx"), int(binary.BigEndian.Uint16(vhea[hheaNumberOfMetricsAt:])), in.n); err != nil {
		t.Fatal(err)
	}
	in.out = make([][]byte, in.n)
	in.comps = make([][]component, in.n)
	in.offsets = make([][2][]int32, in.n)
	boxes = make([][4]int16, in.n)
	for gid := range in.n {
		g := in.glyf[in.loca[gid]:in.loca[gid+1]]
		in.out[gid] = g
		if len(g) == 0 {
			continue
		}
		for i := range boxes[gid] {
			boxes[gid][i] = i16(g, 2+2*i)
		}
		if i16(g, 0) < 0 {
			comps, err := parseComposite(g)
			if err != nil {
				t.Fatal(err)
			}
			in.comps[gid] = comps
			ox, oy := make([]int32, len(comps)), make([]int32, len(comps))
			for i, c := range comps {
				ox[i], oy[i] = c.dx, c.dy
			}
			in.offsets[gid] = [2][]int32{ox, oy}
			continue
		}
		sg, err := parseSimple(g, int(i16(g, 0)))
		if err != nil {
			t.Fatal(err)
		}
		in.memo[gid] = sg.pts
	}
	for gid := range in.n {
		p, err := in.points(gid, 0)
		if err != nil {
			t.Fatal(err)
		}
		pts = append(pts, p)
	}
	return pts, h, boxes, v
}

// TestInstanceAgainstFontTools pins the instancer to fontTools' instancer
// on testdata/varfont.ttf, a font varLib built from masters with an 'avar'
// map, an intermediate master, sparse point sets, composites, 'HVAR',
// 'VVAR' and 'MVAR'. Outline points and advances match exactly, and
// normalized coordinates within one 2.14 unit (see nearInt16). A composite's box and left side bearing may differ by one
// unit: fontTools bounds a composite by its unrounded component offsets,
// litho by the rounded ones the glyph stores and a reader draws.
func TestInstanceAgainstFontTools(t *testing.T) {
	t.Parallel()
	font, goldens := readGoldens(t)
	f, err := Parse(font)
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range goldens {
		user := userCoords(f.Axes(), g)
		if got := f.Normalize(user); !nearInt16(got, g.Normalized) {
			t.Errorf("%v: Normalize = %v, want %v", g.Location, got, g.Normalized)
		}
		program, err := f.Instance(context.Background(), user)
		if err != nil {
			t.Fatalf("%v: %v", g.Location, err)
		}
		pts, h, boxes, v := staticGlyphs(t, program)
		if len(pts) != len(g.Glyphs) {
			t.Fatalf("%v: %d glyphs, want %d", g.Location, len(pts), len(g.Glyphs))
		}
		for gid, want := range g.Glyphs {
			if len(pts[gid]) != len(want.Points) {
				t.Errorf("%v %s: %d points, want %d", g.Location, want.Name, len(pts[gid]), len(want.Points))
				continue
			}
			for i, p := range pts[gid] {
				if math.Abs(p.x-want.Points[i][0]) > 1e-9 || math.Abs(p.y-want.Points[i][1]) > 1e-9 {
					t.Errorf("%v %s: point %d = %v, want %v", g.Location, want.Name, i, p, want.Points[i])
					break
				}
			}
			if int(h[gid].advance) != want.Advance || int(v[gid].advance) != want.VAdvance {
				t.Errorf("%v %s: advances %d, %d, want %d, %d", g.Location, want.Name, h[gid].advance, v[gid].advance, want.Advance, want.VAdvance)
			}
			slack := 0
			if isComposite(t, program, gid) {
				slack = 1
			}
			if d := abs(int(h[gid].bearing) - want.LSB); d > slack {
				t.Errorf("%v %s: lsb %d, want %d", g.Location, want.Name, h[gid].bearing, want.LSB)
			}
			for i := range boxes[gid] {
				if d := abs(int(boxes[gid][i]) - want.Box[i]); d > slack {
					t.Errorf("%v %s: box %v, want %v", g.Location, want.Name, boxes[gid], want.Box)
					break
				}
			}
		}
		records, _ := truetype.ParseTableDirectory(program)
		for tag, fields := range g.Tables {
			data, err := truetype.TableData(program, records, tag)
			if err != nil {
				t.Fatal(err)
			}
			for field, want := range fields {
				at, ok := goldenFields[tag+"."+field]
				if !ok {
					t.Fatalf("no offset for %s.%s", tag, field)
				}
				got := int(i16(data, at.at))
				if at.unsigned {
					got = int(binary.BigEndian.Uint16(data[at.at:]))
				}
				if d := abs(got - want); d > at.slack {
					t.Errorf("%v: %s.%s = %d, want %d", g.Location, tag, field, got, want)
				}
			}
		}
	}
}

// goldenFields locates the font-wide values outlines_golden.json records.
// The head box may differ by one unit, as a composite's box may.
var goldenFields = map[string]struct {
	at       int
	unsigned bool
	slack    int
}{
	"OS/2.sxHeight": {86, false, 0}, "OS/2.sCapHeight": {88, false, 0},
	"OS/2.sTypoAscender": {68, false, 0}, "OS/2.sTypoDescender": {70, false, 0},
	"OS/2.usWeightClass": {os2WeightClassAt, true, 0}, "OS/2.usWidthClass": {os2WidthClassAt, true, 0},
	"hhea.ascent": {hheaAscenderAt, false, 0}, "hhea.descent": {hheaDescenderAt, false, 0},
	"hhea.advanceWidthMax": {hheaAdvanceMaxAt, true, 0},
	"head.xMin":            {headBoxAt, false, 1}, "head.yMin": {headBoxAt + 2, false, 1},
	"head.xMax": {headBoxAt + 4, false, 1}, "head.yMax": {headBoxAt + 6, false, 1},
}

// isComposite reports whether glyph gid of program is a composite.
func isComposite(t *testing.T, program []byte, gid int) bool {
	t.Helper()
	records, _ := truetype.ParseTableDirectory(program)
	glyf, _ := truetype.TableData(program, records, "glyf")
	loca, _ := truetype.TableData(program, records, "loca")
	start := binary.BigEndian.Uint32(loca[4*gid:])
	end := binary.BigEndian.Uint32(loca[4*gid+4:])
	return end > start && i16(glyf, int(start)) < 0
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// nearInt16 reports whether a and b differ by at most one unit in each
// coordinate: the specification normalizes in 16.16 and truncates to 2.14
// after adding 2 (OpenType Font Variations Overview, Coordinate scales and
// normalization), fontTools rounds a float to 2.14, and the two differ by
// one unit where the 16.16 value ends in more than half a 2.14 unit.
func nearInt16(a, b []int16) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if abs(int(a[i])-int(b[i])) > 1 {
			return false
		}
	}
	return true
}

// TestInstanceNames pins the PostScript name of an instance (Adobe
// Technical Note #5902): the named instance's own name, the family and the
// subfamily of a named instance without one, and the axis values
// otherwise.
func TestInstanceNames(t *testing.T) {
	t.Parallel()
	font, _ := readGoldens(t)
	f, err := Parse(font)
	if err != nil {
		t.Fatal(err)
	}
	name, err := truetype.TableData(font, f.records, "name")
	if err != nil {
		t.Fatal(err)
	}
	bold := f.Axes().Instances[1].Coords
	for _, tc := range []struct {
		user []float64
		want string
	}{
		{bold, "LithoVar-BoldInstance"},
		{[]float64{300, 100, 0, 12, 0}, "LithoVar-Light"},
		{[]float64{651.25, 80, -2, 30, 12}, "LithoVar_651.25wght_80wdth_-2slnt_30opsz_12TEST"},
		{[]float64{2000, 0, -50, 0, -5}, "LithoVar_900wght_75wdth_-12slnt_8opsz_0TEST"},
		{[]float64{500, 100, math.Copysign(0, -1), 12, 0}, "LithoVar_500wght_100wdth_0slnt_12opsz_0TEST"},
	} {
		if got := PostScriptName(f.Axes(), name, tc.user); got != tc.want {
			t.Errorf("PostScriptName(%v) = %q, want %q", tc.user, got, tc.want)
		}
		program, err := f.Instance(context.Background(), tc.user)
		if err != nil {
			t.Fatal(err)
		}
		records, _ := truetype.ParseTableDirectory(program)
		renamed, _ := truetype.TableData(program, records, "name")
		if got := nameString(renamed, namePostScript); got != tc.want {
			t.Errorf("instance at %v names itself %q, want %q", tc.user, got, tc.want)
		}
	}
	if got := limitName("Prefix", "Prefix_"+string(make([]byte, 80))); len(got) > maxPostScriptName {
		t.Errorf("limitName kept %d characters", len(got))
	}
}

// TestInstanceRefusals pins what Instance refuses: a program without glyf
// outlines and a truncated one.
func TestInstanceRefusals(t *testing.T) {
	t.Parallel()
	font, _ := readGoldens(t)
	f, err := Parse(font)
	if err != nil {
		t.Fatal(err)
	}
	f.records = withoutTable(f.records, "glyf")
	if _, err := f.Instance(context.Background(), nil); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Instance without glyf = %v, want ErrUnsupported", err)
	}
	if _, err := Parse(font[:len(font)/2]); err == nil {
		t.Error("Parse of half a font succeeded")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	g, _ := Parse(font)
	if _, err := g.Instance(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("Instance with an ended context = %v", err)
	}
}

func withoutTable(records []truetype.TableRecord, tag string) []truetype.TableRecord {
	var out []truetype.TableRecord
	for _, r := range records {
		if r.Tag != tag {
			out = append(out, r)
		}
	}
	return out
}
