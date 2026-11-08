package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// FetchSpec says where the files a corpus fetches on the host come from:
// every file its files.sha256 lists ("<sha256>  <path>" per line), read from
// a GitHub repository or out of remote zip archives at the value of a
// versions.env pin, and checked against its hash.
//
// A corpus whose input list holds {"id", "path"} entries installs each
// document at <testdata>/<list_root>/<id>, and every other listed file (a
// licence) beside them by its base name. Any other corpus installs a file
// at its path under the testdata root, rewritten by the first Map rule
// whose prefix it starts with.
type FetchSpec struct {
	// GitHub is the "<owner>/<repo>" the files are read from, at the
	// commit Pin names.
	GitHub string `json:"github,omitempty"`
	// Zip is the URL the archives are read from: a file's path is
	// "<archive>/<entry>", read out of <Zip>/<archive> through HTTP range
	// requests, so only the entries listed are downloaded. Pin names the
	// value that selects the archives (GOVDOCS_THREADS).
	Zip string `json:"zip,omitempty"`
	// Pin is the versions.env key the files are fetched at.
	Pin string `json:"pin"`
	// Map rewrites the paths of a corpus without a documents list.
	Map []FetchMap `json:"map,omitempty"`
}

// FetchMap rewrites the files whose path starts with Prefix: the rest of the
// path goes under To, relative to the corpus testdata root or, with Fonts,
// to the fixture fonts directory, where the converters read fonts from.
type FetchMap struct {
	Prefix string `json:"prefix"`
	To     string `json:"to"`
	Fonts  bool   `json:"fonts,omitempty"`
}

// githubRaw is the host the github fetches read files from; a test points
// it at a local server.
//
//nolint:gochecknoglobals // overridden by tests only
var githubRaw = "https://raw.githubusercontent.com/"

// fetchJobs is the default number of files downloaded at once, across every
// corpus a fetch reads: enough to keep a connection busy without tripping
// the rate limits of raw.githubusercontent.com.
const fetchJobs = 8

// fetchRetries bounds the attempts at one download. A transient failure
// (a reset connection, a 5xx answer) is retried after a growing pause; a
// hash mismatch or a 4xx answer is not.
const fetchRetries = 4

// fetchTimeout bounds one HTTP request.
const fetchTimeout = 5 * time.Minute

// maxFetchBytes bounds a file a fetch reads into memory. Anti-DoS guard, not
// a property of any source: the largest file listed is a few megabytes, and a
// font release a few tens.
const maxFetchBytes = 256 << 20

// fixtureFontsDir is where the converter features read their fonts, relative
// to the testdata directory.
const fixtureFontsDir = "fonts"

// errHashMismatch reports a download whose bytes are not the ones listed.
var errHashMismatch = errors.New("hash mismatch")

// errPermanent marks a download failure a retry cannot fix.
var errPermanent = errors.New("permanent")

// fetchFile is one file to download: where it comes from, its hash, and
// where it goes.
type fetchFile struct {
	corpus string
	path   string
	sha256 string
	dest   string
	read   func(ctx context.Context) ([]byte, error)
}

func runFetch(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
	corpus := fs.String("corpus", "", "Comma-separated corpora, sources or \"fonts\" to fetch (poi/docx, poi, wpt, fonts); empty fetches the fixture fonts and every corpus that declares a fetch")
	jobs := fs.Int("jobs", fetchJobs, "Files downloaded at once")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *jobs < 1 {
		fmt.Fprintln(os.Stderr, "fetch: --jobs must be at least 1")
		return 2
	}
	if err := fetch(ctx, splitCSV(*corpus), *jobs, http.DefaultClient); err != nil {
		fmt.Fprintf(os.Stderr, "fetch: %v\n", err)
		return 1
	}
	return 0
}

// fetchFontsName selects the fixture fonts in a fetch's list.
const fetchFontsName = "fonts"

