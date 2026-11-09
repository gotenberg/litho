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
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gotenberg/litho/test/integration/fixture"
)

// officeReferenceJobs is how many sources the LibreOffice route converts at
// once: it converts one document per LibreOffice instance and queues the
// rest, so more only waits longer.
const officeReferenceJobs = 4

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

// runReferences builds the reference PDFs of the Office and html corpora
// with the Gotenberg image versions.env pins (GOTENBERG_IMAGE), started on a
// free loopback port: the LibreOffice references of a corpus with
// [Manifest.References] into its testdata, the Chromium references of an
// html corpus into its source directory's references/, which git versions.
//
//	corpus references --corpus=gotenberg/docx,litho/html [--names=a,b]
//
// --names restricts the run to those references, keeping the others; a
// corpus holding none of them is left as it is.
func runReferences(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("references", flag.ExitOnError)
	corpus := fs.String("corpus", "", "Comma-separated corpora (empty = every Office and html corpus)")
	names := fs.String("names", "", "Comma- or space-separated references to build (empty = all)")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "parsing flags: %v\n", err)
		return 1
	}
	if err := references(ctx, splitCSV(*corpus), strings.FieldsFunc(*names, func(r rune) bool { return r == ',' || r == ' ' })); err != nil {
		fmt.Fprintf(os.Stderr, "references: %v\n", err)
		return 1
	}
	return 0
}

// referenceKind is how a corpus's references are built.
type referenceKind int

const (
	noReferences referenceKind = iota
	officeReferences
	chromiumReferences
)

// kindOfReferences returns how m's references are built.
func kindOfReferences(m *Manifest) referenceKind {
	switch {
	case m.References != nil:
		return officeReferences
	case m.CommittedPDFs == "references" && m.Inputs != nil:
		return chromiumReferences
	default:
		return noReferences
	}
}

