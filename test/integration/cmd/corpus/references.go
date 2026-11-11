package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gotenberg/litho/test/integration/fixture"
)

// referenceJobsAtOnce is how many sources are converted at once: the
// LibreOffice route converts one document per LibreOffice instance and
// queues the rest, so more only waits longer.
const referenceJobsAtOnce = 4

// referenceAttempts is how many times a source is posted before its answer
// counts: a container under load can drop a connection or answer 503.
const referenceAttempts = 3

// gotenbergStartTimeout bounds the wait for the Gotenberg container's health
// route: Chromium and LibreOffice start lazily.
const gotenbergStartTimeout = 60 * time.Second

// referenceTimeout bounds one conversion. Anti-hang guard for test tooling:
// the route's own timeout is 30 seconds.
const referenceTimeout = 5 * time.Minute

// maxReferenceAnswer is how much of a refusal's body the reference manifest
// keeps.
const maxReferenceAnswer = 300

// runReferences builds the reference PDFs of the selected corpora into each
// corpus's testdata (see [ReferenceSpec]): the LibreOffice and Chromium
// ones with the Gotenberg image versions.env pins (GOTENBERG_IMAGE), started
// on a free loopback port, the Microsoft ones with Office for the web
// through Microsoft Graph, with the access token LITHO_CORPUS_MSGRAPH_TOKEN
// holds or signed in through the app registration
// LITHO_CORPUS_MSGRAPH_CLIENT_ID names (see [newMSGraph]).
//
//	corpus references [--corpus=litho/html,poi/*] [--fixtures=a,b]
//
// --fixtures restricts the run to those references, keeping the others; a
// corpus holding none of them is left as it is.
func runReferences(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("references", flag.ExitOnError)
	corpus := fs.String("corpus", "", corpusFlagUsage)
	fixtures := fs.String("fixtures", "", "Comma- or space-separated references to build (empty = all)")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "parsing flags: %v\n", err)
		return 1
	}
	ms, err := selectManifests(splitCSV(*corpus))
	if err != nil {
		fmt.Fprintf(os.Stderr, "references: %v\n", err)
		return 1
	}
	if err := references(ctx, ms, strings.FieldsFunc(*fixtures, func(r rune) bool { return r == ',' || r == ' ' })); err != nil {
		fmt.Fprintf(os.Stderr, "references: %v\n", err)
		return 1
	}
	return 0
}

// references builds the references of every corpus of ms that has some,
// the ones only names when set.
func references(ctx context.Context, ms []*Manifest, only []string) error {
	var corpora []*Manifest
	for _, m := range ms {
		if m.References != nil {
			corpora = append(corpora, m)
		}
	}
	if len(corpora) == 0 {
		fmt.Println("references: no selected corpus has references")
		return nil
	}
	pins, err := loadPins()
	if err != nil {
		return fmt.Errorf("loading the pins: %w", err)
	}
	b := &referenceBuilders{pins: pins}
	defer b.close()
	for _, m := range corpora {
		if err := b.build(ctx, m, only); err != nil {
			return fmt.Errorf("%s: %w", m.Name, err)
		}
	}
	return nil
}

// referenceConverter converts one source into its reference PDF: a
// Gotenberg container for the LibreOffice and Chromium references, Microsoft
// Graph for the Microsoft ones.
type referenceConverter interface {
	// convert returns the status and body of the conversion of j with the
	// route engine names.
	convert(ctx context.Context, engine string, j referenceJob) (int, []byte, error)
	// builder is what each reference records as having built it, the
	// value [checkReference] expects (see [referenceBuilder]).
	builder() string
	// version is the converting application's version, "" when unknown.
	version(ctx context.Context, engine string) string
	close()
}

// referenceBuilder is the builder a reference of engine must record: the
// pinned image, or [microsoftRenderer].
func referenceBuilder(engine string, pins map[string]string) string {
	if engine == engineMicrosoft {
		return microsoftRenderer
	}
	return pins["GOTENBERG_IMAGE"]
}

