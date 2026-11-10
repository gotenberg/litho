package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gotenberg/litho/test/integration/fixture"
)

// microsoftRenderer is what a Microsoft reference records as the builder in
// place of an image: Office for the web, behind Microsoft Graph, has no
// version a caller can pin or read, so each reference records the day it
// was built instead (see [referenceRecord.BuiltAt]).
const microsoftRenderer = "microsoft-graph"

// msGraphClientIDEnv names the environment variable holding the client ID
// of the app registration corpus references signs in with: an app that
// allows personal Microsoft accounts and public client flows.
const msGraphClientIDEnv = "LITHO_CORPUS_MSGRAPH_CLIENT_ID"

// msGraphTokenEnv names the environment variable holding an access token
// to use as it is, in place of a sign-in: one Graph Explorer shows after
// signing in with Files.ReadWrite consented, for an account no directory
// holds, which cannot register an app. It lasts about an hour and is not
// refreshed: a run it outlives stops, keeping what it built.
const msGraphTokenEnv = "LITHO_CORPUS_MSGRAPH_TOKEN" //nolint:gosec // the variable's name, not a credential

// errMSGraphSignIn reports a token Graph no longer accepts: it stops the
// run rather than count every remaining source as refused.
var errMSGraphSignIn = errors.New("the Microsoft sign-in is no longer valid")

const (
	// msGraphAuthority is the sign-in endpoint of personal Microsoft
	// accounts.
	msGraphAuthority = "https://login.microsoftonline.com/consumers/oauth2/v2.0"
	// msGraphScope is what the conversion needs: uploading the document
	// to the signed-in account's OneDrive, and a refresh token.
	msGraphScope = "Files.ReadWrite offline_access"
	msGraphAPI   = "https://graph.microsoft.com/v1.0"
	// msGraphFolder is the OneDrive folder documents are uploaded to,
	// for the time of one conversion each.
	msGraphFolder = "litho-corpus"
	// msGraphMaxUpload is the largest document a single PUT uploads;
	// Graph wants an upload session above it, and no corpus document is
	// that large.
	msGraphMaxUpload = 4 << 20
	// msGraphAttempts is how many times a call answered 429 or 5xx is
	// tried: Graph throttles bursts and says how long to wait.
	msGraphAttempts = 5
	// msGraphMaxRetryAfter bounds the wait a throttled call honours.
	msGraphMaxRetryAfter = 2 * time.Minute
)

// msGraph converts Office documents to PDF with Office for the web, through
// Microsoft Graph's driveItem content ?format=pdf, as the signed-in
// personal account: upload, convert, delete.
type msGraph struct {
	clientID, cachePath string
	client              *http.Client
	// static is a token given in msGraphTokenEnv, never refreshed.
	static bool

	mu    sync.Mutex
	token msGraphToken
}

// msGraphToken is the cached sign-in: kept in the user's config directory,
// outside the repository, readable by the user alone.
type msGraphToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
}

// newMSGraph uses the token msGraphTokenEnv holds, or signs in: from the
// cached refresh token when it still works, otherwise with a device code
// the user enters in a browser.
func newMSGraph(ctx context.Context) (*msGraph, error) {
	if token := strings.TrimSpace(strings.TrimPrefix(os.Getenv(msGraphTokenEnv), "Bearer ")); token != "" {
		return &msGraph{
			client: &http.Client{Timeout: referenceTimeout},
			static: true,
			token:  msGraphToken{AccessToken: token},
		}, nil
	}
	clientID := os.Getenv(msGraphClientIDEnv)
	if clientID == "" {
		return nil, fmt.Errorf("the Microsoft references need an access token in %s or the client ID of an app registration in %s", msGraphTokenEnv, msGraphClientIDEnv)
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("locating the token cache: %w", err)
	}
	g := &msGraph{
		clientID:  clientID,
		cachePath: filepath.Join(dir, "litho-corpus", "msgraph-token.json"),
		client:    &http.Client{Timeout: referenceTimeout},
	}
	if data, err := os.ReadFile(g.cachePath); err == nil {
		if err := json.Unmarshal(data, &g.token); err != nil {
			return nil, fmt.Errorf("decoding %s: %w", g.cachePath, err)
		}
	}
	if g.token.RefreshToken != "" {
		if err := g.refresh(ctx); err == nil {
			return g, nil
		}
		fmt.Fprintln(os.Stderr, "references: the cached Microsoft sign-in expired, signing in again")
	}
	if err := g.deviceLogin(ctx); err != nil {
		return nil, fmt.Errorf("signing in to Microsoft: %w", err)
	}
	return g, nil
}

