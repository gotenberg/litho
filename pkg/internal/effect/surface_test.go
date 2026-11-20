package effect

import (
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/models"
)

func TestNew_Bounds(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		f    Format
		w, h int
		err  error
	}{
		{Coverage, 1, 1, nil}, {RGBA, 4096, 4096, nil}, {Coverage, 0, 4, ErrTooLarge}, {RGBA, 4, -1, ErrTooLarge},
		{Coverage, MaxPixels, 2, ErrTooLarge}, {Coverage, 1 << 30, 1 << 30, ErrTooLarge}, {Format(3), 1, 1, ErrFormat},
	} {
		s, err := New(ctx, tc.f, tc.w, tc.h, Grid{})
		if !errors.Is(err, tc.err) {
			t.Errorf("New(%d, %d, %d) = %v, want %v", tc.f, tc.w, tc.h, err, tc.err)
		}
		if err == nil && len(s.Pix) != tc.w*tc.h*int(tc.f) {
			t.Errorf("New(%d, %d, %d) holds %d bytes", tc.f, tc.w, tc.h, len(s.Pix))
		}
	}
	if !errors.Is(ErrTooLarge, limits.ErrInputTooLarge) {
		t.Error("ErrTooLarge does not wrap limits.ErrInputTooLarge")
	}
}

func TestNew_ChargesBudget(t *testing.T) {
	ctx, b, done := limits.WithBudget(context.Background(), models.Limits{MaxConvertMemoryBytes: 1 << 20})
	defer done()
	s, err := New(ctx, RGBA, 256, 256, Grid{})
	if err != nil {
		t.Fatal(err)
	}
	if b.Held() != 256*256*4 {
		t.Errorf("budget holds %d bytes, want %d", b.Held(), 256*256*4)
	}
	if err := Blur(ctx, s, GaussianKernel(2), GaussianKernel(2), EdgeNone); err != nil {
		t.Fatal(err)
	}
	if b.Held() != 256*256*4 {
		t.Errorf("budget holds %d bytes after a blur, want the surface's %d", b.Held(), 256*256*4)
	}
	c, err := s.Clone(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c.Release()
	s.Release()
	s.Release()
	if b.Held() != 0 {
		t.Errorf("budget holds %d bytes after release, want 0", b.Held())
	}
	if _, err := New(ctx, RGBA, 1024, 1024, Grid{}); !errors.Is(err, limits.ErrMemoryBudget) {
		t.Errorf("New past the budget = %v, want ErrMemoryBudget", err)
	}
}

func TestPrimitives_RefuseFormats(t *testing.T) {
	ctx := context.Background()
	cov, rgba := newSurface(t, Coverage, 2, 2), newSurface(t, RGBA, 2, 2)
	other := newSurface(t, Coverage, 3, 2)
	for name, err := range map[string]error{
		"tint":       func() error { _, err := Tint(ctx, rgba, Color{}); return err }(),
		"threshold":  Threshold(rgba, 1),
		"matrix":     ColorMatrix(ctx, cov, IdentityMatrix),
		"transfer":   Transfer(ctx, cov, &IdentityLUT, &IdentityLUT, &IdentityLUT, &IdentityLUT),
		"composite":  Composite(ctx, cov, other, Over),
		"arithmetic": Arithmetic(ctx, cov, rgba, 0, 0, 0, 0),
		"blend":      Blend(ctx, cov, cov, Multiply),
		"transform":  Transform(ctx, cov, rgba, typeset.Identity),
		"tile":       Tile(ctx, cov, rgba, 0, 0, 1, 1),
	} {
		if !errors.Is(err, ErrFormat) {
			t.Errorf("%s = %v, want ErrFormat", name, err)
		}
	}
}