// fetch downloads the fixture fonts and the files of every corpus that
// declares a fetch when selected is empty; else the fonts when it names
// "fonts", and the corpora it names or whose source it names.
func fetch(ctx context.Context, selected []string, jobs int, client *http.Client) error {
	pins, err := loadPins()
	if err != nil {
		return err
	}
	testdata := testdataDir()
	all := len(selected) == 0
	fonts := all || slices.Contains(selected, fetchFontsName)
	selected = slices.DeleteFunc(slices.Clone(selected), func(s string) bool { return s == fetchFontsName })
	if fonts {
		if err := fetchFonts(ctx, client, pins.Values, filepath.Join(testdata, fixtureFontsDir)); err != nil {
			return fmt.Errorf("fixture fonts: %w", err)
		}
	}
	if !all && len(selected) == 0 {
		return nil
	}
	ms, err := manifests()
	if err != nil {
		return err
	}
	chosen, err := fetchedCorpora(ms, selected)
	if err != nil {
		return err
	}
	var files []fetchFile
	zips := newZipCache(client)
	for _, m := range chosen {
		fs, err := corpusFetchFiles(m, pins.Values, testdata, client, zips)
		if err != nil {
			return fmt.Errorf("%s: %w", m.Name, err)
		}
		files = append(files, fs...)
	}
	counts, err := downloadAll(ctx, files, jobs)
	if err != nil {
		return err
	}
	for _, m := range chosen {
		if m.ProvisionedBy != "" {
			if err := writeFetchRecord(m, pins.Values[m.Fetch.Pin]); err != nil {
				return err
			}
		}
		c := counts[m.Name]
		fmt.Printf("  %s: %d files at %s=%s, %d downloaded, all hash-checked\n", m.Name, c.total, m.Fetch.Pin, pins.Values[m.Fetch.Pin], c.downloaded)
	}
	return nil
}

// testdataDir is the directory the corpora are provisioned under.
func testdataDir() string {
	return filepath.Dir((&Manifest{Source: "x"}).TestdataRoot())
}

// fetchedCorpora returns the manifests to fetch: those that declare a fetch
// and match selected, each a corpus name or a source (its first path
// segment). A selected name that matches nothing fetchable is an error.
func fetchedCorpora(ms []*Manifest, selected []string) ([]*Manifest, error) {
	var out []*Manifest
	matched := make(map[string]bool)
	for _, m := range ms {
		if m.Fetch == nil {
			continue
		}
		source, _, _ := strings.Cut(m.Name, "/")
		if len(selected) == 0 || slices.Contains(selected, m.Name) || slices.Contains(selected, source) {
			out = append(out, m)
			matched[m.Name], matched[source] = true, true
		}
	}
	for _, s := range selected {
		if !matched[s] {
			return nil, fmt.Errorf("no corpus named %q declares a fetch", s)
		}
	}
	return out, nil
}

// corpusFetchFiles lists the files of one corpus, each with its source and
// destination.
func corpusFetchFiles(m *Manifest, pins map[string]string, testdata string, client *http.Client, zips *zipCache) ([]fetchFile, error) {
	spec := m.Fetch
	value := pins[spec.Pin]
	if value == "" {
		return nil, fmt.Errorf("fetch pin %s is not pinned", spec.Pin)
	}
	if (spec.GitHub == "") == (spec.Zip == "") {
		return nil, errors.New("fetch names neither or both of github and zip")
	}
	listed, err := readHashList(filepath.Join(m.SourceDir(), "files.sha256"))
	if err != nil {
		return nil, err
	}
	dest, err := fetchDestinations(m, listed, testdata)
	if err != nil {
		return nil, err
	}
	files := make([]fetchFile, 0, len(listed))
	for _, l := range listed {
		f := fetchFile{corpus: m.Name, path: l.path, sha256: l.sha256, dest: dest[l.path]}
		switch {
		case spec.GitHub != "":
			u := githubRaw + spec.GitHub + "/" + value + "/" + escapePath(l.path)
			f.read = func(ctx context.Context) ([]byte, error) { return httpGet(ctx, client, u) }
		default:
			archive, entry, ok := strings.Cut(l.path, "/")
			if !ok {
				return nil, fmt.Errorf("%s: a zip path is <archive>/<entry>", l.path)
			}
			u := strings.TrimSuffix(spec.Zip, "/") + "/" + archive
			f.read = func(ctx context.Context) ([]byte, error) { return zips.read(ctx, u, entry) }
		}
		files = append(files, f)
	}
	return files, nil
}