func references(ctx context.Context, selected, only []string) error {
	pins, err := loadPins()
	if err != nil {
		return err
	}
	image := pins.Values["GOTENBERG_IMAGE"]
	if image == "" {
		return errors.New("GOTENBERG_IMAGE is not pinned")
	}
	var corpora []*Manifest
	if len(selected) == 0 {
		all, err := manifests()
		if err != nil {
			return err
		}
		for _, m := range all {
			if kindOfReferences(m) != noReferences {
				corpora = append(corpora, m)
			}
		}
	} else {
		for _, name := range selected {
			m, err := LoadManifest(name)
			if err != nil {
				return err
			}
			if kindOfReferences(m) == noReferences {
				return fmt.Errorf("%s builds no references", name)
			}
			corpora = append(corpora, m)
		}
	}
	if len(corpora) == 0 {
		return errors.New("no corpus builds references")
	}
	g, err := startGotenberg(ctx, image)
	if err != nil {
		return err
	}
	defer g.stop()
	for _, m := range corpora {
		var err error
		if kindOfReferences(m) == officeReferences {
			err = g.officeReferences(ctx, m, pins, only)
		} else {
			err = g.chromiumReferences(ctx, m, only)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", m.Name, err)
		}
	}
	return nil
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
		g.stop()
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
			g.stop()
			return nil, fmt.Errorf("%s answered no health check within %s", image, gotenbergStartTimeout)
		}
		select {
		case <-ctx.Done():
			g.stop()
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func (g *gotenberg) stop() {
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
// there is no file. The caller validates the file with the fixture package.
func readOptionFields(path string) (map[string]json.RawMessage, error) {
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

// chromiumReferences posts each sources/<name>/ directory of an html corpus
// to the Chromium route and writes the PDF to references/<name>.pdf of its
// source directory. A directory holding index.html goes to the HTML route
// with every sibling file; one holding index.md goes to the Markdown route
// with [fixture.MarkdownWrapper] as its index.html. The files options.json's
// header and footer name are posted as header.html and footer.html, and
// options.json is not posted.
//
// Chromium writes the time into each PDF, so two runs write other bytes: the
// PDFs are committed and rebuilt only when a source or the pin changes.
func (g *gotenberg) chromiumReferences(ctx context.Context, m *Manifest, only []string) error {
	sources := filepath.Join(m.SourceDir(), "sources")
	out := filepath.Join(m.SourceDir(), "references")
	entries, err := os.ReadDir(sources)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	wrap, err := os.MkdirTemp("", "litho-markdown-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(wrap)
	count := 0
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || (len(only) > 0 && !slices.Contains(only, name)) {
			continue
		}
		dir := filepath.Join(sources, name)
		src, err := fixture.Load(dir)
		if errors.Is(err, fixture.ErrNoDocument) {
			continue
		}
		if err != nil {
			return err
		}
		opts, err := readOptionFields(filepath.Join(dir, fixture.OptionsFile))
		if err != nil {
			return err
		}
		files, err := chromiumFiles(dir, src.Options)
		if err != nil {
			return err
		}
		route := "html"
		if src.Format == fixture.FormatMarkdown {
			route = "markdown"
			index := filepath.Join(wrap, name+".html")
			if err := os.WriteFile(index, []byte(fixture.MarkdownWrapper), 0o600); err != nil {
				return err
			}
			files = append(files, formFile{index, fixture.HTMLDocument})
		}
		if src.Options.Header != "" {
			files = append(files, formFile{filepath.Join(dir, src.Options.Header), "header.html"})
		}
		if src.Options.Footer != "" {
			files = append(files, formFile{filepath.Join(dir, src.Options.Footer), "footer.html"})
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
		status, body, err := g.post(ctx, "/forms/chromium/convert/"+route, files, fields)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if status != http.StatusOK {
			return fmt.Errorf("%s: the %s route answered %d: %s", name, route, status, firstLine(body))
		}
		if err := writeFileAtomic(filepath.Join(out, name+".pdf"), body); err != nil {
			return err
		}
		fmt.Printf("  %s: references/%s.pdf (%d bytes, %s route)\n", m.Name, name, len(body), route)
		count++
	}
	switch {
	case count == 0 && len(only) > 0:
		fmt.Printf("  %s: none of the names is a source here\n", m.Name)
	case count == 0:
		return fmt.Errorf("no sources/<name>/index.html or index.md under %s", sources)
	default:
		fmt.Printf("  %s: %d Chromium references built with %s\n", m.Name, count, g.image)
	}
	return nil
}

// chromiumFiles returns the files of a source directory posted with its
// document, sorted: every file but options.json and the header and footer
// templates, which go under their route names.
func chromiumFiles(dir string, opts fixture.Options) ([]formFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
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

// officeJob is one Office source to convert.
type officeJob struct {
	name, document, options, source string
}

// officeReferences posts each source of an Office corpus to the LibreOffice
// route and writes the PDF to <name>.pdf of the corpus's reference directory
// in its testdata: the committed sources/<name>/ directories, one document
// and an optional options.json each, and the documents the corpus's
// documents.json lists, fetched by corpus fetch, each named by its ID with
// "/" written "__". A key options.json leaves out keeps the route's default.
//
// The reference directory's manifest.json records, per reference, the image,
// the pin the fetched documents come from and its value, and the SHA-256 of
// the source, of its options.json and of the PDF, so corpus run refuses a
// reference built from another pin or from a source that changed since. A
// source the route refuses is recorded under "failed" with the route's
// answer, and gets no reference.
func (g *gotenberg) officeReferences(ctx context.Context, m *Manifest, pins *resolvedPins, only []string) error {
	jobs, err := officeJobs(m, only)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		if len(only) > 0 {
			fmt.Printf("  %s: none of the names is a source here\n", m.Name)
			return nil
		}
		return fmt.Errorf("no source under %s", m.SourceDir())
	}
	sourcePin, sourceValue := m.References.SourcePin, ""
	if sourcePin != "" {
		if sourceValue = pins.Values[sourcePin]; sourceValue == "" {
			return fmt.Errorf("%s is not pinned", sourcePin)
		}
	}
	version := gotenbergLibreOfficeVersion(ctx, g.image)
	out := filepath.Join(m.TestdataRoot(), filepath.FromSlash(m.References.Dir))
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}

	var (
		mu     sync.Mutex
		ok     = map[string]referenceRecord{}
		failed = map[string]referenceFailure{}
	)
	err = forEach(ctx, len(jobs), officeReferenceJobs, func(i int) error {
		j := jobs[i]
		srcSum, err := fileDigest(j.document)
		if err != nil {
			return err
		}
		optSum := ""
		var fields [][2]string
		if j.options != "" {
			if optSum, err = fileDigest(j.options); err != nil {
				return err
			}
			opts, err := readOptionFields(j.options)
			if err != nil {
				return err
			}
			for _, k := range fixture.OfficeOptionNames {
				if raw, ok := opts[k]; ok {
					if v := formValue(raw); v != "" {
						fields = append(fields, [2]string{k, v})
					}
				}
			}
		}
		status, body, err := g.post(ctx, "/forms/libreoffice/convert", []formFile{{j.document, filepath.Base(j.document)}}, fields)
		if err != nil {
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
				Image: g.image, SourcePin: sourcePin, SourcePinValue: sourceValue,
			}
			fmt.Printf("  %s: references/%s.pdf (%d bytes)\n", m.Name, j.name, len(body))
			return nil
		}
		_ = os.Remove(filepath.Join(out, j.name+".pdf")) //nolint:errcheck // a refused source may have had no reference
		answer := bytes.ReplaceAll(body[:min(len(body), maxReferenceAnswer)], []byte{0}, nil)
		failed[j.name] = referenceFailure{
			Source: j.source, SourceSHA256: srcSum, OptionsSHA256: optSum, Image: g.image,
			Status: strconv.Itoa(status), Answer: string(answer),
		}
		fmt.Fprintf(os.Stderr, "  %s: %s refused by the route (HTTP %d)\n", m.Name, j.name, status)
		return nil
	})
	if err != nil {
		return err
	}

	man := referenceManifest{Format: 1, Image: g.image, LibreOffice: version, References: ok, Failed: failed}
	path := filepath.Join(out, referenceManifestName)
	if len(only) > 0 {
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
	fmt.Printf("  %s: %d references built, %d refused, with %s (%s)\n", m.Name, len(ok), len(failed), g.image, version)
	return nil
}

// officeJobs lists the sources of an Office corpus, the ones only names
// when set.
func officeJobs(m *Manifest, only []string) ([]officeJob, error) {
	var jobs []officeJob
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
		src, err := fixture.LoadOffice(dir)
		if err != nil {
			return nil, err
		}
		options := ""
		if _, err := os.Stat(filepath.Join(dir, fixture.OptionsFile)); err == nil {
			options = filepath.Join(dir, fixture.OptionsFile)
		}
		jobs = append(jobs, officeJob{name, src.Document, options, "sources/" + name + "/" + filepath.Base(src.Document)})
	}
	if m.Inputs == nil || m.Inputs.List == "" {
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
			return nil, fmt.Errorf("%s is missing: run make corpus-fetch CORPUS_NAME=%s", doc, m.Name)
		}
		jobs = append(jobs, officeJob{name, doc, "", d.ID})
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].name < jobs[j].name })
	return jobs, nil
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
