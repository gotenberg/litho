package varfont

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/typography/cff"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// cff2Golden is one location of cff2_golden.json: fontTools' outlines of
// testdata/varfont_cff2.otf there.
type cff2Golden struct {
	Location map[string]float64 `json:"location"`
	Glyphs   []struct {
		Name    string               `json:"name"`
		Ops     [][2]json.RawMessage `json:"ops"`
		Advance float64              `json:"advance"`
	} `json:"glyphs"`
}

// cff2OutlineTolerance is how far an instance's points may sit from
// fontTools' unrounded ones, in font units. litho normalizes coordinates
// to 2.14 values, as the OpenType Font Variations Overview prescribes, and
// fontTools' glyph set keeps them as floats: a scalar differs by up to
// 1/32768, which a delta of a few hundred units turns into a few
// thousandths of a unit. The 16.16 operands of a Type 2 charstring then
// hold each value to 1/65536.
const cff2OutlineTolerance = 1.0 / 64

// TestCFF2InstanceMatchesFontTools compares the instances of a CFF2
// variable font with fontTools' outlines at the same locations: every
// point within [cff2OutlineTolerance], every advance fontTools' rounded
// half up. The instance is an OpenType program with CFF outlines and no
// variation table.
func TestCFF2InstanceMatchesFontTools(t *testing.T) {
	t.Parallel()
	program, err := os.ReadFile("testdata/varfont_cff2.otf")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/cff2_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var goldens []cff2Golden
	if err := json.Unmarshal(data, &goldens); err != nil {
		t.Fatal(err)
	}
	vf, err := Parse(program)
	if err != nil {
		t.Fatal(err)
	}
	if !vf.HasCFF2() {
		t.Fatal("HasCFF2 = false for a CFF2 program")
	}
	worst := 0.0
	for _, g := range goldens {
		user := vf.Axes().Defaults()
		for i, ax := range vf.Axes().Axes {
			if v, ok := g.Location[ax.Tag]; ok {
				user[i] = v
			}
		}
		inst, err := vf.Instance(context.Background(), user)
		if err != nil {
			t.Fatalf("%v: %v", g.Location, err)
		}
		records, err := truetype.ParseTableDirectory(inst)
		if err != nil {
			t.Fatal(err)
		}
		for _, tag := range []string{"CFF2", "fvar", "HVAR", "MVAR", "avar"} {
			if _, err := truetype.TableData(inst, records, tag); err == nil {
				t.Errorf("%v: the instance keeps %s", g.Location, tag)
			}
		}
		table, err := truetype.CFFTable(inst)
		if err != nil {
			t.Fatalf("%v: no CFF table: %v", g.Location, err)
		}
		font, err := cff.ParseFont(table)
		if err != nil {
			t.Fatalf("%v: %v", g.Location, err)
		}
		hm, err := truetype.ReadHMetrics(inst)
		if err != nil {
			t.Fatal(err)
		}
		for gid, gg := range g.Glyphs {
			glyph, err := font.Glyph(context.Background(), gid)
			if err != nil {
				t.Fatalf("%v: glyph %s: %v", g.Location, gg.Name, err)
			}
			want := goldenPoints(t, gg.Ops)
			got := pathPoints(glyph.Path)
			if len(got) != len(want) {
				t.Errorf("%v: glyph %s has %d points, fontTools %d", g.Location, gg.Name, len(got), len(want))
				continue
			}
			for i := range got {
				d := math.Max(math.Abs(got[i].X-want[i].X), math.Abs(got[i].Y-want[i].Y))
				worst = math.Max(worst, d)
				if d > cff2OutlineTolerance {
					t.Errorf("%v: glyph %s point %d at %v, fontTools %v", g.Location, gg.Name, i, got[i], want[i])
					break
				}
			}
			if adv := hm.Advance(gid); adv != float64(otRound(gg.Advance)) {
				t.Errorf("%v: glyph %s advances %v, fontTools %v", g.Location, gg.Name, adv, gg.Advance)
			}
			if glyph.Advance != hm.Advance(gid) {
				t.Errorf("%v: glyph %s: charstring width %v, hmtx %v", g.Location, gg.Name, glyph.Advance, hm.Advance(gid))
			}
		}
	}
	t.Logf("largest distance from fontTools: %g units", worst)
}

// goldenPoints returns the points of fontTools' pen calls, closePath
// left out.
func goldenPoints(t *testing.T, ops [][2]json.RawMessage) []geom.Point {
	t.Helper()
	var out []geom.Point
	for _, op := range ops {
		var name string
		var pts [][2]float64
		if err := json.Unmarshal(op[0], &name); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(op[1], &pts); err != nil {
			t.Fatal(err)
		}
		for _, p := range pts {
			out = append(out, geom.Point{X: p[0], Y: p[1]})
		}
	}
	return out
}

// pathPoints returns the points of p.
func pathPoints(p geom.Path) []geom.Point {
	return p.Pts
}

