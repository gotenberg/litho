package docx

import (
	"errors"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// isMalformed reports an error of a part that does not parse.
func isMalformed(err error) bool { return errors.Is(err, office.ErrMalformed) }
