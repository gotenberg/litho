package html

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"  // registers GIF with image.Decode
	_ "image/jpeg" // registers JPEG with image.Decode
	_ "image/png"  // registers PNG with image.Decode
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"sync"

	_ "golang.org/x/image/bmp"  // registers BMP with image.Decode
	_ "golang.org/x/image/tiff" // registers TIFF with image.Decode
	_ "golang.org/x/image/webp" // registers WebP with image.Decode

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// rootPrefix is the path every URL of the document resolves under: the
// document sits at the root of [Options.Resources], and a URL is resolved
// as an absolute path below this prefix. A reference that leaves the root
// with ".." resolves outside the prefix, where RFC 3986, 5.2.4 would
// otherwise clamp it silently at the root, and is refused. The prefix is a
// dot segment no real directory under the root is reached through.
const rootPrefix = "/.litho-root/"

// documentURL is the URL the document itself resolves as.
const documentURL = rootPrefix + "index.html"

// Errors a resource reference fails with. They end up in warning messages.
var (
	errNoResources  = errors.New("no resources are given to read it from")
	errAbsoluteURL  = errors.New("absolute URLs are not fetched")
	errOutsideRoot  = errors.New("the path leaves the resources root")
	errBadDataURL   = errors.New("malformed data: URL")
	errTooLarge     = fmt.Errorf("the resource is larger than %d bytes", maxResourceBytes)
	errBudget       = fmt.Errorf("the conversion already read %d bytes of resources", maxTotalResourceBytes)
	errTooManyImgs  = fmt.Errorf("the document holds more than %d distinct images", limits.MaxConvertPictures)
	errTooManyPx    = errors.New("the image holds too many pixels")
	errEmptyPicture = errors.New("the image has no pixels")
)

// warningCode returns the warning code a resource error stands for.
func warningCode(err error) string {
	if code := networkCode(err); code != "" {
		return code
	}
	switch {
	case errors.Is(err, errNoResources), errors.Is(err, errAbsoluteURL), errors.Is(err, errOutsideRoot):
		return CodeResourceRefused
	case errors.Is(err, errTooLarge), errors.Is(err, errBudget), errors.Is(err, errTooManyImgs),
		errors.Is(err, errTooManyPx):
		return CodeResourceLimit
	default:
		return CodeResourceFailed
	}
}

// displayURL returns u as a warning names it: relative to the resources
// root.
func displayURL(u string) string {
	return strings.ReplaceAll(u, rootPrefix, "")
}

// resolveRef resolves ref, an attribute value, against base, a URL under
// [rootPrefix] or the document's absolute <base>. Leading and trailing
// ASCII whitespace is stripped (HTML Standard, 2.4.1). It returns "" for a
// reference that does not parse.
func resolveRef(base, ref string) string {
	ref = strings.Trim(ref, "\t\n\f\r ")
	b, err := url.Parse(base)
	if err != nil {
		return ""
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	return b.ResolveReference(r).String()
}

// resources reads the files the document names. It lives for one
// [Convert] call; the mutex makes it safe for the layout engine to call
// from any goroutine.
type resources struct {
	fsys   fs.FS
	client *http.Client

	mu       sync.Mutex
	read     int64
	pictures map[string]pictureResult
	// svg is what the SVG pictures share.
	svg svgState
	// requests counts the HTTP requests sent; network caches their
	// results by URL.
	requests int
	network  map[string]networkResult
}

// pictureResult is a decoded picture or why it failed.
type pictureResult struct {
	pic *layout.Picture
	err error
}

func newResources(fsys fs.FS) *resources {
	return &resources{fsys: fsys, pictures: map[string]pictureResult{}, network: map[string]networkResult{}}
}

// fetch returns the bytes u names: a data: URL's payload (RFC 2397), an
// http or https resource fetched through the client, or a file of the
// resources below [rootPrefix].
func (r *resources) fetch(ctx context.Context, u string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(u) >= 5 && strings.EqualFold(u[:5], "data:") {
		return decodeDataURL(u)
	}
	p, err := url.Parse(u)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", displayURL(u), err)
	}
	if p.Scheme != "" || p.Host != "" {
		return r.fetchAbsolute(ctx, p)
	}
	if r.fsys == nil {
		return nil, errNoResources
	}
	rel, ok := strings.CutPrefix(p.Path, rootPrefix)
	if !ok || rel == "" || !fs.ValidPath(rel) {
		return nil, errOutsideRoot
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.read >= maxTotalResourceBytes {
		return nil, errBudget
	}
	f, err := r.fsys.Open(rel)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", rel, err)
	}
	defer f.Close() // read-only: a close error loses nothing
	data, err := io.ReadAll(io.LimitReader(f, maxResourceBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", rel, err)
	}
	if len(data) > maxResourceBytes {
		return nil, errTooLarge
	}
	if r.read+int64(len(data)) > maxTotalResourceBytes {
		r.read = maxTotalResourceBytes
		return nil, errBudget
	}
	// A resource's bytes are held as long as what is built from them: a
	// JPEG is written as it is, a stylesheet's rules live with the cascade.
	if err := limits.BudgetFrom(ctx).ChargeUnits(int64(len(data)), 1, "resources"); err != nil {
		return nil, err
	}
	r.read += int64(len(data))
	return data, nil
}

// admit counts n bytes the caller gave, a user stylesheet, as a resource
// read: [errTooLarge] past [maxResourceBytes], [errBudget] past
// [maxTotalResourceBytes], and charged to the conversion's budget, since
// what is built from them lives with the cascade.
func (r *resources) admit(ctx context.Context, n int) error {
	if n > maxResourceBytes {
		return errTooLarge
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.read+int64(n) > maxTotalResourceBytes {
		r.read = maxTotalResourceBytes
		return errBudget
	}
	if err := limits.BudgetFrom(ctx).ChargeUnits(int64(n), 1, "resources"); err != nil {
		return err
	}
	r.read += int64(n)
	return nil
}

// decodeDataURL returns the payload of a data: URL (RFC 2397, 3):
// base64 when the media type part ends in ";base64", percent-encoded
// otherwise. The payload is no larger than the URL, which the source cap
// bounds.
func decodeDataURL(u string) ([]byte, error) {
	// A fragment is not part of the resource (Fetch Standard, 4.2).
	body, _, _ := strings.Cut(u[5:], "#")
	meta, payload, ok := strings.Cut(body, ",")
	if !ok {
		return nil, errBadDataURL
	}
	if strings.HasSuffix(strings.ToLower(strings.TrimSpace(meta)), ";base64") {
		// Whitespace inside base64 data is common in hand-written markup
		// and dropped by every browser (Fetch Standard, forgiving-base64
		// decode).
		clean := strings.Map(func(c rune) rune {
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' {
				return -1
			}
			return c
		}, payload)
		if un, err := url.PathUnescape(clean); err == nil {
			clean = un
		}
		clean = strings.TrimRight(clean, "=")
		data, err := base64.RawStdEncoding.DecodeString(clean)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", errBadDataURL, err)
		}
		return data, nil
	}
	data, err := url.PathUnescape(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errBadDataURL, err)
	}
	return []byte(data), nil
}