// fetchDestinations maps each listed path to the file it is installed at.
func fetchDestinations(m *Manifest, listed []hashEntry, testdata string) (map[string]string, error) {
	root := m.TestdataRoot()
	out := make(map[string]string, len(listed))
	byPath := make(map[string]string)
	if m.Inputs != nil && m.Inputs.List != "" {
		docs, err := readDocumentList(filepath.Join(m.SourceDir(), m.Inputs.List))
		if err != nil {
			return nil, err
		}
		for _, d := range docs {
			if d.Path == "" {
				continue
			}
			if !validInputID(d.ID) {
				return nil, fmt.Errorf("%s: invalid id %q", m.Inputs.List, d.ID)
			}
			byPath[d.Path] = d.ID
		}
	}
	for _, l := range listed {
		switch {
		case len(byPath) > 0:
			listRoot := filepath.Join(root, filepath.FromSlash(m.Inputs.ListRoot))
			if id, ok := byPath[l.path]; ok {
				out[l.path] = filepath.Join(listRoot, filepath.FromSlash(id))
			} else {
				out[l.path] = filepath.Join(listRoot, path.Base(l.path))
			}
		default:
			out[l.path] = mapFetchPath(m.Fetch.Map, l.path, root, filepath.Join(testdata, fixtureFontsDir))
		}
	}
	return out, nil
}

// mapFetchPath rewrites p by the first rule whose prefix it starts with,
// under root or fonts; a path no rule matches stays at its place under root.
func mapFetchPath(rules []FetchMap, p, root, fonts string) string {
	for _, r := range rules {
		if rest, ok := strings.CutPrefix(p, r.Prefix); ok {
			base := root
			if r.Fonts {
				base = fonts
			}
			return filepath.Join(base, filepath.FromSlash(r.To+rest))
		}
	}
	return filepath.Join(root, filepath.FromSlash(p))
}

// hashEntry is one line of a files.sha256 list.
type hashEntry struct {
	sha256 string
	path   string
}

// readHashList reads a files.sha256 list: "<sha256>  <path>" per line,
// each path once.
func readHashList(p string) ([]hashEntry, error) {
	f, err := os.Open(p) //nolint:gosec // committed corpus list
	if err != nil {
		return nil, fmt.Errorf("read hash list: %w", err)
	}
	defer f.Close()
	var out []hashEntry
	seen := make(map[string]bool)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		digest, name, ok := strings.Cut(line, "  ")
		if !ok || len(digest) != sha256.Size*2 || name == "" || seen[name] || strings.Contains(name, "..") {
			return nil, fmt.Errorf("%s: malformed line %q", p, line)
		}
		seen[name] = true
		out = append(out, hashEntry{sha256: digest, path: name})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	return out, nil
}

// documentEntry is one entry of an input list.
type documentEntry struct {
	ID   string `json:"id"`
	Path string `json:"path,omitempty"`
}

func readDocumentList(p string) ([]documentEntry, error) {
	data, err := os.ReadFile(p) //nolint:gosec // committed corpus list
	if err != nil {
		return nil, fmt.Errorf("read input list: %w", err)
	}
	var docs []documentEntry
	if err := json.Unmarshal(data, &docs); err != nil {
		return nil, fmt.Errorf("decode %s: %w", p, err)
	}
	return docs, nil
}

