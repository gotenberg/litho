package recolor

import (
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
)

// iccEngine builds transforms through the ICC engine: from the source
// endpoint's profile to the destination's through the profile connection
// space (ICC.1:2010, 6.3), each read through its table for the rendering
// intent ([icc.NewLink]). A profile-less XYZ endpoint is the connection space
// itself, which [icc.NewPCSLink] takes unclipped, and a profile-less Lab
// endpoint is CIELAB relative to D50 ([icc.LabProfile]).
type iccEngine struct{}

// Transform returns the identity between endpoints that mean the same colour,
// and the ICC engine's link otherwise.
func (iccEngine) Transform(src, dst Endpoint, opts Options) (Converter, error) {
	if src.same(dst) {
		return identity{}, nil
	}
	if dst.Space == SpaceXYZ {
		return nil, fmt.Errorf("%w: XYZ is no destination", ErrUnsupportedTarget)
	}
	dp, err := endpointProfile(dst)
	if err != nil {
		return nil, fmt.Errorf("the destination profile: %w", err)
	}
	topts := icc.TransformOptions{Intent: opts.Intent, BlackPointCompensation: opts.BlackPointCompensation}
	var link *icc.Link
	if src.Space == SpaceXYZ && src.Profile == nil {
		link, err = icc.NewPCSLink(dp, topts)
		if err != nil {
			return nil, err
		}
		return xyzLink{link}, nil
	}
	sp, err := endpointProfile(src)
	if err != nil {
		return nil, fmt.Errorf("the source profile: %w", err)
	}
	link, err = icc.NewLink(sp, dp, topts)
	if err != nil {
		return nil, err
	}
	if link.InputChannels() != src.components() || link.OutputChannels() != dst.components() {
		return nil, fmt.Errorf("%w: a link of %d to %d channels between %q and %q", ErrUnsupportedSpace,
			link.InputChannels(), link.OutputChannels(), src.Space, dst.Space)
	}
	return link, nil
}

// endpointProfile parses the profile of an endpoint, which must characterise
// the endpoint's data colour space (ICC.1:2010, 7.2.6). A Lab endpoint with
// no profile is CIELAB relative to D50.
func endpointProfile(e Endpoint) (*icc.Profile, error) {
	if e.Profile == nil {
		if e.Space == SpaceLab {
			return icc.LabProfile(), nil
		}
		return nil, fmt.Errorf("%w: a %q endpoint without a profile", ErrUnsupportedSpace, e.Space)
	}
	p, err := icc.Parse(e.Profile)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnsupportedSpace, err)
	}
	if p.Space != e.Space {
		return nil, fmt.Errorf("%w: a %q profile for a %q endpoint", ErrUnsupportedSpace, p.Space, e.Space)
	}
	return p, nil
}

// xyzLink converts an XYZ endpoint's components, encoded as ICC.1:2010,
// 6.3.4.3 encodes XYZ (see [Endpoint]), through a link from the PCS.
type xyzLink struct {
	link *icc.Link
}

func (c xyzLink) Convert(in, out []float64) {
	xyz := decodeXYZ(in)
	c.link.Convert(xyz[:], out)
}

// identity copies its input.
type identity struct{}

func (identity) Convert(in, out []float64) {
	copy(out, in)
}
