package limits_test

import (
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/filter/dct"
	"github.com/gotenberg/litho/pkg/internal/filter/jbig2"
	"github.com/gotenberg/litho/pkg/internal/filter/jpx"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// TestDefault_KeepsThePreviousCaps pins every default, and every codec cap
// scaled from the image budget, to the constant it replaced, so the default
// configuration reads documents exactly as before.
func TestDefault_KeepsThePreviousCaps(t *testing.T) {
	t.Parallel()

	want := limits.Limits{
		MaxStreamBytes:       256 << 20,
		MaxImagePixels:       100_000_000,
		MaxEmbeddedFileBytes: 50 * 1024 * 1024,
		MaxFormDepth:         10,
		MaxCachedObjects:     50_000,
	}
	if got := limits.Default(); got != want {
		t.Errorf("Default() = %+v, want %+v", got, want)
	}
	if got := (limits.Limits{}).WithDefaults(); got != want {
		t.Errorf("zero Limits with defaults = %+v, want %+v", got, want)
	}

	d := limits.Default()
	for name, c := range map[string]struct{ base, was int64 }{
		"jbig2 bitmap": {jbig2.DefaultMaxBitmapPixels, 100_000_000},
		"jpx samples":  {jpx.DefaultMaxSamples, 1 << 27},
		"dct samples":  {dct.DefaultMaxSamples, 1 << 27},
	} {
		if c.base != c.was {
			t.Errorf("%s default = %d, want %d", name, c.base, c.was)
		}
		if got := d.ScaleImageCap(c.base); got != c.was {
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
		if got := (limits.Limits{MaxImagePixels: tc.pixels}).ScaleImageCap(tc.base); got != tc.want {
			t.Errorf("ScaleImageCap(%d) under %d pixels = %d, want %d", tc.base, tc.pixels, got, tc.want)
		}
	}
}

// TestDerivedBounds_FollowTheStreamBudget pins the page content bound, the
// object-stream cache bound, the font-program cache bound and the
// colour-transform cache bound to the stream budget: at the default budget
// each equals the constant it replaced, 64 MiB, 256 MiB, 64 MiB and 64 MiB,
// and a raised or lowered budget moves all four, the page, font-program and
// colour-transform bounds as a quarter of it and never below 1.
func TestDerivedBounds_FollowTheStreamBudget(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name                                     string
		l                                        limits.Limits
		page, objStmCache, fontCache, colorCache int64
	}{
		{"default", limits.Default(), 64 << 20, 256 << 20, 64 << 20, 64 << 20},
		{"zero reads as the default", limits.Limits{}, 64 << 20, 256 << 20, 64 << 20, 64 << 20},
		{"raised", limits.Limits{MaxStreamBytes: 1 << 30}, 256 << 20, 1 << 30, 256 << 20, 256 << 20},
		{"ceiling", limits.Limits{MaxStreamBytes: limits.MaxStreamBytesCeiling}, 1 << 30, 4 << 30, 1 << 30, 1 << 30},
		{"lowered", limits.Limits{MaxStreamBytes: 64 << 10}, 16 << 10, 64 << 10, 16 << 10, 16 << 10},
		{"below four bytes", limits.Limits{MaxStreamBytes: 3}, 1, 3, 1, 1},
	} {
		if got := tc.l.MaxPageContentBytes(); got != tc.page {
			t.Errorf("%s: MaxPageContentBytes() = %d, want %d", tc.name, got, tc.page)
		}
		if got := tc.l.MaxObjStmCacheBytes(); got != tc.objStmCache {
			t.Errorf("%s: MaxObjStmCacheBytes() = %d, want %d", tc.name, got, tc.objStmCache)
		}
		if got := tc.l.MaxFontProgCacheBytes(); got != tc.fontCache {
			t.Errorf("%s: MaxFontProgCacheBytes() = %d, want %d", tc.name, got, tc.fontCache)
		}
		if got := tc.l.MaxColorCacheBytes(); got != tc.colorCache {
			t.Errorf("%s: MaxColorCacheBytes() = %d, want %d", tc.name, got, tc.colorCache)
		}
	}
}

// TestValidate_RefusesNegativeAndPastCeiling pins [limits.ErrInvalid] for each
// budget below zero or above its ceiling, and accepts zero and the ceilings.
func TestValidate_RefusesNegativeAndPastCeiling(t *testing.T) {
	t.Parallel()

	ceilings := limits.Limits{
		MaxStreamBytes:       limits.MaxStreamBytesCeiling,
		MaxImagePixels:       limits.MaxImagePixelsCeiling,
		MaxEmbeddedFileBytes: limits.MaxEmbeddedFileBytesCeiling,
		MaxFormDepth:         limits.MaxFormDepthCeiling,
		MaxCachedObjects:     limits.MaxCachedObjectsCeiling,
	}
	for _, l := range []limits.Limits{{}, limits.Default(), ceilings} {
		if err := l.Validate(); err != nil {
			t.Errorf("Validate(%+v) = %v, want nil", l, err)
		}
	}
	for _, l := range []limits.Limits{
		{MaxStreamBytes: -1},
		{MaxImagePixels: -1},
		{MaxEmbeddedFileBytes: -1},
		{MaxFormDepth: -1},
		{MaxCachedObjects: -1},
		{MaxStreamBytes: limits.MaxStreamBytesCeiling + 1},
		{MaxImagePixels: limits.MaxImagePixelsCeiling + 1},
		{MaxEmbeddedFileBytes: limits.MaxEmbeddedFileBytesCeiling + 1},
		{MaxFormDepth: limits.MaxFormDepthCeiling + 1},
		{MaxCachedObjects: limits.MaxCachedObjectsCeiling + 1},
	} {
		if err := l.Validate(); !errors.Is(err, limits.ErrInvalid) {
			t.Errorf("Validate(%+v) = %v, want ErrInvalid", l, err)
		}
	}
}