// referenceBuilders starts each converter the first time a corpus needs
// it, and keeps it for the next ones.
type referenceBuilders struct {
	pins *resolvedPins
	g    *gotenberg
	ms   *msGraph
}

// build builds the references of m, the ones only names when set.
func (b *referenceBuilders) build(ctx context.Context, m *Manifest, only []string) error {
	var conv referenceConverter
	if m.References.Engine == engineMicrosoft {
		if b.ms == nil {
			ms, err := newMSGraph(ctx)
			if err != nil {
				return err
			}
			b.ms = ms
		}
		conv = b.ms
	} else {
		if b.g == nil {
			image := b.pins.Values["GOTENBERG_IMAGE"]
			if image == "" {
				return errors.New("GOTENBERG_IMAGE is not pinned")
			}
			g, err := startGotenberg(ctx, image)
			if err != nil {
				return fmt.Errorf("starting %s: %w", image, err)
			}
			b.g = g
		}
		conv = b.g
	}
	return buildReferences(ctx, conv, m, b.pins, only)
}

func (b *referenceBuilders) close() {
	if b.g != nil {
		b.g.close()
	}
	if b.ms != nil {
		b.ms.close()
	}
}

// gotenberg is a running Gotenberg container.
type gotenberg struct {
	image, id, url string
	client         *http.Client
}

// startGotenberg starts image on a free loopback port and waits for its
// health route.
func startGotenberg(ctx context.Context, image string) (*gotenberg, error) {
	out, err := exec.CommandContext(ctx, "docker", "run", "-d", "--rm", "-p", "127.0.0.1::3000", image).Output() //nolint:gosec // the pinned image
	if err != nil {
		return nil, fmt.Errorf("docker run %s: %w", image, err)
	}
	g := &gotenberg{image: image, id: strings.TrimSpace(string(out)), client: &http.Client{Timeout: referenceTimeout}}
	port, err := exec.CommandContext(ctx, "docker", "port", g.id, "3000/tcp").Output() //nolint:gosec // the container just started
	if err != nil {
		g.close()
		return nil, fmt.Errorf("docker port: %w", err)
	}
	first, _, _ := strings.Cut(strings.TrimSpace(string(port)), "\n")
	g.url = "http://" + first
	deadline := time.Now().Add(gotenbergStartTimeout)
	for {
		resp, err := g.client.Get(g.url + "/health") //nolint:noctx // bounded by the deadline below
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return g, nil
			}
		}
		if time.Now().After(deadline) {
			g.close()
			return nil, fmt.Errorf("%s answered no health check within %s", image, gotenbergStartTimeout)
		}
		select {
		case <-ctx.Done():
			g.close()
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func (g *gotenberg) close() {
	_ = exec.Command("docker", "stop", g.id).Run() //nolint:errcheck,gosec // --rm removes it; a stopped container is the goal either way
}

// formFile is one file of a multipart post.
type formFile struct {
	path, name string
}

// post sends files and fields to route and returns the status and body. It
// retries a dropped connection and a 429 or 5xx answer.
func (g *gotenberg) post(ctx context.Context, route string, files []formFile, fields [][2]string) (int, []byte, error) {
	var (
		status int
		body   []byte
		err    error
	)
	for attempt := range referenceAttempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return 0, nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		status, body, err = g.postOnce(ctx, route, files, fields)
		if err == nil && status != http.StatusTooManyRequests && status < 500 {
			return status, body, nil
		}
	}
	return status, body, err
}

func (g *gotenberg) postOnce(ctx context.Context, route string, files []formFile, fields [][2]string) (int, []byte, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, f := range files {
		data, err := os.ReadFile(f.path)
		if err != nil {
			return 0, nil, err
		}
		part, err := w.CreateFormFile("files", f.name)
		if err != nil {
			return 0, nil, err
		}
		if _, err := part.Write(data); err != nil {
			return 0, nil, err
		}
	}
	for _, kv := range fields {
		if err := w.WriteField(kv[0], kv[1]); err != nil {
			return 0, nil, err
		}
	}
	if err := w.Close(); err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.url+route, &buf)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := g.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes))
	return resp.StatusCode, body, err
}

