package limits_test

import (
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/filter/dct"
	"github.com/gotenberg/litho/pkg/internal/filter/jbig2"
	"github.com/gotenberg/litho/pkg/internal/filter/jpx"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/models"
)

// TestDefault_PinsTheCaps pins every default, and every codec cap scaled
// from the image budget, to its value, so the default configuration reads
// every document with the same caps.
func TestDefault_PinsTheCaps(t *testing.T) {
	t.Parallel()

	want := models.Limits{
		MaxStreamBytes:       256 << 20,
		MaxImagePixels:       100_000_000,
		MaxEmbeddedFileBytes: 50 * 1024 * 1024,
		MaxFormDepth:         10,
		MaxCachedObjects:     50_000,
		MaxRenderPixels:      50_000_000,
		MaxConvertInputBytes: 16 << 20,
		MaxConvertPages:      10_000,

		MaxConvertMemoryBytes: 1 << 30,
	}
	if got := limits.Default(); got != want {
		t.Errorf("Default() = %+v, want %+v", got, want)
	}
	if got := limits.WithDefaults((models.Limits{})); got != want {
		t.Errorf("zero Limits with defaults = %+v, want %+v", got, want)
	}

	d := limits.Default()
	for name, c := range map[string]struct{ base, was int64 }{
		"jbig2 bitmap": {jbig2.DefaultMaxBitmapBytes, 100_000_000},
		"jpx samples":  {jpx.DefaultMaxSamples, 4 * 100_000_000},
		"dct samples":  {dct.DefaultMaxSamples, 4 * 100_000_000},
	} {
		if c.base != c.was {
			t.Errorf("%s default = %d, want %d", name, c.base, c.was)
		}
		if got := limits.ScaleImageCap(d, c.base); got != c.was {
			t.Errorf("%s scaled at the default budget = %d, want %d", name, got, c.was)
		}
	}
}

// TestScaleImageCap_MovesWithTheBudget pins that a codec cap follows the image
// budget in proportion, and never falls to zero.
func TestScaleImageCap_MovesWithTheBudget(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		pixels, base, want int64
	}{
		{50_000_000, 1 << 27, 1 << 26},
		{200_000_000, 1 << 27, 1 << 28},
		{limits.MaxImagePixelsCeiling, 100_000_000, limits.MaxImagePixelsCeiling},
		{1, 1 << 27, 1},
		{1, 100, 1},
	} {
		if got := limits.ScaleImageCap((models.Limits{MaxImagePixels: tc.pixels}), tc.base); got != tc.want {
			t.Errorf("ScaleImageCap(%d) under %d pixels = %d, want %d", tc.base, tc.pixels, got, tc.want)
		}
	}
}

// TestDerivedBounds_FollowTheStreamBudget pins the page content bound, the
// object-stream cache bound, the font-program cache bound, the
// colour-transform cache bound and the form content cache bound to the stream
// budget: at the default budget each equals the constant it replaced, 64 MiB,
// 256 MiB, 64 MiB and 64 MiB, the form content bound 64 MiB as the
// colour-transform bound is, and a raised or lowered budget moves all five,
// the page, font-program, colour-transform and form content bounds as a
// quarter of it and never below 1.
func TestDerivedBounds_FollowTheStreamBudget(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name                                     string
		l                                        models.Limits
		page, objStmCache, fontCache, colorCache int64
	}{
		{"default", limits.Default(), 64 << 20, 256 << 20, 64 << 20, 64 << 20},
		{"zero reads as the default", models.Limits{}, 64 << 20, 256 << 20, 64 << 20, 64 << 20},
		{"raised", models.Limits{MaxStreamBytes: 1 << 30}, 256 << 20, 1 << 30, 256 << 20, 256 << 20},
		{"ceiling", models.Limits{MaxStreamBytes: limits.MaxStreamBytesCeiling}, limits.MaxStreamBytesCeiling / 4, limits.MaxStreamBytesCeiling, limits.MaxStreamBytesCeiling / 4, limits.MaxStreamBytesCeiling / 4},
		{"lowered", models.Limits{MaxStreamBytes: 64 << 10}, 16 << 10, 64 << 10, 16 << 10, 16 << 10},
		{"below four bytes", models.Limits{MaxStreamBytes: 3}, 1, 3, 1, 1},
	} {
		if got := limits.MaxPageContentBytes(tc.l); got != tc.page {
			t.Errorf("%s: MaxPageContentBytes() = %d, want %d", tc.name, got, tc.page)
		}
		if got := limits.MaxObjStmCacheBytes(tc.l); got != tc.objStmCache {
			t.Errorf("%s: MaxObjStmCacheBytes() = %d, want %d", tc.name, got, tc.objStmCache)
		}
		if got := limits.MaxFontProgCacheBytes(tc.l); got != tc.fontCache {
			t.Errorf("%s: MaxFontProgCacheBytes() = %d, want %d", tc.name, got, tc.fontCache)
		}
		if got := limits.MaxColorCacheBytes(tc.l); got != tc.colorCache {
			t.Errorf("%s: MaxColorCacheBytes() = %d, want %d", tc.name, got, tc.colorCache)
		}
		if got := limits.MaxFormCacheBytes(tc.l); got != tc.colorCache {
			t.Errorf("%s: MaxFormCacheBytes() = %d, want %d", tc.name, got, tc.colorCache)
		}
	}
}

