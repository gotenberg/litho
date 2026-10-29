package pdf

import (
	"context"
	"net/http"

	"github.com/gotenberg/litho/pkg/internal/signature"
)

// ContextWithHTTPClient returns a copy of ctx that makes litho reach timestamp
// authorities, OCSP responders, and CRL and certificate servers through client.
// Pass the returned context to [Writer.WriteTo] or [Pipeline.WriteTo] when it
// signs with a timestamp or long-term validation data, or timestamps, to route those requests through a proxy, restrict
// the hosts they may reach, or set their timeout.
//
// Without it, or with a nil client, litho uses its own client, which times out
// each exchange after a minute. The client's own timeout replaces that one.
func ContextWithHTTPClient(ctx context.Context, client *http.Client) context.Context {
	return signature.ContextWithHTTPClient(ctx, client)
}
