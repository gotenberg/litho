package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Not parallel: it sets the token's environment variable.
func TestMSGraphCall_AnExpiredStaticTokenStopsTheRun(t *testing.T) { //nolint:paralleltest // t.Setenv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	t.Setenv(msGraphTokenEnv, "Bearer tok")
	g, err := newMSGraph(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := g.call(t.Context(), http.MethodGet, srv.URL, nil); !errors.Is(err, errMSGraphSignIn) {
		t.Errorf("call error %v, want errMSGraphSignIn", err)
	}
}
