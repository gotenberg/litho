package main

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// variableFontPins are the pins of every font release, the variable ones
// set to a commit "c1" and to the hashes of the given files.
func variableFontPins(files map[string][]byte) map[string]string {
	pins := map[string]string{}
	for _, k := range []string{
		"LIBERATION_FONTS_URL", "LIBERATION_FONTS_SHA256", "LIBERATION_FONTS_VERSION",
		"DEJAVU_FONTS_URL", "DEJAVU_FONTS_SHA256", "DEJAVU_FONTS_VERSION",
		"NOTO_CJK_SANS_VERSION", "NOTO_SANS_JP_SHA256", "NOTO_SANS_KR_SHA256", "NOTO_SANS_SC_SHA256", "NOTO_SANS_TC_SHA256",
		"NOTO_SANS_CJK_OTC_SHA256", "WEASYPRINT_VERSION", "WEASYPRINT_FONT_SHA256",
	} {
		pins[k] = "x"
	}
	pins["ROBOTO_FLEX_COMMIT"], pins["SOURCE_SANS_3_COMMIT"] = "c1", "c1"
	pins["ROBOTO_FLEX_SHA256"] = sha(files["/google/fonts/c1/ofl/robotoflex/RobotoFlex[GRAD,XOPQ,XTRA,YOPQ,YTAS,YTDE,YTFI,YTLC,YTUC,opsz,slnt,wdth,wght].ttf"])
	pins["ROBOTO_FLEX_LICENSE_SHA256"] = sha(files["/google/fonts/c1/ofl/robotoflex/OFL.txt"])
	pins["SOURCE_SANS_3_SHA256"] = sha(files["/adobe-fonts/source-sans/c1/VF/SourceSans3VF-Upright.otf"])
	pins["SOURCE_SANS_3_LICENSE_SHA256"] = sha(files["/adobe-fonts/source-sans/c1/LICENSE.md"])
	return pins
}

func TestFetchFonts_VariableFamiliesAloneWithAPlainUserAgent(t *testing.T) {
	files := map[string][]byte{
		"/google/fonts/c1/ofl/robotoflex/RobotoFlex[GRAD,XOPQ,XTRA,YOPQ,YTAS,YTDE,YTFI,YTLC,YTUC,opsz,slnt,wdth,wght].ttf": []byte("roboto flex"),
		"/google/fonts/c1/ofl/robotoflex/OFL.txt":                  []byte("OFL\r\n"),
		"/adobe-fonts/source-sans/c1/VF/SourceSans3VF-Upright.otf": []byte("source sans"),
		"/adobe-fonts/source-sans/c1/LICENSE.md":                   []byte("OFL too\r\n"),
	}
	var (
		mu     sync.Mutex
		agents []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		agents = append(agents, r.UserAgent())
		mu.Unlock()
		data, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data) //nolint:errcheck // the client reports a short body
	}))
	t.Cleanup(srv.Close)
	old := githubRaw
	githubRaw = srv.URL + "/"
	t.Cleanup(func() { githubRaw = old })

	dir := t.TempDir()
	pins := variableFontPins(files)
	if err := fetchFonts(t.Context(), srv.Client(), pins, dir, []string{"roboto-flex", "source-sans-3"}); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]string{
		"roboto-flex/RobotoFlex.ttf":              "roboto flex",
		"roboto-flex/OFL.txt":                     "OFL\r\n",
		"source-sans-3/SourceSans3VF-Upright.otf": "source sans",
		"source-sans-3/LICENSE.md":                "OFL too\r\n",
	} {
		if got, err := os.ReadFile(filepath.Join(dir, p)); err != nil || string(got) != want {
			t.Errorf("%s: %q, %v", p, got, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Errorf("installed %v, %v: want the two families alone", entries, err)
	}
	if len(agents) != 4 {
		t.Errorf("%d requests, want 4", len(agents))
	}
	for _, a := range agents {
		if !strings.HasPrefix(a, "Go-http-client/") || strings.Contains(strings.ToLower(a), "litho") {
			t.Errorf("User-Agent %q, want Go's default", a)
		}
	}

	// A family at its pins is not fetched again.
	before := len(agents)
	if err := fetchFonts(t.Context(), srv.Client(), pins, dir, []string{"roboto-flex"}); err != nil {
		t.Fatal(err)
	}
	if len(agents) != before {
		t.Errorf("%d requests for a family at its pins", len(agents)-before)
	}

	// A licence that is not the pinned one is refused, the family not
	// installed.
	pins["SOURCE_SANS_3_LICENSE_SHA256"] = strings.Repeat("0", 64)
	if err := fetchFonts(t.Context(), srv.Client(), pins, t.TempDir(), []string{"source-sans-3"}); !errors.Is(err, errHashMismatch) {
		t.Errorf("licence of another hash: %v", err)
	}
	if err := fetchFonts(t.Context(), srv.Client(), pins, dir, []string{"no-such-family"}); err == nil {
		t.Error("an unknown release was accepted")
	}
}

// TestFixtureFontLicences_AreTheFetchedTexts holds the licence texts
// committed beside the fixture fonts' list to the pinned downloads: the
// same bytes the fetch hash-checks.
func TestFixtureFontLicences_AreTheFetchedTexts(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", "corpus", versionsFileName))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := parsePins(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	pins := map[string]string{}
	for _, e := range entries {
		pins[e.Key] = e.Value
	}
	for file, key := range map[string]string{
		"roboto-flex-OFL.txt":       "ROBOTO_FLEX_LICENSE_SHA256",
		"source-sans-3-LICENSE.txt": "SOURCE_SANS_3_LICENSE_SHA256",
	} {
		got, err := fileSHA256(filepath.Join("..", "..", "corpus", "fonts", file))
		if err != nil {
			t.Fatal(err)
		}
		if got != pins[key] {
			t.Errorf("%s hashes to %s, %s pins %s", file, got, key, pins[key])
		}
	}
}