// TestMaxTextVisibilityCacheBytes_FollowsTheStreamBudget pins the text
// visibility cache bound to a sixty-fourth of the stream budget, 4 MiB at the
// default, never below 1.
func TestMaxTextVisibilityCacheBytes_FollowsTheStreamBudget(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		l    models.Limits
		want int64
	}{
		{"default", limits.Default(), 4 << 20},
		{"zero reads as the default", models.Limits{}, 4 << 20},
		{"raised", models.Limits{MaxStreamBytes: 1 << 30}, 16 << 20},
		{"lowered", models.Limits{MaxStreamBytes: 64 << 10}, 1 << 10},
		{"below sixty-four bytes", models.Limits{MaxStreamBytes: 3}, 1},
	} {
		if got := limits.MaxTextVisibilityCacheBytes(tc.l); got != tc.want {
			t.Errorf("%s: MaxTextVisibilityCacheBytes() = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestValidate_RefusesNegativeAndPastCeiling pins [limits.ErrInvalid] for each
// budget below zero or above its ceiling, and accepts zero and the ceilings.
func TestValidate_RefusesNegativeAndPastCeiling(t *testing.T) {
	t.Parallel()

	ceilings := models.Limits{
		MaxStreamBytes:       limits.MaxStreamBytesCeiling,
		MaxImagePixels:       limits.MaxImagePixelsCeiling,
		MaxEmbeddedFileBytes: limits.MaxEmbeddedFileBytesCeiling,
		MaxFormDepth:         limits.MaxFormDepthCeiling,
		MaxCachedObjects:     limits.MaxCachedObjectsCeiling,
		MaxRenderPixels:      limits.MaxRenderPixelsCeiling,
		MaxConvertInputBytes: limits.MaxConvertInputBytesCeiling,
		MaxConvertPages:      limits.MaxConvertPagesCeiling,

		MaxConvertMemoryBytes: limits.MaxConvertMemoryBytesCeiling,
	}
	for _, l := range []models.Limits{{}, limits.Default(), ceilings} {
		if err := limits.Validate(l); err != nil {
			t.Errorf("Validate(%+v) = %v, want nil", l, err)
		}
	}
	for _, l := range []models.Limits{
		{MaxStreamBytes: -1},
		{MaxImagePixels: -1},
		{MaxEmbeddedFileBytes: -1},
		{MaxFormDepth: -1},
		{MaxCachedObjects: -1},
		{MaxRenderPixels: -1},
		{MaxStreamBytes: limits.MaxStreamBytesCeiling + 1},
		{MaxImagePixels: limits.MaxImagePixelsCeiling + 1},
		{MaxEmbeddedFileBytes: limits.MaxEmbeddedFileBytesCeiling + 1},
		{MaxFormDepth: limits.MaxFormDepthCeiling + 1},
		{MaxCachedObjects: limits.MaxCachedObjectsCeiling + 1},
		{MaxRenderPixels: limits.MaxRenderPixelsCeiling + 1},
		{MaxConvertInputBytes: -1},
		{MaxConvertPages: -1},
		{MaxConvertInputBytes: limits.MaxConvertInputBytesCeiling + 1},
		{MaxConvertPages: limits.MaxConvertPagesCeiling + 1},
		{MaxConvertMemoryBytes: -1},
		{MaxConvertMemoryBytes: limits.MaxConvertMemoryBytesCeiling + 1},
	} {
		if err := limits.Validate(l); !errors.Is(err, limits.ErrInvalid) {
			t.Errorf("Validate(%+v) = %v, want ErrInvalid", l, err)
		}
	}
}
