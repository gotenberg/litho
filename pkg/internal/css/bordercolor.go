package css

// BorderColorIsCurrent reports whether the computed border colour of side
// is the currentColor keyword (CSS Color 4, 4.4), the initial value: the
// two-tone border styles paint it apart, as [Border] already holds the
// resolved colour.
func (s *Style) BorderColorIsCurrent(side Side) bool {
	if side > Left {
		return false
	}
	return s.currentColor&(slotBorderTop<<side) != 0
}