// escapePath URL-escapes each segment of a "/"-separated path: some file
// names hold spaces and other characters a URL path cannot carry raw.
func escapePath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

// fetchCount counts one corpus's files and those a fetch downloaded.
type fetchCount struct {
	total, downloaded int
}

// downloadAll downloads every file not already on disk with its hash, jobs
// at a time, and returns the counts by corpus. The first failure cancels
// the rest.
func downloadAll(ctx context.Context, files []fetchFile, jobs int) (map[string]fetchCount, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	counts := make(map[string]fetchCount)
	var mu sync.Mutex
	work := make(chan fetchFile)
	var wg sync.WaitGroup
	for range jobs {
		wg.Go(func() {
			for f := range work {
				got, err := installFile(ctx, f)
				mu.Lock()
				c := counts[f.corpus]
				c.total++
				if got {
					c.downloaded++
				}
				counts[f.corpus] = c
				mu.Unlock()
				if err != nil {
					cancel(fmt.Errorf("%s: %s: %w", f.corpus, f.path, err))
				}
			}
		})
	}
feed:
	for _, f := range files {
		select {
		case work <- f:
		case <-ctx.Done():
			break feed
		}
	}
	close(work)
	wg.Wait()
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	return counts, nil
}

// installFile writes f at its destination unless a file with its hash is
// already there, and reports whether it downloaded it.
func installFile(ctx context.Context, f fetchFile) (bool, error) {
	if got, err := fileSHA256(f.dest); err == nil && got == f.sha256 {
		return false, nil
	}
	var data []byte
	var err error
	for attempt := range fetchRetries {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return false, context.Cause(ctx)
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
		}
		if data, err = f.read(ctx); err == nil || errors.Is(err, errPermanent) || ctx.Err() != nil {
			break
		}
	}
	if err != nil {
		return false, err
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != f.sha256 {
		return false, fmt.Errorf("%w: got %s, files.sha256 lists %s", errHashMismatch, got, f.sha256)
	}
	return true, writeFileAtomic(f.dest, data)
}

// writeFileAtomic writes data to p through a temporary file renamed into
// place, so an interrupted fetch never leaves a short file behind.
func writeFileAtomic(p string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(p), err)
	}
	tmp := p + ".part"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, p); err != nil {
		return fmt.Errorf("install %s: %w", p, err)
	}
	return nil
}

// httpGet reads the body of a GET of u, at most maxFetchBytes. A 4xx answer
// is permanent; any other failure may be retried.
func httpGet(ctx context.Context, client *http.Client, u string) ([]byte, error) {
	return httpRange(ctx, client, u, -1, 0)
}

// httpRange reads n bytes of u from offset off through a Range request, or
// the whole body when off is negative.
func httpRange(ctx context.Context, client *http.Client, u string, off, n int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errPermanent, err)
	}
	want := http.StatusOK
	if off >= 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", off, off+n-1))
		want = http.StatusPartialContent
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", u, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != want {
		err := fmt.Errorf("GET %s: %s", u, resp.Status)
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			err = fmt.Errorf("%w: %w", errPermanent, err)
		}
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes+1))
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", u, err)
	}
	if len(data) > maxFetchBytes {
		return nil, fmt.Errorf("%w: GET %s: more than %d bytes", errPermanent, u, maxFetchBytes)
	}
	return data, nil
}

// writeFetchRecord writes the versions.json a host-provisioned corpus's
// runs check: the pin its files were fetched at.
func writeFetchRecord(m *Manifest, value string) error {
	rec := map[string]any{
		"format": 1,
		"stages": map[string]any{"pdfs": map[string]any{
			"recorded_at": time.Now().UTC().Format(time.RFC3339),
			"pins":        map[string]string{m.Fetch.Pin: value},
		}},
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("encode versions record: %w", err)
	}
	return writeFileAtomic(filepath.Join(m.TestdataRoot(), versionRecordName), append(data, '\n'))
}
