package pdf

import (
	"context"
	"errors"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
)

// recolorRequest is what [Writer.ConvertColors] asks the colour conversion
// engine for: the target space and the options that shape the conversion.
type recolorRequest struct {
	// family is the family the converted colours are written in:
	// DeviceGray, DeviceRGB, DeviceCMYK, or ICCBased over profile.
	family string
	// profile is the target's ICC profile, the destination of every
	// transform, and components its channel count.
	profile    []byte
	components int
	intent     icc.Intent
	// blackPointCompensation and convertSpots are the options of
	// [WithBlackPointCompensation] and [WithSpotConversion].
	blackPointCompensation bool
	convertSpots           bool
}

// recolorResult counts what the engine converted.
type recolorResult struct {
	operators, colorSpaces, images, shadings int
	// keptSpots names the colorants of the Separation and DeviceN spaces
	// the engine kept, their alternate space converted.
	keptSpots []string
}

// recolorEngine rewrites the colours of every page of doc, and of everything
// they draw, to req's target, queueing the edits on alloc. It returns an
// error naming the construct when it cannot convert a colour, and queues
// nothing it would have to take back: [Writer.ConvertColors] discards alloc
// on an error.
type recolorEngine func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pages []*document.Page, req recolorRequest) (recolorResult, error)

// errRecolorEngineMissing is what [recolorUnavailable] refuses with.
var errRecolorEngineMissing = errors.New("no colour conversion engine is built in")

// recolorUnavailable is the engine until package recolor joins it, and the
// one a [Writer] uses when its recolor field is nil.
//
// TODO: [wave C1] replace with the entry point of pkg/internal/recolor
// (content streams, images and the spot policy), mapping recolorRequest onto
// its Target and Options.
func recolorUnavailable(_ context.Context, _ *writer.ObjAllocator, _ *document.Document, _ []*document.Page, _ recolorRequest) (recolorResult, error) {
	return recolorResult{}, errRecolorEngineMissing
}
