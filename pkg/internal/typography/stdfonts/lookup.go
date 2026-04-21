package stdfonts

import "strings"

// CMap returns a partial GID→Unicode table for a non-embedded standard
// font referenced by PostScript name. Returns nil when the name does
// not match any bundled table, signaling the caller to fall back to
// its usual CIDIdentityFallback offset path.
func CMap(baseFont string) map[uint16]rune {
	if baseFont == "" {
		return nil
	}
	// PDF BaseFont names often carry subset prefixes ("ABCDEF+Arial")
	// and style suffixes (",Bold", "-Italic", "MT", "PSMT"). Normalise
	// the name to its family keyword before matching.
	name := baseFont
	if i := strings.IndexByte(name, '+'); i >= 0 {
		name = name[i+1:]
	}
	name = strings.ToLower(name)
	switch {
	case strings.Contains(name, "arial"), strings.Contains(name, "helvetica"):
		return arialGIDToUnicode
	case strings.Contains(name, "calibri"):
		return calibriGIDToUnicode
	}
	return nil
}