// msGraphTokenAnswer is the token endpoint's answer, a token or an error.
type msGraphTokenAnswer struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

// deviceLogin runs the device authorization grant: it prints the code and
// the page to enter it on, then polls until the user signs in.
func (g *msGraph) deviceLogin(ctx context.Context) error {
	var code struct {
		DeviceCode string `json:"device_code"`
		Message    string `json:"message"`
		Interval   int    `json:"interval"`
		ExpiresIn  int    `json:"expires_in"`
	}
	if err := g.form(ctx, msGraphAuthority+"/devicecode", url.Values{"client_id": {g.clientID}, "scope": {msGraphScope}}, &code); err != nil {
		return fmt.Errorf("requesting a device code: %w", err)
	}
	if code.DeviceCode == "" {
		return errors.New("the device code endpoint answered no code")
	}
	fmt.Fprintln(os.Stderr, code.Message)
	interval := time.Duration(max(code.Interval, 1)) * time.Second
	deadline := time.Now().Add(time.Duration(code.ExpiresIn) * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
		var answer msGraphTokenAnswer
		err := g.form(ctx, msGraphAuthority+"/token", url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"client_id":   {g.clientID},
			"device_code": {code.DeviceCode},
		}, &answer)
		if err != nil {
			return fmt.Errorf("polling the token endpoint: %w", err)
		}
		switch answer.Error {
		case "":
			return g.keep(answer)
		case "authorization_pending":
		case "slow_down":
			interval += 5 * time.Second
		default:
			return fmt.Errorf("%s: %s", answer.Error, answer.Description)
		}
	}
	return errors.New("the device code expired before the sign-in")
}

// refresh trades the refresh token for a new access token.
func (g *msGraph) refresh(ctx context.Context) error {
	var answer msGraphTokenAnswer
	err := g.form(ctx, msGraphAuthority+"/token", url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {g.clientID},
		"refresh_token": {g.token.RefreshToken},
		"scope":         {msGraphScope},
	}, &answer)
	if err != nil {
		return fmt.Errorf("refreshing the Microsoft sign-in: %w", err)
	}
	if answer.Error != "" {
		return fmt.Errorf("refreshing the Microsoft sign-in: %s: %s", answer.Error, answer.Description)
	}
	return g.keep(answer)
}

