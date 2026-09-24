package icc

import (
	_ "embed" // the built-in CMYK profile
	"sync"
)

// builtinCMYK is CGATS001Compat-v2-micro from Compact ICC Profiles, a compact
// version 2 profile of the CGATS TR 001 (SWOP) printing condition, dedicated
// to the public domain under CC0 1.0 (profiles/README.md). PDF.js reads
// DeviceCMYK through the same file.
//
//go:embed profiles/CGATS001Compat-v2-micro.icc
var builtinCMYK []byte

// builtinCMYKTransforms builds the built-in profile's transform once per
// rendering intent.
var builtinCMYKTransforms = func() [4]func() (*Transform, error) {
	var ts [4]func() (*Transform, error)
	for i := range ts {
		ts[i] = sync.OnceValues(func() (*Transform, error) {
			p, err := Parse(builtinCMYK)
			if err != nil {
				return nil, err
			}
			return p.NewTransform(Intent(i))
		})
	}
	return ts
}()

// BuiltinCMYK returns the transform of litho's built-in CMYK profile for the
// rendering intent: the reading of DeviceCMYK when neither the document nor
// the caller names a profile for it. The transform is shared.
func BuiltinCMYK(intent Intent) (*Transform, error) {
	return builtinCMYKTransforms[intent&3]()
}
