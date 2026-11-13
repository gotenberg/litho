package css

// PrintColorAdjust is print-color-adjust (CSS Color Adjust 1, 3.1):
// whether the user agent may adjust an element's colours to save ink.
type PrintColorAdjust uint8

// The print-color-adjust values. Economy is the initial value.
const (
	ColorEconomy PrintColorAdjust = iota
	ColorExact
)

// printColorAdjustProp is print-color-adjust, inherited, which
// -webkit-print-color-adjust aliases. color-adjust, its old shorthand,
// Chromium does not parse: it stays among [silentProperties].
func printColorAdjustProp() propDef {
	return enumProp("print-color-adjust", true, func(s *Style) *PrintColorAdjust { return &s.PrintColorAdjust }, map[string]PrintColorAdjust{
		"economy": ColorEconomy, "exact": ColorExact,
	})
}