// keep stores a token answer and caches it, with a minute of margin on its
// expiry.
func (g *msGraph) keep(answer msGraphTokenAnswer) error {
	g.token.AccessToken = answer.AccessToken
	if answer.RefreshToken != "" {
		g.token.RefreshToken = answer.RefreshToken
	}
	g.token.Expiry = time.Now().Add(time.Duration(answer.ExpiresIn)*time.Second - time.Minute)
	data, err := json.Marshal(g.token) //nolint:gosec // the token cache, written 0600 in the user config directory
	if err != nil {
		return fmt.Errorf("encoding the token: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(g.cachePath), 0o700); err != nil {
		return fmt.Errorf("creating the token cache directory: %w", err)
	}
	if err := os.WriteFile(g.cachePath, data, 0o600); err != nil {
		return fmt.Errorf("caching the token: %w", err)
	}
	return nil
}

// form posts a form to the sign-in endpoint and decodes its JSON answer,
// which carries an error code rather than failing on a 400.
func (g *msGraph) form(ctx context.Context, endpoint string, values url.Values, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return fmt.Errorf("building the request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", endpoint, err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(into); err != nil {
		return fmt.Errorf("decoding the answer of %s (HTTP %d): %w", endpoint, resp.StatusCode, err)
	}
	return nil
}

// accessToken returns a valid access token, refreshing an expired one.
func (g *msGraph) accessToken(ctx context.Context) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.static && time.Now().After(g.token.Expiry) {
		if err := g.refresh(ctx); err != nil {
			return "", fmt.Errorf("%w: %w", errMSGraphSignIn, err)
		}
	}
	return g.token.AccessToken, nil
}

// call sends one Graph request with the access token, retrying a 429 or
// 5xx answer after the wait Graph asks for. A 401 refreshes the token once.
func (g *msGraph) call(ctx context.Context, method, u string, body []byte) (int, []byte, error) {
	var (
		status int
		data   []byte
	)
	for attempt := range msGraphAttempts {
		token, err := g.accessToken(ctx)
		if err != nil {
			return 0, nil, err
		}
		req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
		if err != nil {
			return 0, nil, fmt.Errorf("building %s %s: %w", method, u, err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if body != nil {
			req.Header.Set("Content-Type", "application/octet-stream")
		}
		resp, err := g.client.Do(req)
		if err != nil {
			return 0, nil, fmt.Errorf("%s %s: %w", method, u, err)
		}
		data, err = io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes))
		resp.Body.Close()
		if err != nil {
			return 0, nil, fmt.Errorf("reading the answer of %s %s: %w", method, u, err)
		}
		status = resp.StatusCode
		switch {
		case status == http.StatusUnauthorized && (g.static || attempt > 0):
			return status, data, fmt.Errorf("%w (HTTP 401: %s)", errMSGraphSignIn, data)
		case status == http.StatusUnauthorized:
			g.mu.Lock()
			g.token.Expiry = time.Time{}
			g.mu.Unlock()
			continue
		case status != http.StatusTooManyRequests && status < 500:
			return status, data, nil
		}
		wait := time.Duration(attempt+1) * 5 * time.Second
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
			wait = min(time.Duration(s)*time.Second, msGraphMaxRetryAfter)
		}
		select {
		case <-ctx.Done():
			return 0, nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	return status, data, nil
}

// convert uploads the document of one Office source under a name of its
// own, downloads its PDF and deletes it. A document Office for the web
// cannot convert answers the status Graph gave, and its error as the body.
// A source with conversion options gets none: Graph applies none.
func (g *msGraph) convert(ctx context.Context, _ string, j referenceJob) (int, []byte, error) {
	if j.options != "" {
		opts, err := readOptionFields(j.options)
		if err != nil {
			return 0, nil, err
		}
		for _, k := range fixture.OfficeOptionNames {
			if _, ok := opts[k]; ok {
				return 0, []byte("Microsoft Graph converts with no options, and the source sets " + k), nil
			}
		}
	}
	src, err := fixture.LoadOffice(j.path)
	if err != nil {
		return 0, nil, fmt.Errorf("loading %s: %w", j.path, err)
	}
	data, err := os.ReadFile(src.Document)
	if err != nil {
		return 0, nil, fmt.Errorf("reading %s: %w", src.Document, err)
	}
	if len(data) > msGraphMaxUpload {
		return 0, []byte(fmt.Sprintf("%d bytes, over the %d a single upload takes", len(data), msGraphMaxUpload)), nil
	}
	name := url.PathEscape(j.name + strings.ToLower(filepath.Ext(src.Document)))
	status, body, err := g.call(ctx, http.MethodPut, msGraphAPI+"/me/drive/root:/"+msGraphFolder+"/"+name+":/content", data)
	if err != nil || (status != http.StatusOK && status != http.StatusCreated) {
		return status, body, err
	}
	var item struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &item); err != nil || item.ID == "" {
		return 0, nil, fmt.Errorf("the upload of %s answered no item: %s", j.name, body)
	}
	// The conversion answers a redirect to a pre-authenticated download,
	// which the client follows without the token.
	status, pdf, err := g.call(ctx, http.MethodGet, msGraphAPI+"/me/drive/items/"+item.ID+"/content?format=pdf", nil)
	delStatus, delBody, delErr := g.call(ctx, http.MethodDelete, msGraphAPI+"/me/drive/items/"+item.ID, nil)
	switch {
	case delErr != nil:
		return 0, nil, fmt.Errorf("deleting the upload of %s: %w", j.name, delErr)
	case delStatus != http.StatusNoContent && delStatus != http.StatusNotFound:
		return 0, nil, fmt.Errorf("deleting the upload of %s: HTTP %d: %s", j.name, delStatus, delBody)
	}
	return status, pdf, err
}

// builder is what each reference records as having built it.
func (*msGraph) builder() string { return microsoftRenderer }

// version is empty: Office for the web reports none.
func (*msGraph) version(context.Context, string) string { return "" }

// close has nothing to stop.
func (*msGraph) close() {}
