package odf

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// ErrExternalPicture reports a picture linked outside the package, which
// a converter does not fetch.
var ErrExternalPicture = errors.New("odf: the picture is linked outside the package")

// PictureBytes returns the bytes of the picture a draw:image holds (ODF
// 1.3, Part 3, 10.4.4): the package entry its xlink:href names, else its
// office:binary-data child in base64 (10.4.5), which a flat document uses.
// It fails with [ErrExternalPicture] for an href with a scheme, an error
// [IsNotExist] reports for an entry the package does not hold, the
// errors of [Package.ReadFile], and office.ErrMalformed for base64 that
// does not decode or a draw:image with neither.
func (p *Package) PictureBytes(ctx context.Context, img *Element) ([]byte, error) {
	if href := img.A(NSXLink, "href"); href != "" {
		if strings.Contains(href, "://") || strings.HasPrefix(href, "data:") {
			return nil, ErrExternalPicture
		}
		if p.Flat {
			return nil, fmt.Errorf("%w: %s", errNoEntry, href)
		}
		return p.ReadFile(ctx, href)
	}
	bin := img.Child(NSOffice, "binary-data")
	if bin == nil {
		return nil, fmt.Errorf("%w: a picture with neither an href nor binary data", office.ErrMalformed)
	}
	data, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(bin.TextContent()), ""))
	if err != nil {
		return nil, fmt.Errorf("%w: a picture's binary data does not decode: %w", office.ErrMalformed, err)
	}
	return data, nil
}

// MediaType returns the manifest's media type of the entry or directory
// name, "" when the manifest does not list it (ODF 1.3, Part 2, 4.3): an
// embedded object's directory, "Object 1/", is listed with its document's
// media type, application/vnd.oasis.opendocument.chart for a chart.
func (p *Package) MediaType(name string) string {
	name = cleanName(name)
	for _, e := range p.Manifest {
		if cleanName(e.FullPath) == name {
			return e.MediaType
		}
	}
	return ""
}

// MimeChart is the media type of an OpenDocument chart (ODF 1.3, Part 3,
// Appendix C).
const MimeChart = "application/vnd.oasis.opendocument.chart"
