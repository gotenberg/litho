package cmap

// AdobeCIDToUnicode returns the predefined CID→Unicode map for the given
// Adobe character collection ordering (e.g., "Japan1", "GB1", "CNS1", "Korea1").
// Returns nil if the ordering is not recognized.
func AdobeCIDToUnicode(ordering string) map[int]rune {
	switch ordering {
	case "Japan1":
		return adobeJapan1ToUnicode
	case "GB1":
		return adobeGB1ToUnicode
	case "CNS1":
		return adobeCNS1ToUnicode
	case "Korea1":
		return adobeKorea1ToUnicode
	default:
		return nil
	}
}