// picture returns the decoded picture u names, decoding it once per URL.
func (r *resources) picture(ctx context.Context, u string) (*layout.Picture, error) {
	r.mu.Lock()
	if res, ok := r.pictures[u]; ok {
		r.mu.Unlock()
		return res.pic, res.err
	}
	full := len(r.pictures) >= limits.MaxConvertPictures
	r.mu.Unlock()
	if full {
		return nil, errTooManyImgs
	}
	pic, err := r.decodePicture(ctx, u)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	r.mu.Lock()
	r.pictures[u] = pictureResult{pic: pic, err: err}
	r.mu.Unlock()
	return pic, err
}

// decodePicture reads and decodes the picture u names. A JPEG whose colour
// model the backend writes as DCTDecode is kept as its bytes; every other
// picture is decoded and encoded at once, keeping no decoded pixels.
func (r *resources) decodePicture(ctx context.Context, u string) (*layout.Picture, error) {
	f, err := r.fetchTyped(ctx, u)
	if err != nil {
		return nil, err
	}
	data := f.data
	if isSVG(u, data) || f.mediaType == "image/svg+xml" {
		return r.decodeSVG(ctx, u, data)
	}
	if name := unsupportedFormat(data, f.mediaType); name != "" {
		return nil, fmt.Errorf("%w: %s", errFormatUnsupported, name)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decoding the image header: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, errEmptyPicture
	}
	mem := limits.BudgetFrom(ctx)
	err = mem.CheckPicture(cfg.Width, cfg.Height)
	switch {
	case errors.Is(err, limits.ErrPictureTooLarge):
		return nil, fmt.Errorf("%w: %w", errTooManyPx, err)
	case err != nil:
		return nil, err
	}
	pic := &layout.Picture{Width: float64(cfg.Width) * css.Pt, Height: float64(cfg.Height) * css.Pt}
	orient := 1
	if format == "jpeg" {
		orient = jpegOrientation(data)
	}
	if orient >= 5 {
		// Orientations 5 to 8 turn the picture a quarter.
		pic.Width, pic.Height = pic.Height, pic.Width
	}
	if format == "jpeg" && orient == 1 && dctModel(cfg.ColorModel) {
		src, err := compose.JPEGImage(ctx, data)
		if err != nil {
			return nil, fmt.Errorf("reading the JPEG image: %w", err)
		}
		pic.Source = src
		return pic, nil
	}
	// A turned picture is encoded from the copy orientImage draws, gray
	// for a gray one and RGBA otherwise, which EncodeImage releases.
	model := cfg.ColorModel
	if orient > 1 && model != color.GrayModel {
		model = color.RGBAModel
	}
	charged, err := mem.ChargePicture(cfg.Width, cfg.Height, model)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		mem.Release(charged)
		return nil, fmt.Errorf("decoding the %s image: %w", format, err)
	}
	oriented, err := orientImage(ctx, img, orient)
	if err != nil {
		mem.Release(charged)
		return nil, err
	}
	src, err := compose.EncodeImage(ctx, oriented)
	if err != nil {
		return nil, fmt.Errorf("encoding the %s image: %w", format, err)
	}
	pic.Source = src
	return pic, nil
}

// dctModel reports a JPEG colour model the backend writes as a DCTDecode
// stream untouched (ISO 32000-1, 7.4.8): grey, YCbCr and CMYK.
func dctModel(m color.Model) bool {
	return m == color.GrayModel || m == color.YCbCrModel || m == color.CMYKModel
}

// isSVG reports a picture in SVG, by its URL's extension or media type or
// by an XML or <svg start.
func isSVG(u string, data []byte) bool {
	lu := strings.ToLower(u)
	if strings.HasPrefix(lu, "data:image/svg") {
		return true
	}
	if p, err := url.Parse(lu); err == nil && strings.HasSuffix(p.Path, ".svg") {
		return true
	}
	head := bytes.TrimLeft(data[:min(len(data), 512)], "\xef\xbb\xbf \t\r\n")
	return bytes.HasPrefix(head, []byte("<svg")) || bytes.HasPrefix(head, []byte("<?xml")) && bytes.Contains(data[:min(len(data), 4096)], []byte("<svg"))
}
