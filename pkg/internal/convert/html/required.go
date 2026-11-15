package html

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// loadFailed reports whether err, the error of fetching a resource, means
// the resource did not load: what a browser calls a failed load. Fetch
// Standard, 2.2.6 makes a network error of a response that never came: a
// scheme the fetch does not serve, a data: URL that does not decode (4.2),
// a request the transport did not complete. A stylesheet, a picture or a
// font whose response holds a status outside 2xx fails its load too (HTML
// Standard, 4.6.7.18 and 4.8.4.3; CSS Fonts 4, 4.8.1), as does a
// stylesheet served under another type than text/css (HTML Standard,
// 4.6.7.18). litho's equivalents are every error [resources.fetch] and
// [resources.fetchNetwork] return: a scheme or path the options refuse, no
// resources or no HTTP client to read it from, a file that does not open
// or read, a request that fails, a status outside 2xx, and the caps that
// stop a load before its bytes arrive whole (the size of one resource,
// the bytes of all, the requests sent), since the resource is then not
// there to draw.
//
// What arrives whole has loaded, whatever litho then makes of it: a
// picture that does not decode, in a format litho does not read or past
// the pixel or picture caps, a font program that does not parse. Neither
// is a refusal of the memory budget or the context's end a load that
// failed: they stop the conversion on their own ([limits.Stops]).
func loadFailed(err error) bool {
	return err != nil && !limits.Stops(err)
}

// failLoad ends the conversion with an error wrapping
// [ErrResourceUnavailable] that names the kind of resource, its URL u and
// err, the reason it did not load, when the resources are required and
// err is a failed load ([loadFailed]). It reports whether it did: the
// caller then stops, and [convert] returns that error. The first resource
// to fail is the one named.
func (r *resources) failLoad(kind, u string, err error) bool {
	if r.stop == nil || !loadFailed(err) {
		return false
	}
	r.stop(fmt.Errorf("%w: %s %s: %s", ErrResourceUnavailable, kind, shownURL(u), loadReason(err)))
	return true
}

// shownURL returns u as an error names it: relative to the resources
// root, a data: URL cut to its scheme and media type.
func shownURL(u string) string {
	s := displayURL(u)
	if len(s) > maxShownDataURL && strings.HasPrefix(strings.ToLower(s), "data:") {
		return s[:maxShownDataURL] + "..."
	}
	return s
}

// loadReason returns why a resource did not load, without the URL the
// error of a request or a file repeats: the resource is named once.
func loadReason(err error) string {
	var uerr *url.Error
	var perr *fs.PathError
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "the file does not exist"
	case errors.As(err, &perr):
		return "the file does not read: " + perr.Err.Error()
	case errors.As(err, &uerr):
		return "the request failed: " + uerr.Err.Error()
	}
	return err.Error()
}

// requireResources returns ctx with a cancel cause [Options.RequiredResources]
// stops it with, and o with that cause set ([Options.stop]); the caller
// cancels the context as it returns. Without RequiredResources it returns
// ctx and o unchanged and a no-op.
func requireResources(ctx context.Context, o Options) (context.Context, Options, func()) {
	if !o.RequiredResources {
		return ctx, o, func() {}
	}
	ctx, stop := context.WithCancelCause(ctx)
	o.stop = stop
	return ctx, o, func() { stop(nil) }
}

// resourceFailure returns the error a required resource stopped ctx with,
// nil when none did.
func resourceFailure(ctx context.Context) error {
	if cause := context.Cause(ctx); errors.Is(cause, ErrResourceUnavailable) {
		return cause
	}
	return nil
}
