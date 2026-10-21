package blend

import (
	"strconv"
	"strings"
)

// Edition is the edition of ISO 32000 whose blend functions a [Mode]
// follows. The two editions part on the edges of two separable modes of
// Table 136: ColorDodge of a black backdrop under a white source is 1 in ISO
// 32000-1 and 0 in ISO 32000-2, and ColorBurn of a white backdrop under a
// black source is 0 in ISO 32000-1 and 1 in ISO 32000-2. Every other mode,
// and every other colour of those two, blends alike in both.
type Edition uint8

// The editions.
const (
	// ISO32000v1 is ISO 32000-1:2008, PDF 1.7, which the PDF 1.x files
	// follow.
	ISO32000v1 Edition = iota
	// ISO32000v2 is ISO 32000-2, PDF 2.0.
	ISO32000v2
)

// EditionFor returns the edition a document of PDF version version, as the
// header or the catalog's /Version gives it (ISO 32000-1, 7.5.2), follows:
// ISO 32000-2 from version 2.0 on, ISO 32000-1 below it and for a version
// that does not read as one.
func EditionFor(version string) Edition {
	major, _, _ := strings.Cut(version, ".")
	if n, err := strconv.Atoi(major); err == nil && n >= 2 {
		return ISO32000v2
	}
	return ISO32000v1
}

// edition2 marks a mode that blends by the ISO 32000-2 formulas. Only the
// modes whose formulas differ carry it, so Normal stays Normal.
const edition2 = 0x80

// In returns m blending by the formulas of edition e. Paint and composite
// with the mode In returns: the edition rides the mode, so a [Group] needs
// no setting of its own.
func (m Mode) In(e Edition) Mode {
	base := m &^ edition2
	if e == ISO32000v2 && (base == ColorDodge || base == ColorBurn) {
		return base | edition2
	}
	return base
}

// base returns m without its edition.
func (m Mode) base() Mode { return m &^ edition2 }
