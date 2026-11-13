package cff

import (
	"context"
	"errors"
	"math"
	"os"
	"testing"
)

// zeroScalars returns scalars of 0 for every region of each
// ItemVariationData of the CFF2 table data: the default instance.
func zeroScalars(t testing.TB, data []byte) func([]byte) ([][]float64, error) {
	t.Helper()
	f, err := ParseCFF2(data)
	if err != nil {
		t.Fatal(err)
	}
	return func([]byte) ([][]float64, error) {
		out := make([][]float64, len(f.regionCounts))
		for i, k := range f.regionCounts {
			out[i] = make([]float64, k)
		}
		return out, nil
	}
}

// TestInstanceCFF2DefaultDrawsAsTheSource pins that the instance at the
// default coordinates draws every glyph as the CFF2 program's default
// instance: subroutines inlined, hints and masks kept, flex kept, the 121
// operands of one operator cut into operators the Type 2 stack holds (the
// CFF reader refuses more than 48), and each width first.
func TestInstanceCFF2DefaultDrawsAsTheSource(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/cff2_ops.cff2")
	if err != nil {
		t.Fatal(err)
	}
	src, err := ParseCFF2(data)
	if err != nil {
		t.Fatal(err)
	}
	widths := make([]float64, src.NumGlyphs())
	for i := range widths {
		widths[i] = float64(100 + i)
	}
	inst, err := InstanceCFF2(context.Background(), data, "Probe-Instance", widths, zeroScalars(t, data))
	if err != nil {
		t.Fatal(err)
	}
	out, err := ParseFont(inst.Program)
	if err != nil {
		t.Fatal(err)
	}
	if !out.CIDKeyed() || out.NumGlyphs() != src.NumGlyphs() {
		t.Fatalf("CID-keyed %v, %d glyphs; want a CID-keyed font of %d", out.CIDKeyed(), out.NumGlyphs(), src.NumGlyphs())
	}
	if info, err := ReadInfo(inst.Program); err != nil || info.FontName != "Probe-Instance" {
		t.Errorf("font name %q (%v), want Probe-Instance", info.FontName, err)
	}
	for gid := range src.NumGlyphs() {
		a, err := src.Glyph(context.Background(), gid)
		if err != nil {
			t.Fatal(err)
		}
		b, err := out.Glyph(context.Background(), gid)
		if err != nil {
			t.Fatalf("glyph %d: %v", gid, err)
		}
		if b.Advance != widths[gid] {
			t.Errorf("glyph %d advances %v, want %v", gid, b.Advance, widths[gid])
		}
		if len(a.Path.Ops) != len(b.Path.Ops) || len(a.Path.Pts) != len(b.Path.Pts) {
			t.Fatalf("glyph %d: %d ops, the source %d", gid, len(b.Path.Ops), len(a.Path.Ops))
		}
		for i := range a.Path.Pts {
			if a.Path.Pts[i] != b.Path.Pts[i] {
				t.Fatalf("glyph %d point %d at %v, the source %v", gid, i, b.Path.Pts[i], a.Path.Pts[i])
			}
		}
		if box, ok := b.Path.Bounds(); ok && !inst.Empty[gid] {
			if got := inst.Boxes[gid]; got != [4]float64{box.Min.X, box.Min.Y, box.Max.X, box.Max.Y} {
				t.Errorf("glyph %d box %v, its outline's %v", gid, got, box)
			}
		}
	}
}

// TestBlendResolve pins the blend arithmetic: each default value plus its
// deltas, the first value's first, times the regions' scalars (OpenType
// specification, CFF2 table, blend).
func TestBlendResolve(t *testing.T) {
	t.Parallel()
	got, ok := blendResolve([]float64{9, 5, 6, 1, 2, 3, 4, 2}, []float64{0.5, 1})
	if !ok || len(got) != 3 || got[0] != 9 || got[1] != 5+0.5+2 || got[2] != 6+1.5+4 {
		t.Errorf("blend = %v, %v; want [9 7.5 11.5]", got, ok)
	}
	if _, ok := blendResolve([]float64{5, 1, 2}, []float64{0.5, 1}); ok {
		t.Error("a blend short of its operands resolved")
	}
}

// TestInstanceCFF2Rejects pins the refusals: scalars that do not fit the
// VariationStore, too few widths, and a program that is not CFF2.
func TestInstanceCFF2Rejects(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/cff2_ops.cff2")
	if err != nil {
		t.Fatal(err)
	}
	widths := make([]float64, 16)
	for _, tc := range []struct {
		name    string
		data    []byte
		widths  []float64
		scalars func([]byte) ([][]float64, error)
	}{
		{"scalar sets", data, widths, func([]byte) ([][]float64, error) { return [][]float64{{0}}, nil }},
		{"scalars of a set", data, widths, func([]byte) ([][]float64, error) { return [][]float64{{0}, {0}}, nil }},
		{"widths", data, nil, zeroScalars(t, data)},
		{"not CFF2", []byte{1, 0, 4, 4}, widths, zeroScalars(t, data)},
	} {
		if _, err := InstanceCFF2(context.Background(), tc.data, "X", tc.widths, tc.scalars); !errors.Is(err, ErrInvalidCFF) {
			t.Errorf("%s: err = %v, want ErrInvalidCFF", tc.name, err)
		}
	}
	boom := errors.New("boom")
	if _, err := InstanceCFF2(context.Background(), data, "X", widths, func([]byte) ([][]float64, error) { return nil, boom }); !errors.Is(err, boom) {
		t.Errorf("err = %v, want the scalars' error", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := InstanceCFF2(ctx, data, "X", widths, zeroScalars(t, data)); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

// TestAppendDictNumber pins the DICT operand forms an instance writes:
// integers in their shortest form, other values as reals that read back
// as written.
func TestAppendDictNumber(t *testing.T) {
	t.Parallel()
	for _, v := range []float64{0, 107, -107, 108, 1131, -1131, 2000, -40000, 70000, 0.039625, -0.5, 1e-7, 123456.75} {
		b := appendDictNumber(nil, v)
		var got []float64
		scanDict(append(b, opStdHW), func(op int, operands []float64) {
			if op == opStdHW {
				got = operands
			}
		})
		if len(got) != 1 || math.Abs(got[0]-v) > 1e-9*math.Max(1, math.Abs(v)) {
			t.Errorf("%v reads back as %v (% x)", v, got, b)
		}
	}
}
