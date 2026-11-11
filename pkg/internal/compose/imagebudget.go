package compose

import (
	"context"
	"fmt"
	"image"
	"image/color"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// A picture costs the conversion's memory budget twice over its life: its
// decoded pixels while they live, [limits.DecodedPixelCost] or
// [limits.DecodedPixelCost16] a pixel, charged before they are allocated
// and released once [EncodeImage] has read them; then its compressed
// samples, charged by [EncodeImage] and held as long as the document, which
// keeps every [ImageSource] until it is written.

// pixelCost returns what one decoded pixel of colour model m costs.
func pixelCost(m color.Model) int64 {
	if limits.DeepModel(m) {
		return limits.DecodedPixelCost16
	}
	return limits.DecodedPixelCost
}

// ChargeImage charges ctx's budget for a decoded picture of w by h pixels
// of colour model m, before the caller allocates it, failing with the
// budget's refusal. [EncodeImage] releases the charge, as
// [ReleaseImage] does for a picture dropped without being encoded. A
// converter decoding a document's picture charges it with
// [limits.Budget.ChargePicture] instead, which also applies the picture
// cap.
func ChargeImage(ctx context.Context, w, h int, m color.Model) error {
	if err := limits.BudgetFrom(ctx).ChargeUnits(int64(w)*int64(h), pixelCost(m), "decoded pictures"); err != nil {
		return fmt.Errorf("decoding image: %w", err)
	}
	return nil
}

// ReleaseImage gives back to ctx's budget what the decoded picture img was
// charged, the caller dropping it.
func ReleaseImage(ctx context.Context, img image.Image) {
	if img == nil {
		return
	}
	b := img.Bounds()
	limits.BudgetFrom(ctx).Release(int64(b.Dx()) * int64(b.Dy()) * pixelCost(img.ColorModel()))
}
