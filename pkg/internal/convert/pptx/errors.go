package pptx

import (
	"errors"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// errNotPresentationPart reports a part whose root is not the
// PresentationML element its relationship promises.
var errNotPresentationPart = fmt.Errorf("%w: not the expected PresentationML part", office.ErrMalformed)

// errStop ends the reading of a presentation past a cap: the slides read
// so far are kept.
var errStop = errors.New("presentation cap reached")
