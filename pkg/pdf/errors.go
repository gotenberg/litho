package pdf

import "errors"

// Exported errors for granular error handling by consumers.
var (
	// ErrInvalidPassword is returned when the provided password does not
	// unlock the encrypted document.
	ErrInvalidPassword = errors.New("invalid PDF password")

	// ErrNotAPDF is returned when the file does not start with a valid
	// %PDF- header.
	ErrNotAPDF = errors.New("not a valid PDF document (missing %PDF- header)")

	// ErrDamagedXRef is returned when the cross-reference table is
	// corrupted or missing and could not be reconstructed.
	ErrDamagedXRef = errors.New("damaged or missing cross-reference table")

	// ErrUnsupportedEncryption is returned when the document uses an
	// encryption algorithm or revision that this engine does not support.
	ErrUnsupportedEncryption = errors.New("unsupported encryption algorithm or revision")

	// ErrFontMissing is returned when a required font could not be
	// resolved or decoded during text extraction.
	ErrFontMissing = errors.New("required font is missing or failed to decode")

	// ErrRequiresOCR is returned when a page contains no digital text
	// and requires optical character recognition.
	ErrRequiresOCR = errors.New("page contains no digital text and requires optical character recognition")
)