// TestCFF2DefaultInstance pins that the instance at the default
// coordinates is the font itself with CFF outlines: its glyphs draw as
// the CFF2 program's default instance does.
func TestCFF2DefaultInstance(t *testing.T) {
	t.Parallel()
	program, err := os.ReadFile("testdata/varfont_cff2.otf")
	if err != nil {
		t.Fatal(err)
	}
	vf, err := Parse(program)
	if err != nil {
		t.Fatal(err)
	}
	inst, err := vf.Instance(context.Background(), vf.Axes().Defaults())
	if err != nil {
		t.Fatal(err)
	}
	records, err := truetype.ParseTableDirectory(program)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := truetype.TableData(program, records, "CFF2")
	if err != nil {
		t.Fatal(err)
	}
	src, err := cff.ParseCFF2(raw)
	if err != nil {
		t.Fatal(err)
	}
	table, err := truetype.CFFTable(inst)
	if err != nil {
		t.Fatal(err)
	}
	out, err := cff.ParseFont(table)
	if err != nil {
		t.Fatal(err)
	}
	if !out.CIDKeyed() || out.NumGlyphs() != src.NumGlyphs() {
		t.Fatalf("instance: CID-keyed %v, %d glyphs; want a CID-keyed font of %d", out.CIDKeyed(), out.NumGlyphs(), src.NumGlyphs())
	}
	for gid := range src.NumGlyphs() {
		a, err := src.Glyph(context.Background(), gid)
		if err != nil {
			t.Fatal(err)
		}
		b, err := out.Glyph(context.Background(), gid)
		if err != nil {
			t.Fatal(err)
		}
		if len(a.Path.Pts) != len(b.Path.Pts) {
			t.Fatalf("glyph %d: %d points, the CFF2 default %d", gid, len(b.Path.Pts), len(a.Path.Pts))
		}
		for i := range a.Path.Pts {
			if a.Path.Pts[i] != b.Path.Pts[i] {
				t.Fatalf("glyph %d point %d: %v, the CFF2 default %v", gid, i, b.Path.Pts[i], a.Path.Pts[i])
			}
		}
	}
}

// TestDefaultCFF pins that every CFF2 program converts to CFF outlines at
// its default instance: a variable one as [Font.Instance] writes it at
// the default coordinates, a static one without 'fvar' under its own
// PostScript name with the default outlines, and that a program with
// glyf outlines is refused.
func TestDefaultCFF(t *testing.T) {
	t.Parallel()
	program, err := os.ReadFile("testdata/varfont_cff2.otf")
	if err != nil {
		t.Fatal(err)
	}
	vf, err := Parse(program)
	if err != nil {
		t.Fatal(err)
	}
	want, err := vf.Instance(context.Background(), vf.Axes().Defaults())
	if err != nil {
		t.Fatal(err)
	}
	got, err := DefaultCFF(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Error("the default CFF program of a variable font is not its instance at the default coordinates")
	}

	records, err := truetype.ParseTableDirectory(program)
	if err != nil {
		t.Fatal(err)
	}
	tables := map[string][]byte{}
	var tags []string
	for _, r := range records {
		if r.Tag == "fvar" || r.Tag == "avar" || r.Tag == "HVAR" || r.Tag == "MVAR" || r.Tag == "STAT" {
			continue
		}
		if tables[r.Tag], err = truetype.TableData(program, records, r.Tag); err != nil {
			t.Fatal(err)
		}
		tags = append(tags, r.Tag)
	}
	static := truetype.AssembleTables(cffSfntVersion, tags, tables)
	if !IsCFF2(static) {
		t.Fatal("IsCFF2 = false for a static CFF2 program")
	}
	inst, err := DefaultCFF(context.Background(), static)
	if err != nil {
		t.Fatal(err)
	}
	if IsCFF2(inst) {
		t.Error("the static program keeps its CFF2 outlines")
	}
	instRecords, err := truetype.ParseTableDirectory(inst)
	if err != nil {
		t.Fatal(err)
	}
	name, err := truetype.TableData(inst, instRecords, "name")
	if err != nil {
		t.Fatal(err)
	}
	if ps := nameString(name, namePostScript); ps != "LithoVarCFF-ExtraLight" {
		t.Errorf("PostScript name %q, want the program's own LithoVarCFF-ExtraLight", ps)
	}
	a, err := truetype.CFFTable(inst)
	if err != nil {
		t.Fatal(err)
	}
	b, err := truetype.CFFTable(want)
	if err != nil {
		t.Fatal(err)
	}
	fa, err := cff.ParseFont(a)
	if err != nil {
		t.Fatal(err)
	}
	fb, err := cff.ParseFont(b)
	if err != nil {
		t.Fatal(err)
	}
	if fa.NumGlyphs() != fb.NumGlyphs() {
		t.Fatalf("%d glyphs, want %d", fa.NumGlyphs(), fb.NumGlyphs())
	}
	for gid := range fa.NumGlyphs() {
		ga, err := fa.Glyph(context.Background(), gid)
		if err != nil {
			t.Fatal(err)
		}
		gb, err := fb.Glyph(context.Background(), gid)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(ga.Path.Pts, gb.Path.Pts) || ga.Advance != gb.Advance {
			t.Fatalf("glyph %d of the static program differs from the default instance", gid)
		}
	}

	ttf, err := os.ReadFile("testdata/varfont.ttf")
	if err != nil {
		t.Fatal(err)
	}
	if IsCFF2(ttf) {
		t.Error("IsCFF2 = true for glyf outlines")
	}
	if _, err := DefaultCFF(context.Background(), ttf); !errors.Is(err, ErrUnsupported) {
		t.Errorf("glyf outlines: %v, want ErrUnsupported", err)
	}
}
