package compliance

import (
	"bytes"
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// checkObjectHeaderFormat validates that indirect object headers conform to
// PDF/A format requirements (6.1.8 / 6.1.9):
// - Object number and generation number separated by a single space (0x20)
// - Generation number and "obj" keyword separated by a single space
// - "obj" keyword followed by whitespace (EOL or space)
//
// This is a post-parse check that reads raw file bytes at xref offsets.
func checkObjectHeaderFormat(_ context.Context, doc *document.Document) []error {
	if doc.RawReader() == nil {
		return nil // Test mocks without file access.
	}

	for objNum, entry := range doc.Index.Entries {
		if entry.Free || entry.Type != 1 || entry.Offset <= 0 {
			continue // Skip free, compressed, or invalid entries.
		}

		// Read enough bytes for "N G obj" header (generous buffer).
		buf, err := doc.ReadFileBytes(entry.Offset, 40) //nolint:mnd // enough for "NNNNN GGGGG obj\n"
		if err != nil {
			continue
		}

		if err := validateObjHeader(buf, objNum); err != nil {
			return []error{err}
		}
	}
	return nil
}

// validateObjHeader checks that the raw bytes of an object header conform
// to the single-space-separated format: "N SP G SP obj EOL".
func validateObjHeader(buf []byte, objNum int) error {
	// Find "obj" keyword in the buffer.
	objIdx := bytes.Index(buf, []byte("obj"))
	if objIdx < 0 {
		return nil // Not a standard object header.
	}

	// Walk backwards from "obj" to find the separators.
	// Expected format: digits SP digits SP obj
	header := buf[:objIdx]

	// Must have at least "N G " (3 chars minimum).
	if len(header) < 3 { //nolint:mnd
		return nil
	}

	// The character immediately before "obj" must be exactly one space.
	if header[len(header)-1] != ' ' {
		return fmt.Errorf("%w: object %d has non-space before obj keyword",
			models.ErrInvalidFileHeader, objNum)
	}

	// Check that it's a SINGLE space (not multiple).
	if len(header) >= 2 && header[len(header)-2] == ' ' { //nolint:mnd
		return fmt.Errorf("%w: object %d has multiple spaces before obj keyword",
			models.ErrInvalidFileHeader, objNum)
	}

	// Find the space between object number and generation number.
	genStart := len(header) - 1 // position of space before "obj"
	// Walk backwards past generation digits.
	genEnd := genStart
	for genStart > 0 && buf[genStart-1] >= '0' && buf[genStart-1] <= '9' {
		genStart--
	}
	if genStart == genEnd {
		return nil // Can't parse generation number.
	}

	// The character before generation digits must be a single space.
	if genStart > 0 && buf[genStart-1] != ' ' {
		return fmt.Errorf("%w: object %d has non-space between obj num and gen",
			models.ErrInvalidFileHeader, objNum)
	}
	if genStart > 1 && buf[genStart-2] == ' ' {
		return fmt.Errorf("%w: object %d has multiple spaces between obj num and gen",
			models.ErrInvalidFileHeader, objNum)
	}

	// Check that "obj" is followed by a whitespace character.
	afterObj := objIdx + 3 //nolint:mnd // len("obj")
	if afterObj < len(buf) {
		c := buf[afterObj]
		if c != '\n' && c != '\r' && c != ' ' && c != '\t' {
			return fmt.Errorf("%w: object %d obj keyword not followed by whitespace",
				models.ErrInvalidFileHeader, objNum)
		}
	}

	return nil
}
