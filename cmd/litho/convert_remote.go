package main

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/gotenberg/litho/pkg/pdf"
)

// cliHTTPTimeout bounds one HTTP exchange of litho convert, the document's
// or a resource's, body included: 30 seconds. Anti-DoS guard, not
// spec-mandated: a server that never answers would otherwise hold the
// command forever. A page and its assets load in seconds.
const cliHTTPTimeout = 30 * time.Second

// isRemoteInput reports an http or https URL, which litho convert fetches.
func isRemoteInput(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Host != "" && (strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https"))
}

// cliHTTPClient returns the client litho convert fetches a remote document
// and the resources of a document on the network with: Go's default
// transport, redirects followed up to Go's default of 10, each exchange
// bounded by [cliHTTPTimeout]. The CLI fetches whatever the document
// names; a service embedding litho passes its own client, with its own
// allow-list, through [pdf.WithHTTPClient].
func cliHTTPClient() *http.Client {
	return &http.Client{Timeout: cliHTTPTimeout}
}

// fetchInput fetches the document at raw and returns its body, its format
// and its URL after redirects. The format is from, when set, else the
// response's Content-Type when it is text/html or text/markdown, else the
// URL path's extension, else html. The body is read by [pdf.Convert],
// which caps it at the input budget; the caller closes it.
func fetchInput(ctx context.Context, client *http.Client, raw string, from pdf.Format) (io.ReadCloser, pdf.Format, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, "", "", fmt.Errorf("requesting the document: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", "", fmt.Errorf("fetching the document: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		resp.Body.Close() //nolint:errcheck,gosec // the body is discarded
		return nil, "", "", fmt.Errorf("fetching the document: the server answered %s", resp.Status)
	}
	final := raw
	if resp.Request != nil && resp.Request.URL != nil {
		final = resp.Request.URL.String()
	}
	format := from
	if format == "" {
		format = remoteFormat(resp.Header.Get("Content-Type"), final)
	}
	return resp.Body, format, final, nil
}

// remoteFormat returns the format of a remote document from its
// Content-Type, then its URL's extension, html when neither tells.
func remoteFormat(contentType, rawURL string) pdf.Format {
	if mt, _, err := mime.ParseMediaType(contentType); err == nil {
		switch mt {
		case "text/html", "application/xhtml+xml":
			return pdf.FormatHTML
		case "text/markdown", "text/x-markdown":
			return pdf.FormatMarkdown
		}
	}
	if u, err := url.Parse(rawURL); err == nil {
		if f, ok := convertExtensions[strings.ToLower(path.Ext(u.Path))]; ok {
			return f
		}
	}
	return pdf.FormatHTML
}