// formValue writes an options.json value as a form field value: a string as
// itself, an object as compact JSON (emulatedMediaFeatures), anything else as
// its JSON text.
func formValue(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var b bytes.Buffer
	if json.Compact(&b, raw) == nil {
		return b.String()
	}
	return string(raw)
}

// readOptionFields reads the keys of the options.json at path, none when
// path is "" or there is no file. The caller validates the file with the fixture package.
func readOptionFields(path string) (map[string]json.RawMessage, error) {
	if path == "" {
		return map[string]json.RawMessage{}, nil
	}
	data, err := os.ReadFile(path) //nolint:gosec // a committed source's options
	if errors.Is(err, os.ErrNotExist) {
		return map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return fields, nil
}

// chromiumDefaults are the fields of a source without options.json, in the
// order posted: A4 (210 by 297 mm), the print media type, backgrounds
// printed, since a document converter paints them, and the CSS page size
// preferred, since litho honoured @page size before the option existed. The
// margins are the route's (0.39 inch), left unset. Mirrors the defaults of
// [fixture.Options].
var chromiumDefaults = [][2]string{ //nolint:gochecknoglobals // static table
	{"paperWidth", "210mm"},
	{"paperHeight", "297mm"},
	{"emulatedMediaType", "print"},
	{"printBackground", "true"},
	{"preferCssPageSize", "true"},
}

// chromiumOptional are the fields posted only when options.json sets them.
var chromiumOptional = []string{ //nolint:gochecknoglobals // static table
	"marginTop", "marginRight", "marginBottom", "marginLeft", "landscape", "scale",
	"singlePage", "nativePages", "nativeBookmarks", "emulatedMediaFeatures",
}

// chromiumFiles returns the files of a source directory posted with its
// document, sorted: every file but options.json and the header and footer
// templates, which go under their route names.
func chromiumFiles(dir string, opts fixture.Options) ([]formFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}
	var files []formFile
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || name == fixture.OptionsFile || name == opts.Header || name == opts.Footer {
			continue
		}
		files = append(files, formFile{filepath.Join(dir, name), name})
	}
	return files, nil
}

// referenceJob is one source to convert: path is what its fixture points at
// (a source directory, or a fetched document), source how the reference
// manifest names it, options its options.json ("" without one).
type referenceJob struct {
	name, path, options, source string
}

