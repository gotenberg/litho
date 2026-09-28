package signature

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// fetchTimeout bounds one HTTP exchange with a timestamp authority, an OCSP
// responder or a CRL or certificate server. Not spec-mandated: it keeps a
// server that accepts the connection and never answers from holding a signing
// call forever when the caller's ctx has no deadline, and a minute is far
// longer than such a service takes to answer.
const fetchTimeout = time.Minute

// httpClient is the client litho reaches timestamp authorities, OCSP
// responders and CRL and certificate servers with.
var httpClient = &http.Client{Timeout: fetchTimeout}

// post sends body to rawURL as a POST of contentType and returns the answer,
// refusing a status other than 200 and an answer of more than limit bytes.
func post(ctx context.Context, rawURL, contentType string, body []byte, limit int64) ([]byte, error) {
	if err := httpURL(rawURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building the request to %s: %w", rawURL, err)
	}
	req.Header.Set("Content-Type", contentType)
	return do(req, limit)
}

// httpURL refuses a URL litho does not fetch: one of a scheme other than http
// or https, such as the ldap URLs some certificates list beside their http
// ones.
func httpURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parsing %q: %w", rawURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%q is not an http or https URL", u.Redacted())
	}
	return nil
}

// redacted returns rawURL with any password it carries masked, for an error
// message.
func redacted(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "an unparsable URL"
	}
	return u.Redacted()
}

// do sends req and reads its answer (see [post]). The answer's content type
// is not checked: its DER is, by whoever reads it.
func do(req *http.Request, limit int64) ([]byte, error) {
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reaching %s: %w", req.URL.Redacted(), err)
	}
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err := resp.Body.Close(); err != nil && readErr == nil {
		readErr = err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", req.URL.Redacted(), resp.Status)
	}
	if readErr != nil {
		return nil, fmt.Errorf("reading the answer of %s: %w", req.URL.Redacted(), readErr)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s answered with more than %d bytes", req.URL.Redacted(), limit)
	}
	return data, nil
}
