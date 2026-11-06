package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
)

// pixelRule is a pixel comparison rule: when two pixels count as equal, and
// how many may differ for two images to match.
type pixelRule struct {
	// Tolerance is the largest channel difference, in 8-bit levels, two
	// pixels may show and still count as equal.
	Tolerance int
	// Neighbourhood is the radius, in pixels, of the window a pixel looks
	// for its match in, checked both ways (see [comparePage]). 0 compares
	// each pixel with the one at its place alone.
	Neighbourhood int
	// MaxDiffPerMillion is the share of the compared pixels, in parts per
	// million, that may differ.
	MaxDiffPerMillion int64
	// MaxDiffPixels is a count of pixels that may differ whatever the
	// share; the larger of the two allowances applies.
	MaxDiffPixels int
}

// renderPixelRule is the render cell's page rule without its ink-share bar:
// channel tolerance 64, neighbourhood 1, at most 1% of the pixels differing.
// See renderChannelTolerance, renderNeighbourhood and
// renderMaxDiffPerMillion for why each value.
//
//nolint:gochecknoglobals // a fixed rule, read only
var renderPixelRule = pixelRule{
	Tolerance:         renderChannelTolerance,
	Neighbourhood:     renderNeighbourhood,
	MaxDiffPerMillion: renderMaxDiffPerMillion,
}

// pixelDiff is the outcome of comparing two images under a [pixelRule].
type pixelDiff struct {
	// Width and Height are the compared area: the overlap of the two images.
	Width, Height int
	// GotWidth, GotHeight, WantWidth and WantHeight are the images' sizes.
	GotWidth, GotHeight, WantWidth, WantHeight int
	// Pixels is Width times Height, DiffPixels how many of them differ, and
	// MaxDelta the largest channel difference at one place, before the
	// neighbourhood applies.
	Pixels, DiffPixels, MaxDelta int
	// SizeMismatch marks images whose sizes differ by more than the slack
	// the comparison allowed; nothing else is then compared.
	SizeMismatch bool
	// mask marks each differing pixel of the compared area, row by row.
	mask []bool
}

// comparePixels compares got with want, both composed over white, under rule.
// Sizes may differ by slack pixels along each axis, and the overlap is
// compared; past it the result is a size mismatch.
//
// Cost: O(w*h) when the images agree pixel for pixel, O(w*h*(2r+1)^2) at
// worst, r the neighbourhood.
func comparePixels(got, want image.Image, rule pixelRule, slack int) pixelDiff {
	gb, wb := got.Bounds(), want.Bounds()
	d := pixelDiff{GotWidth: gb.Dx(), GotHeight: gb.Dy(), WantWidth: wb.Dx(), WantHeight: wb.Dy()}
	if absInt(d.GotWidth-d.WantWidth) > slack || absInt(d.GotHeight-d.WantHeight) > slack {
		d.SizeMismatch = true
		return d
	}
	w, h := min(d.GotWidth, d.WantWidth), min(d.GotHeight, d.WantHeight)
	d.Width, d.Height, d.Pixels = w, h, w*h
	g, wt := flattenOnWhite(got, w, h), flattenOnWhite(want, w, h)
	d.mask = make([]bool, w*h)
	for y := range h {
		for x := range w {
			i := 3 * (y*w + x)
			delta := channelDelta(g[i:i+3], wt[i:i+3])
			d.MaxDelta = max(d.MaxDelta, delta)
			if delta <= rule.Tolerance {
				continue
			}
			if !nearMatchWithin(g[i:i+3], wt, w, h, x, y, rule) || !nearMatchWithin(wt[i:i+3], g, w, h, x, y, rule) {
				d.DiffPixels++
				d.mask[y*w+x] = true
			}
		}
	}
	return d
}

// within reports whether the differing pixels stay within the rule's
// allowance. A size mismatch is never within.
func (d pixelDiff) within(rule pixelRule) bool {
	if d.SizeMismatch {
		return false
	}
	if d.DiffPixels <= rule.MaxDiffPixels {
		return true
	}
	return int64(d.DiffPixels)*1_000_000 <= rule.MaxDiffPerMillion*int64(d.Pixels)
}