// sourceDigest returns the SHA-256 a reference records for the source at
// path: an Office source's document, or, for Chromium, every file of the
// source directory but options.json, by name, since a page draws its
// stylesheets and images too.
func sourceDigest(engine, path string) (string, error) {
	if engine != engineChromium {
		src, err := fixture.LoadOffice(path)
		if err != nil {
			return "", err
		}
		return fileSHA256(src.Document)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, e := range entries {
		if e.IsDir() || e.Name() == fixture.OptionsFile {
			continue
		}
		sum, err := fileSHA256(filepath.Join(path, e.Name()))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%s\n", e.Name(), sum)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// convert posts one source to the route of engine and returns the status
// and body.
func (g *gotenberg) convert(ctx context.Context, engine string, j referenceJob) (int, []byte, error) {
	opts, err := readOptionFields(j.options)
	if err != nil {
		return 0, nil, err
	}
	if engine != engineChromium {
		src, err := fixture.LoadOffice(j.path)
		if err != nil {
			return 0, nil, err
		}
		var fields [][2]string
		for _, k := range fixture.OfficeOptionNames {
			if raw, ok := opts[k]; ok {
				if v := formValue(raw); v != "" {
					fields = append(fields, [2]string{k, v})
				}
			}
		}
		return g.post(ctx, "/forms/libreoffice/convert", []formFile{{src.Document, filepath.Base(src.Document)}}, fields)
	}
	src, err := fixture.Load(j.path)
	if err != nil {
		return 0, nil, err
	}
	files, err := chromiumFiles(j.path, src.Options)
	if err != nil {
		return 0, nil, err
	}
	route := "html"
	if src.Format == fixture.FormatMarkdown {
		route = "markdown"
		wrap, err := os.MkdirTemp("", "litho-markdown-")
		if err != nil {
			return 0, nil, err
		}
		defer os.RemoveAll(wrap)
		index := filepath.Join(wrap, "index.html")
		if err := os.WriteFile(index, []byte(fixture.MarkdownWrapper), 0o600); err != nil {
			return 0, nil, err
		}
		files = append(files, formFile{index, fixture.HTMLDocument})
	}
	if src.Options.Header != "" {
		files = append(files, formFile{filepath.Join(j.path, src.Options.Header), "header.html"})
	}
	if src.Options.Footer != "" {
		files = append(files, formFile{filepath.Join(j.path, src.Options.Footer), "footer.html"})
	}
	fields := make([][2]string, 0, len(chromiumDefaults)+len(chromiumOptional))
	for _, d := range chromiumDefaults {
		v := d[1]
		if raw, ok := opts[d[0]]; ok {
			v = formValue(raw)
		}
		fields = append(fields, [2]string{d[0], v})
	}
	for _, k := range chromiumOptional {
		if raw, ok := opts[k]; ok {
			if v := formValue(raw); v != "" {
				fields = append(fields, [2]string{k, v})
			}
		}
	}
	return g.post(ctx, "/forms/chromium/convert/"+route, files, fields)
}

// buildReferences converts each source of a corpus with the route its
// manifest names, through conv, and writes the PDF to <name>.pdf of the corpus's reference directory
// in its testdata: the committed sources/<name>/ directories (an Office
// document or an html page, and an optional options.json each), the
// documents a build corpus zips, and the documents the corpus's
// documents.json lists, fetched by corpus fetch, each named by its ID with
// "/" written "__". A key options.json leaves out keeps the route's default.
//
// The reference directory's manifest.json records, per reference, the image
// (or [microsoftRenderer], with the day it was built),
// the pin the fetched documents come from and its value, and the SHA-256 of
// the source (see [sourceDigest]), of its options.json and of the PDF, so
// corpus run refuses a reference built from another pin or from a source
// that changed since. A source the route refuses is recorded under "failed"
// with the route's answer, and gets no reference.
func buildReferences(ctx context.Context, conv referenceConverter, m *Manifest, pins *resolvedPins, only []string) error {
	if isBuildCorpus(m) || hasFlatPapers(m) {
		if err := buildCorpus(ctx, m); err != nil {
			return err
		}
	}
	jobs, err := referenceJobs(m, only)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		if len(only) > 0 {
			fmt.Printf("  %s: none of the fixtures is a source here\n", m.Name)
			return nil
		}
		return fmt.Errorf("no source under %s", m.SourceDir())
	}
	engine := m.References.Engine
	sourcePin, sourceValue := m.References.SourcePin, ""
	if sourcePin != "" {
		if sourceValue = pins.Values[sourcePin]; sourceValue == "" {
			return fmt.Errorf("%s is not pinned", sourcePin)
		}
	}
	version, builder := conv.version(ctx, engine), conv.builder()
	builtAt := ""
	if engine == engineMicrosoft {
		builtAt = time.Now().UTC().Format(time.DateOnly)
	}
	out := filepath.Join(m.TestdataRoot(), filepath.FromSlash(m.References.Dir))
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}

	var (
		mu     sync.Mutex
		ok     = map[string]referenceRecord{}
		failed = map[string]referenceFailure{}
	)
	runErr := forEach(ctx, len(jobs), referenceJobsAtOnce, func(i int) error {
		j := jobs[i]
		srcSum, err := sourceDigest(engine, j.path)
		if err != nil {
			return err
		}
		optSum := ""
		if j.options != "" {
			if optSum, err = fileDigest(j.options); err != nil {
				return err
			}
		}
		status, body, err := conv.convert(ctx, engine, j)
		switch {
		case errors.Is(err, errMSGraphSignIn):
			return err
		case err != nil:
			status, body = 0, []byte(err.Error())
		}
		mu.Lock()
		defer mu.Unlock()
		if status == http.StatusOK && bytes.HasPrefix(body, []byte("%PDF-")) {
			if err := writeFileAtomic(filepath.Join(out, j.name+".pdf"), body); err != nil {
				return err
			}
			sum := sha256.Sum256(body)
			ok[j.name] = referenceRecord{
				Source: j.source, SourceSHA256: srcSum, OptionsSHA256: optSum, PDFSHA256: hex.EncodeToString(sum[:]),
				Image: builder, SourcePin: sourcePin, SourcePinValue: sourceValue, BuiltAt: builtAt,
			}
			fmt.Printf("  %s: references/%s.pdf (%d bytes)\n", m.Name, j.name, len(body))
			return nil
		}
		_ = os.Remove(filepath.Join(out, j.name+".pdf")) //nolint:errcheck // a refused source may have had no reference
		answer := bytes.ReplaceAll(body[:min(len(body), maxReferenceAnswer)], []byte{0}, nil)
		failed[j.name] = referenceFailure{
			Source: j.source, SourceSHA256: srcSum, OptionsSHA256: optSum, Image: builder,
			Status: strconv.Itoa(status), Answer: string(answer),
		}
		if status == 0 {
			// No answer came back: the source was never sent, or the
			// request failed. The cause says which.
			fmt.Fprintf(os.Stderr, "  %s: %s has no reference from %s: %s\n", m.Name, j.name, builder, answer)
			return nil
		}
		fmt.Fprintf(os.Stderr, "  %s: %s refused by %s (HTTP %d)\n", m.Name, j.name, builder, status)
		return nil
	})
	// A sign-in that ran out stops the run: what it built is kept, so the
	// next run, with a new token, builds the rest.
	signIn := errors.Is(runErr, errMSGraphSignIn)
	if runErr != nil && !signIn {
		return runErr
	}

	man := referenceManifest{Format: 1, Image: builder, LibreOffice: version, References: ok, Failed: failed}
	path := filepath.Join(out, referenceManifestName)
	if len(only) > 0 || signIn {
		// A run of some sources keeps the entries of the others.
		if data, err := os.ReadFile(path); err == nil { //nolint:gosec // the manifest this command writes
			var old referenceManifest
			if err := json.Unmarshal(data, &old); err != nil {
				return fmt.Errorf("decode %s: %w", path, err)
			}
			for n, r := range old.References {
				if _, built := ok[n]; !built && !hasKey(failed, n) {
					man.References[n] = r
				}
			}
			for n, f := range old.Failed {
				if _, built := ok[n]; !built && !hasKey(failed, n) {
					man.Failed[n] = f
				}
			}
		}
	}
	data, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomic(path, append(data, '\n')); err != nil {
		return err
	}
	fmt.Printf("  %s: %d references built, %d refused, with %s\n", m.Name, len(ok), len(failed), builder)
	if signIn {
		// The run stops here, whatever it kept: the operator must learn
		// that the other sources have no reference.
		return fmt.Errorf("%w: %d of %d references built, the others not; sign in again and run the command anew", runErr, len(ok), len(jobs))
	}
	return nil
}

// referenceJobs lists the sources of a corpus with references, the ones
// only names when set: its committed sources/ directories, the documents a
// build corpus zips, the paper documents a corpus builds from its sources
// and the documents its list names. A source whose
// options.json borrows another fixture's reference gets none.
func referenceJobs(m *Manifest, only []string) ([]referenceJob, error) {
	var jobs []referenceJob
	if isBuildCorpus(m) {
		built, err := builtReferenceJobs(m, only)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", m.Name, err)
		}
		jobs = append(jobs, built...)
	}
	sources := filepath.Join(m.SourceDir(), "sources")
	entries, err := os.ReadDir(sources)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || (len(only) > 0 && !slices.Contains(only, name)) {
			continue
		}
		dir := filepath.Join(sources, name)
		if borrowsReference(dir) {
			continue
		}
		jobs = append(jobs, referenceJob{name, dir, optionsPath(dir), "sources/" + name})
	}
	jobs = append(jobs, flatPaperReferenceJobs(m, only)...)
	if m.Inputs == nil || m.Inputs.List == "" {
		sort.Slice(jobs, func(i, j int) bool { return jobs[i].name < jobs[j].name })
		return jobs, nil
	}
	docs, err := readDocumentList(filepath.Join(m.SourceDir(), filepath.FromSlash(m.Inputs.List)))
	if err != nil {
		return nil, err
	}
	fetched := filepath.Join(m.TestdataRoot(), filepath.FromSlash(m.Inputs.ListRoot))
	for _, d := range docs {
		name := strings.ReplaceAll(d.ID, "/", "__")
		if len(only) > 0 && !slices.Contains(only, name) {
			continue
		}
		doc := filepath.Join(fetched, filepath.FromSlash(d.ID))
		if _, err := os.Stat(doc); err != nil {
			return nil, fmt.Errorf("%s is missing: run make corpus-fetch CORPUS=%s", doc, m.Name)
		}
		jobs = append(jobs, referenceJob{name, doc, "", d.ID})
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].name < jobs[j].name })
	return jobs, nil
}

