package compose

import "fmt"

// Version is a PDF version [Write] writes a file for: the version its
// header declares and the newest whose constructs it uses.
type Version string

// The versions [Write] targets.
const (
	// Version14 is PDF 1.4 (PDF Reference, third edition), the base of
	// PDF/A-1.
	Version14 Version = "1.4"
	// Version15 is PDF 1.5.
	Version15 Version = "1.5"
	// Version16 is PDF 1.6.
	Version16 Version = "1.6"
	// Version17 is PDF 1.7 (ISO 32000-1), the base of PDF/A-2, PDF/A-3 and
	// PDF/UA-1, and the default.
	Version17 Version = "1.7"
	// Version20 is PDF 2.0 (ISO 32000-2), the base of PDF/A-4 and PDF/UA-2.
	Version20 Version = "2.0"
)

// resolveVersion returns the version v stands for, [Version17] for the
// empty value, and an error wrapping [ErrInvalidVersion] for a version
// [Write] does not target.
func resolveVersion(v Version) (Version, error) {
	switch v {
	case "":
		return Version17, nil
	case Version14, Version15, Version16, Version17, Version20:
		return v, nil
	}
	return "", fmt.Errorf("%w: %q", ErrInvalidVersion, string(v))
}
