package pdf

import (
	"errors"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/models"
)

// toViolation translates an internal compliance violation (carried as an
// error) into a public [models.Violation]. An error that is not a compliance
// violation, such as a Schematron advisory, maps to a fatal Violation
// carrying its message so no information is lost.
func toViolation(e error) models.Violation {
	if cv, ok := errors.AsType[compliance.Violation](e); ok {
		return cv.Structured()
	}
	return models.Violation{Description: e.Error(), Severity: models.SeverityFatal}
}

// toViolations translates a flat slice of validation errors into
// structured [models.Violation] values.
func toViolations(errs []error) []models.Violation {
	out := make([]models.Violation, 0, len(errs))
	for _, e := range errs {
		out = append(out, toViolation(e))
	}
	return out
}
