package html

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
)

// maxNetworkRequests caps the HTTP requests one conversion sends through
// [Options.HTTPClient]: 512. Anti-DoS guard, not spec-mandated: each
// request costs a round trip the conversion waits for, and a document
// naming thousands of distinct URLs would otherwise hold the conversion
// for as long as the server answers. A report page names a few dozen
// stylesheets, fonts and pictures; a product catalogue a few hundred.
// A URL named again is fetched once.
const maxNetworkRequests = 512

// Warning codes of the network resources.
const (
	// CodeResourceNoNetwork flags an http or https reference left out
	// because no [Options.HTTPClient] is given.
	CodeResourceNoNetwork = "resource-no-network"
	// CodeResourceHTTPStatus flags an http or https resource whose server
	// answered with a status outside 2xx; the message names the code.
	CodeResourceHTTPStatus = "resource-http-status"
	// CodeImageFormatUnsupported flags a picture in a format Chromium
	// draws and litho does not decode: AVIF, JPEG XL, ICO and CUR.
	CodeImageFormatUnsupported = "image-format-unsupported"
)

// Errors of the network resources. They end up in warning messages.
var (
	errNoNetwork          = errors.New("no HTTP client is given to fetch it")
	errFileURL            = errors.New("file: URLs are never fetched")
	errTooManyRequests    = fmt.Errorf("the conversion already sent %d HTTP requests", maxNetworkRequests)
	errFormatUnsupported  = errors.New("the image format is not decoded")
	errStylesheetMIMEType = errors.New("the stylesheet is not served as text/css")
)

// httpStatusError is a response whose status is outside 2xx.
type httpStatusError struct{ code int }

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("the server answered %d %s", e.code, http.StatusText(e.code))
}

// networkCode returns the warning code of a network error, "" for an
// error that is not one.
func networkCode(err error) string {
	var status *httpStatusError
	switch {
	case errors.Is(err, errNoNetwork):
		return CodeResourceNoNetwork
	case errors.As(err, &status):
		return CodeResourceHTTPStatus
	case errors.Is(err, errFileURL):
		return CodeResourceRefused
	case errors.Is(err, errTooManyRequests):
		return CodeResourceLimit
	case errors.Is(err, errFormatUnsupported):
		return CodeImageFormatUnsupported
	}
	return ""
}

// isNetworkURL reports an http or https URL, the schemes
// [Options.HTTPClient] fetches.
func isNetworkURL(u string) bool {
	p, err := url.Parse(u)
	return err == nil && isNetworkScheme(p.Scheme)
}

// isNetworkScheme reports the http and https schemes, in any case (RFC
// 3986, 3.1: schemes are case-insensitive).
func isNetworkScheme(scheme string) bool {
	return strings.EqualFold(scheme, "http") || strings.EqualFold(scheme, "https")
}

// documentBase returns the URL the document resolves its references
// against before any <base>: base when it is an http or https URL, without
// its fragment, else [documentURL], the root of [Options.Resources].
func documentBase(base string) string {
	p, err := url.Parse(base)
	if err != nil || !isNetworkScheme(p.Scheme) || p.Host == "" {
		return documentURL
	}
	p.Fragment, p.RawFragment = "", ""
	return p.String()
}

// fetched is what one fetch returned: the bytes, the media type of the
// response's Content-Type, "" when it has none or the resource is not
// fetched over HTTP, and the URL the bytes came from after redirects.
type fetched struct {
	data      []byte
	mediaType string
	url       string
}

// fetchAbsolute fetches p, an absolute URL with a scheme or a host: an
// http or https one through the client, a file: one never, any other
// refused.
func (r *resources) fetchAbsolute(ctx context.Context, p *url.URL) ([]byte, error) {
	switch {
	case isNetworkScheme(p.Scheme):
		f, err := r.fetchNetwork(ctx, p.String())
		if err != nil {
			return nil, err
		}
		return f.data, nil
	case strings.EqualFold(p.Scheme, "file"):
		return nil, errFileURL
	}
	return nil, errAbsoluteURL
}

// fetchTyped is [resources.fetch] with the response's media type and
// final URL, for a stylesheet or a picture that needs them.
func (r *resources) fetchTyped(ctx context.Context, u string) (fetched, error) {
	if isNetworkURL(u) {
		return r.fetchNetwork(ctx, u)
	}
	data, err := r.fetch(ctx, u)
	return fetched{data: data, url: u}, err
}

// fetchNetwork fetches u, an http or https URL, through the client with
// ctx, once per conversion: a URL named again returns what the first fetch
// returned. The body is read up to [maxResourceBytes] and counts against
// [maxTotalResourceBytes], as a file of the resources does; a status
// outside 2xx is an [httpStatusError]. Redirects, cookies, headers,
// timeouts and the hosts reached are the client's.
func (r *resources) fetchNetwork(ctx context.Context, u string) (fetched, error) {
	if err := ctx.Err(); err != nil {
		return fetched{}, err
	}
	if r.client == nil {
		return fetched{}, errNoNetwork
	}
	r.mu.Lock()
	if res, ok := r.network[u]; ok {
		r.mu.Unlock()
		return res.f, res.err
	}
	switch {
	case r.requests >= maxNetworkRequests:
		r.mu.Unlock()
		return fetched{}, errTooManyRequests
	case r.read >= maxTotalResourceBytes:
		r.mu.Unlock()
		return fetched{}, errBudget
	}
	r.requests++
	r.mu.Unlock()

	f, err := r.request(ctx, u)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fetched{}, ctxErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err == nil {
		if r.read+int64(len(f.data)) > maxTotalResourceBytes {
			r.read = maxTotalResourceBytes
			f, err = fetched{}, errBudget
		} else {
			r.read += int64(len(f.data))
		}
	}
	r.network[u] = networkResult{f: f, err: err}
	return f, err
}

// request sends one GET for u and reads its body up to
// [maxResourceBytes].
func (r *resources) request(ctx context.Context, u string) (fetched, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fetched{}, fmt.Errorf("requesting %s: %w", u, err)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return fetched{}, fmt.Errorf("fetching: %w", err)
	}
	defer resp.Body.Close() // read-only: a close error loses nothing
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fetched{}, &httpStatusError{code: resp.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResourceBytes+1))
	if err != nil {
		return fetched{}, fmt.Errorf("reading the response of %s: %w", u, err)
	}
	if len(data) > maxResourceBytes {
		return fetched{}, errTooLarge
	}
	f := fetched{data: data, url: u}
	if resp.Request != nil && resp.Request.URL != nil {
		// The resource's URL is the response's, after redirects (Fetch
		// Standard, 2.2.6, response URL).
		f.url = resp.Request.URL.String()
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		if mt, _, err := mime.ParseMediaType(ct); err == nil {
			f.mediaType = mt
		}
	}
	return f, nil
}

// networkResult is a cached fetch over HTTP.
type networkResult struct {
	f   fetched
	err error
}
