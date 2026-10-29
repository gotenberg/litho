package signature

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type countingTransport struct{ calls int }

func (t *countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.calls++
	return http.DefaultTransport.RoundTrip(req)
}

// TestFetchUsesTheContextClient pins that a client named by the context
// carries every fetch, and that a context without one keeps the default.
func TestFetchUsesTheContextClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	transport := &countingTransport{}
	ctx := ContextWithHTTPClient(context.Background(), &http.Client{Transport: transport})

	if _, err := get(ctx, srv.URL, 16); err != nil {
		t.Fatalf("get: %v", err)
	}
	if _, err := post(ctx, srv.URL, "application/octet-stream", []byte("x"), 16); err != nil {
		t.Fatalf("post: %v", err)
	}
	if transport.calls != 2 {
		t.Fatalf("the context client carried %d fetches, want 2", transport.calls)
	}

	if _, err := get(context.Background(), srv.URL, 16); err != nil {
		t.Fatalf("get without a context client: %v", err)
	}
	if transport.calls != 2 {
		t.Fatalf("a context without a client used the context client")
	}

	if got := clientFor(ContextWithHTTPClient(context.Background(), nil)); got != httpClient {
		t.Fatal("a nil context client must keep the default client")
	}
}
