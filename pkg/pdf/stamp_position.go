package pdf

import (
	"fmt"

	"github.com/gotenberg/litho/pkg/models"
)

// ParseStampPosition returns the position name names, as
// [models.StampPosition.String] writes it. The empty name is
// [models.PositionNone]. Any other name fails with [ErrInvalidStampPosition].
func ParseStampPosition(name string) (models.StampPosition, error) {
	for p := models.PositionNone; p <= models.PositionBottomRight; p++ {
		if p.String() == name {
			return p, nil
		}
	}
	return models.PositionNone, fmt.Errorf("%w: %q", ErrInvalidStampPosition, name)
}

// checkStampPosition refuses a position outside the ones [models.StampPosition]
// lists.
func checkStampPosition(p models.StampPosition) error {
	if p > models.PositionBottomRight {
		return fmt.Errorf("%w: %d", ErrInvalidStampPosition, p)
	}
	return nil
}
