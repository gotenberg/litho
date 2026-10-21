package recolor

import "errors"

var (
	// ErrUnsupportedTarget reports a [Target] recolouring cannot write into:
	// a family other than DeviceGray, DeviceRGB, DeviceCMYK and ICCBased, or
	// an ICCBased target without its profile or colour space object.
	ErrUnsupportedTarget = errors.New("unsupported recolour target")

	// ErrUnsupportedSpace reports a colour space recolouring cannot read: an
	// unknown family, a malformed definition, or a profile whose data colour
	// space disagrees with its component count.
	ErrUnsupportedSpace = errors.New("unsupported colour space")

	// ErrTooManyColorants reports a DeviceN space whose tint transform cannot
	// be sampled into the target within [maxSampledEntries].
	ErrTooManyColorants = errors.New("too many colorants to resample")
)
