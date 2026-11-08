package odf

import (
	"fmt"
	"io"
	"strings"

	"golang.org/x/text/encoding/htmlindex"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// charsetReader decodes an XML document whose declaration names an
// encoding other than UTF-8 (XML 1.0, 4.3.3): an ASCII name passes the
// bytes through, ASCII being a subset of UTF-8; another name the WHATWG
// Encoding Standard knows (ISO-8859-1, windows-1252, Shift_JIS...) is
// decoded to UTF-8 through golang.org/x/text. ODF 1.3, Part 2, 3.1 asks
// for UTF-8 or UTF-16, but producers write ASCII declarations, and
// LibreOffice reads any encoding its parser knows. An unknown name
// fails with office.ErrMalformed.
func charsetReader(label string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "us-ascii", "ascii", "iso-646-us", "iso646-us", "ansi_x3.4-1968", "utf8":
		return input, nil
	}
	enc, err := htmlindex.Get(label)
	if err != nil {
		return nil, fmt.Errorf("%w: the XML encoding %q is not supported", office.ErrMalformed, label)
	}
	return enc.NewDecoder().Reader(input), nil
}