// borrowsReference reports whether an Office source directory's options.json
// names reference_of: another fixture's reference stands for it, and none is
// built for it (see fixture.OfficeOptions.ReferenceOf).
func borrowsReference(dir string) bool {
	if optionsPath(dir) == "" {
		return false
	}
	src, err := fixture.LoadOffice(dir)
	return err == nil && src.Options.ReferenceOf != ""
}

// optionsPath returns the options.json of a source directory, "" when it
// has none.
func optionsPath(dir string) string {
	p := filepath.Join(dir, fixture.OptionsFile)
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// builder is the image each reference records.
func (g *gotenberg) builder() string { return g.image }

// version is the image's LibreOffice version for a LibreOffice reference.
func (g *gotenberg) version(ctx context.Context, engine string) string {
	if engine == engineLibreOffice {
		return gotenbergLibreOfficeVersion(ctx, g.image)
	}
	return ""
}

// gotenbergLibreOfficeVersion is what the image's soffice reports, or "".
func gotenbergLibreOfficeVersion(ctx context.Context, image string) string {
	out, err := exec.CommandContext(ctx, "docker", "run", "--rm", "--entrypoint", "/usr/lib/libreoffice/program/soffice", image, "--headless", "--version").Output() //nolint:gosec // the pinned image
	if err != nil {
		return ""
	}
	first, _, _ := strings.Cut(string(out), "\n")
	return strings.TrimSpace(first)
}

func hasKey[V any](m map[string]V, k string) bool {
	_, ok := m[k]
	return ok
}

// builtReferenceJobs lists the documents a build corpus zips from its
// parts and its paper documents (see [paperDocs]), the ones only names when
// set: its hostile documents have no reference.
func builtReferenceJobs(m *Manifest, only []string) ([]referenceJob, error) {
	names, err := partsNames(m)
	if err != nil {
		return nil, err
	}
	names = append(names, paperDocNames(m.Name)...)
	var jobs []referenceJob
	for _, name := range names {
		if len(only) > 0 && !slices.Contains(only, name) {
			continue
		}
		dir := filepath.Join(buildOut(m), name)
		if _, err := fixture.LoadOffice(dir); err != nil {
			return nil, fmt.Errorf("%w (run make corpus-build CORPUS=%s)", err, m.Name)
		}
		if borrowsReference(dir) {
			continue
		}
		jobs = append(jobs, referenceJob{name, dir, optionsPath(dir), path.Join(m.Inputs.Build, name)})
	}
	return jobs, nil
}