// String summarises the comparison for a verdict's reason.
func (d pixelDiff) String() string {
	if d.SizeMismatch {
		return fmt.Sprintf("size %dx%d against %dx%d", d.GotWidth, d.GotHeight, d.WantWidth, d.WantHeight)
	}
	return fmt.Sprintf("%d of %d pixels differ, max channel delta %d", d.DiffPixels, d.Pixels, d.MaxDelta)
}

// nearMatchWithin reports whether a pixel within the rule's neighbourhood of
// (x, y) in img lies within its tolerance of px.
func nearMatchWithin(px, img []uint8, w, h, x, y int, rule pixelRule) bool {
	r := rule.Neighbourhood
	for ny := max(0, y-r); ny <= min(h-1, y+r); ny++ {
		for nx := max(0, x-r); nx <= min(w-1, x+r); nx++ {
			j := 3 * (ny*w + nx)
			if channelDelta(px, img[j:j+3]) <= rule.Tolerance {
				return true
			}
		}
	}
	return false
}

// reportGutter is the width, in pixels, of the grey band between the panels
// of a failure report.
const reportGutter = 4

// maxReportPixels bounds the pixels of one failure report. Anti-DoS guard,
// not spec-mandated: an A4 page at 72 dpi, three panels wide, is about 1.5
// million pixels, and a page drawn far larger than its golden would otherwise
// write a report of any size.
const maxReportPixels = 16 << 20

// writeDiffReport writes a failure report to path: left, right and a heatmap
// of their differences side by side, each scaled up scale times with nearest
// neighbour sampling so a small grid stays readable. The heatmap is the right
// image faded to a light grey with each differing pixel of d in red; a
// comparison that stopped at a size mismatch marks no pixel. The directory is
// created when missing.
func writeDiffReport(path string, left, right image.Image, d pixelDiff, scale int) error {
	scale = max(scale, 1)
	lb, rb := left.Bounds(), right.Bounds()
	pw, ph := max(lb.Dx(), rb.Dx()), max(lb.Dy(), rb.Dy())
	w, h := (3*pw+2*reportGutter)*scale, ph*scale
	if w <= 0 || h <= 0 || int64(w)*int64(h) > maxReportPixels {
		return fmt.Errorf("report of %dx%d pixels: outside the %d pixel cap", w, h, maxReportPixels)
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	grey := color.RGBA{0x80, 0x80, 0x80, 0xff}
	for y := range h {
		for x := range w {
			out.SetRGBA(x, y, grey)
		}
	}
	lpx, rpx := flattenOnWhite(left, lb.Dx(), lb.Dy()), flattenOnWhite(right, rb.Dx(), rb.Dy())
	paint := func(panel, x, y int, c color.RGBA) {
		ox := panel*(pw+reportGutter)*scale + x*scale
		for dy := range scale {
			for dx := range scale {
				out.SetRGBA(ox+dx, y*scale+dy, c)
			}
		}
	}
	for y := range lb.Dy() {
		for x := range lb.Dx() {
			i := 3 * (y*lb.Dx() + x)
			paint(0, x, y, color.RGBA{lpx[i], lpx[i+1], lpx[i+2], 0xff})
		}
	}
	for y := range rb.Dy() {
		for x := range rb.Dx() {
			i := 3 * (y*rb.Dx() + x)
			paint(1, x, y, color.RGBA{rpx[i], rpx[i+1], rpx[i+2], 0xff})
			// Fade to 25% contrast over white so the red marks stand out.
			lum := (int(rpx[i])*299 + int(rpx[i+1])*587 + int(rpx[i+2])*114) / 1000
			faded := uint8(255 - (255-lum)/4) //nolint:gosec // lum is within 0..255, so the value is too
			c := color.RGBA{faded, faded, faded, 0xff}
			if x < d.Width && y < d.Height && d.mask != nil && d.mask[y*d.Width+x] {
				c = color.RGBA{0xff, 0x00, 0x00, 0xff}
			}
			paint(2, x, y, c)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create report directory: %w", err)
	}
	f, err := os.Create(path) //nolint:gosec // report path under the report directory the caller chose
	if err != nil {
		return fmt.Errorf("create report: %w", err)
	}
	if err := png.Encode(f, out); err != nil {
		f.Close()
		return fmt.Errorf("encode report: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close report: %w", err)
	}
	return nil
}
