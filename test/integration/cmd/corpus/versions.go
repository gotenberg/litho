package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// versionsFileName names the pins file under test/integration/corpus/. It
// holds every tool version and corpus source revision provisioning uses.
const versionsFileName = "versions.env"

// versionRecordName names the record provisioning writes at the root of each
// corpus's testdata directory: the versions every stage ran with.
const versionRecordName = "versions.json"

// versionRecordFormat is the only versions.json layout this tool reads.
const versionRecordFormat = 1

// pdfsStage is the record key of the download stage, which the entrypoint
// writes under the name of the directory it fills.
const pdfsStage = "pdfs"

// veraPDFHeaderBytes bounds how much of a veraPDF JSON report is read to find
// the build header. The header is the report's first object, a few hundred
// bytes long; the reports themselves run to 17 MB, so reading them whole to
// learn one version would be waste.
const veraPDFHeaderBytes = 4096

//nolint:gochecknoglobals // static syntax of versions.env keys
var pinKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// pinValuePattern keeps a value safe to pass as a build argument, to source
// from sh inside the image, and to print on a command line without quoting.
//
//nolint:gochecknoglobals // static syntax of versions.env values
var pinValuePattern = regexp.MustCompile(`^[A-Za-z0-9._+:@,/-]+$`)

//nolint:gochecknoglobals // static header pattern of a veraPDF JSON report
var veraPDFCorePattern = regexp.MustCompile(`"id"\s*:\s*"core"\s*,\s*"version"\s*:\s*"([^"]+)"`)

// digestPins pairs each image digest pin with the pins that name the image it
// pins. Docker resolves a reference that carries both a tag and a digest by
// the digest alone, so overriding the version while keeping the digest would
// install the old build under the new name. An override of any named pin
// without the digest therefore drops the digest, and the image is pulled by
// tag.
//
//nolint:gochecknoglobals // static pairing table
var digestPins = map[string][]string{
	"VERAPDF_IMAGE_DIGEST": {"VERAPDF_VERSION"},
	"NODE_IMAGE_DIGEST":    {"NODE_VERSION", "NODE_DEBIAN_RELEASE"},
}

// localToolPins are the pins of tools the host runs rather than the image:
// the live checks and the litho fixture builders use them, and `corpus tools`
// compares them. They are not build arguments.
//
//nolint:gochecknoglobals // static key set
var localToolPins = map[string]bool{
	"QPDF_VERSION":    true,
	"POPPLER_VERSION": true,
	"PIKEPDF_VERSION": true,
}

// stagePins maps each directory a provisioning stage fills to the pins its
// output depends on: every tool that ran to produce it. A golden recorded
// with another value of any of them is stale. The keys mirror the
// record_stage calls in corpus/entrypoint.sh.
//
//nolint:gochecknoglobals // static dependency table
var stagePins = map[string][]string{
	"goldens/text":               {"PDFJS_REVISION", "NODE_VERSION"},
	"goldens/metadata":           {"PDFJS_REVISION", "NODE_VERSION"},
	"goldens/images":             {"PDFJS_REVISION", "NODE_VERSION"},
	"goldens/compliance":         {"VERAPDF_VERSION", "VERAPDF_IMAGE_DIGEST", "JRE_VERSION"},
	"goldens/bookmarks":          {"PDFCPU_VERSION"},
	"goldens/merges":             {"PDFCPU_VERSION"},
	"goldens/merge-empty":        {"PDFCPU_VERSION"},
	"goldens/facturx-validity":   {"MUSTANG_VERSION", "JRE_VERSION"},
	"goldens/facturx-schematron": {"KOSIT_VERSION", "XRECHNUNG_CONFIG_TAG", "XRECHNUNG_CONFIG_ASSET", "EN16931_VERSION", "MUSTANG_VERSION", "JRE_VERSION"},
}

// sourcePins maps each corpus source directory to the pins its PDFs come
// from. The litho corpus is committed in-tree, so git versions it.
//
//nolint:gochecknoglobals // static dependency table
var sourcePins = map[string][]string{
	"pdfjs":   {"PDFJS_REVISION"},
	"verapdf": {"VERAPDF_CORPUS_REVISION"},
	"govdocs": {"GOVDOCS_THREADS"},
	"pdf20":   {"PDF20_REVISION"},
	"pdfcpu":  {"PDFCPU_CORPUS_REVISION"},
	"facturx": {"FACTURX_CORPUS_REVISION"},
	"litho":   nil,
}

// pinEntry is one KEY=VALUE line of versions.env.
type pinEntry struct {
	Key   string
	Value string
}

// parsePins reads versions.env: one KEY=VALUE per line, blank lines and lines
// starting with `#` ignored. Every key is unique and every value non-empty.
func parsePins(r io.Reader) ([]pinEntry, error) {
	var entries []pinEntry
	seen := make(map[string]int)
	scanner := bufio.NewScanner(r)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		key, value, ok := strings.Cut(text, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: %q is not KEY=VALUE", line, text)
		}
		if !pinKeyPattern.MatchString(key) {
			return nil, fmt.Errorf("line %d: key %q is not upper-case letters, digits and underscores", line, key)
		}
		if prev, dup := seen[key]; dup {
			return nil, fmt.Errorf("line %d: %s repeats line %d", line, key, prev)
		}
		if err := checkPinValue(key, value); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		seen[key] = line
		entries = append(entries, pinEntry{Key: key, Value: value})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading pins: %w", err)
	}
	if len(entries) == 0 {
		return nil, errors.New("no KEY=VALUE entry")
	}
	return entries, nil
}

// checkPinValue rejects an empty value or one carrying a character a build
// argument, a sourced shell file or a printed command line would mangle.
func checkPinValue(key, value string) error {
	if value == "" {
		return fmt.Errorf("%s has no value", key)
	}
	if !pinValuePattern.MatchString(value) {
		return fmt.Errorf("%s=%q carries a character outside [A-Za-z0-9._+:@,/-]", key, value)
	}
	return nil
}

// resolvedPins is versions.env after the environment overrides.
type resolvedPins struct {
	// Entries keeps the file order, so build arguments come out the same way
	// on every run.
	Entries []pinEntry

	// Values maps each key to its resolved value. A digest dropped by an
	// override resolves to "".
	Values map[string]string

	// Notices says, one line each, which pins the environment overrode and
	// which digests the overrides dropped.
	Notices []string
}

// resolvePins applies environment overrides to the parsed file. lookup is
// [os.LookupEnv] outside tests. A variable set to the value the file already
// holds is no override. A digest pin may be overridden to "" to pull its
// image by tag; every other override is checked like a file value.
func resolvePins(file []pinEntry, lookup func(string) (string, bool)) (*resolvedPins, error) {
	res := &resolvedPins{Values: make(map[string]string, len(file))}
	overridden := make(map[string]bool)
	for _, e := range file {
		value := e.Value
		if env, ok := lookup(e.Key); ok && env != e.Value {
			_, isDigest := digestPins[e.Key]
			if env != "" || !isDigest {
				if err := checkPinValue(e.Key, env); err != nil {
					return nil, fmt.Errorf("environment override: %w", err)
				}
			}
			value = env
			overridden[e.Key] = true
			res.Notices = append(res.Notices,
				fmt.Sprintf("%s=%s from the environment (versions.env pins %s)", e.Key, printablePin(env), e.Value))
		}
		res.Values[e.Key] = value
	}

	digests := make([]string, 0, len(digestPins))
	for digest := range digestPins {
		digests = append(digests, digest)
	}
	sort.Strings(digests)
	for _, digest := range digests {
		if overridden[digest] || res.Values[digest] == "" {
			continue
		}
		for _, named := range digestPins[digest] {
			if overridden[named] {
				res.Values[digest] = ""
				res.Notices = append(res.Notices,
					fmt.Sprintf("%s dropped: %s is overridden, and the digest pins a build of the image versions.env names, so the image is pulled by tag", digest, named))
				break
			}
		}
	}

	for _, e := range file {
		res.Entries = append(res.Entries, pinEntry{Key: e.Key, Value: res.Values[e.Key]})
	}
	return res, nil
}

// printablePin renders an empty value visibly in a message.
func printablePin(v string) string {
	if v == "" {
		return "(none)"
	}
	return v
}

// buildArgs returns the `docker build` arguments carrying every pin the image
// consumes, in file order. A dropped digest travels as an empty value so the
// Dockerfile's reference falls back to the tag.
func (p *resolvedPins) buildArgs() []string {
	args := make([]string, 0, 2*len(p.Entries))
	for _, e := range p.Entries {
		if localToolPins[e.Key] {
			continue
		}
		args = append(args, "--build-arg", e.Key+"="+e.Value)
	}
	return args
}

// loadPins reads versions.env from the corpus directory and applies the
// environment overrides.
func loadPins() (*resolvedPins, error) {
	root := corpusRoot()
	if root == "" {
		return nil, errors.New("no corpus directory found")
	}
	p := filepath.Join(root, versionsFileName)
	f, err := os.Open(p) //nolint:gosec // fixed file under the corpus directory
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", p, err)
	}
	defer f.Close()
	file, err := parsePins(f)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	pins, err := resolvePins(file, os.LookupEnv)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	return pins, nil
}

// versionRecord is testdata/<corpus>/versions.json: what each provisioning
// stage ran with, keyed by the directory it filled.
type versionRecord struct {
	Format int                    `json:"format"`
	Stages map[string]stageRecord `json:"stages"`
}

// stageRecord is one stage's entry in versions.json.
type stageRecord struct {
	RecordedAt string            `json:"recorded_at"`
	Pins       map[string]string `json:"pins"`
	Tools      map[string]string `json:"tools,omitempty"`

	// VeraPDFBuild is the release header of a compliance report the stage
	// wrote: veraPDF's own account of the build that made the goldens.
	VeraPDFBuild []veraPDFRelease `json:"verapdf_build,omitempty"`
}

// veraPDFRelease is one component of a veraPDF report's build header.
type veraPDFRelease struct {
	ID        string `json:"id"`
	Version   string `json:"version"`
	BuildDate int64  `json:"buildDate"`
}

// loadVersionRecord reads a corpus's versions.json. A missing file returns an
// error wrapping [os.ErrNotExist].
func loadVersionRecord(testdataRoot string) (*versionRecord, error) {
	p := filepath.Join(testdataRoot, versionRecordName)
	data, err := os.ReadFile(p) //nolint:gosec // fixed file under the corpus testdata root
	if err != nil {
		return nil, err
	}
	var rec versionRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("decode %s: %w", p, err)
	}
	if rec.Format != versionRecordFormat {
		return nil, fmt.Errorf("%s: unsupported format %d (want %d)", p, rec.Format, versionRecordFormat)
	}
	return &rec, nil
}

// versionCheck is the outcome of comparing one corpus's testdata against the
// pins.
type versionCheck struct {
	// Refusals are goldens known to come from other versions than the pins.
	Refusals []string

	// Warnings are goldens whose versions are unknown.
	Warnings []string

	// Remedy is the command that re-provisions what the refusals name.
	Remedy string
}

// checkCorpusVersions compares the versions a corpus's testdata was
// provisioned with against the pins, for the download stage and the golden
// directories the given features read.
//
// Two sources are read. versions.json, written by the entrypoint, says what
// every stage ran with. A veraPDF report also names its own build in its
// header, so compliance goldens are checked against VERAPDF_VERSION even when
// they predate versions.json. Testdata provisioned before versions.json
// existed has no record for anything else: its versions are unknown, which is
// a warning rather than a refusal, since the only way to learn them is to
// re-provision.
func checkCorpusVersions(m *Manifest, testdataRoot string, features []string, pins *resolvedPins) versionCheck {
	var out versionCheck
	if info, err := os.Stat(testdataRoot); err != nil || !info.IsDir() {
		return out
	}

	stages := corpusStages(m, features)
	stale := make(map[string]bool)

	rec, err := loadVersionRecord(testdataRoot)
	switch {
	case errors.Is(err, os.ErrNotExist):
		out.Warnings = append(out.Warnings, fmt.Sprintf(
			"%s: %s has no %s, so it was provisioned before versions were pinned and the tools its goldens came from are unknown",
			m.Name, testdataRoot, versionRecordName))
	case err != nil:
		out.Refusals = append(out.Refusals, fmt.Sprintf("%s: %v", m.Name, err))
		stale[pdfsStage] = true
	default:
		for _, stage := range stages {
			keys := stageKeys(m, stage)
			if len(keys) == 0 {
				continue
			}
			sr, ok := rec.Stages[stage]
			if !ok {
				out.Warnings = append(out.Warnings, fmt.Sprintf(
					"%s: %s records no versions for %s, so the tools it came from are unknown", m.Name, versionRecordName, stage))
				continue
			}
			for _, key := range keys {
				if got, want := sr.Pins[key], pins.Values[key]; got != want {
					out.Refusals = append(out.Refusals, fmt.Sprintf(
						"%s: %s was provisioned with %s=%s, the pin is %s",
						m.Name, stage, key, printablePin(got), printablePin(want)))
					stale[stage] = true
				}
			}
		}
	}

	for _, stage := range stages {
		if stage != "goldens/compliance" || stale[stage] {
			continue
		}
		refusals := checkVeraPDFReports(m.Name, filepath.Join(testdataRoot, filepath.FromSlash(stage)), pins.Values["VERAPDF_VERSION"])
		if len(refusals) > 0 {
			out.Refusals = append(out.Refusals, refusals...)
			stale[stage] = true
		}
	}

	if len(out.Refusals) > 0 {
		out.Remedy = reprovisionCommand(m, testdataRoot, stale)
	}
	return out
}

// corpusStages returns the record keys to check: the download stage and the
// golden directories of the features the run reads, deduplicated and sorted
// after the download stage.
func corpusStages(m *Manifest, features []string) []string {
	set := make(map[string]bool)
	for _, f := range features {
		rel, ok := m.Goldens[f]
		if !ok {
			continue
		}
		set[path.Clean(filepath.ToSlash(rel))] = true
	}
	stages := make([]string, 0, len(set)+1)
	for s := range set {
		stages = append(stages, s)
	}
	sort.Strings(stages)
	return append([]string{pdfsStage}, stages...)
}

// stageKeys returns the pins a stage's output depends on.
func stageKeys(m *Manifest, stage string) []string {
	if stage == pdfsStage {
		return sourcePins[m.Source]
	}
	return stagePins[stage]
}

// checkVeraPDFReports reads the build header of every report in a compliance
// golden directory and refuses the ones whose veraPDF core is not the pinned
// version. A report whose header cannot be read is left to the compliance
// feature, which fails on it with a better message.
func checkVeraPDFReports(corpus, dir, want string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	byVersion := make(map[string][]string)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		core, err := veraPDFReportCore(filepath.Join(dir, e.Name()))
		if err != nil || core == want {
			continue
		}
		byVersion[core] = append(byVersion[core], e.Name())
	}
	versions := make([]string, 0, len(byVersion))
	for v := range byVersion {
		versions = append(versions, v)
	}
	sort.Strings(versions)
	refusals := make([]string, 0, len(versions))
	for _, v := range versions {
		files := byVersion[v]
		sort.Strings(files)
		refusals = append(refusals, fmt.Sprintf(
			"%s: %d compliance golden(s) report veraPDF core %s, versions.env pins VERAPDF_VERSION=%s (%s)",
			corpus, len(files), v, want, strings.Join(files, ", ")))
	}
	return refusals
}

// veraPDFReportCore returns the core version a veraPDF JSON report names in
// its build header.
func veraPDFReportCore(p string) (string, error) {
	f, err := os.Open(p) //nolint:gosec // golden file under the corpus testdata root
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, veraPDFHeaderBytes)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", fmt.Errorf("read %s: %w", p, err)
	}
	m := veraPDFCorePattern.FindSubmatch(buf[:n])
	if m == nil {
		return "", fmt.Errorf("%s: no veraPDF core version in the first %d bytes", p, veraPDFHeaderBytes)
	}
	return string(m[1]), nil
}

// reprovisionCommand returns the shell command that regenerates the stale
// stages. A stale download stage means other PDFs, so the whole testdata
// directory goes; otherwise only the stale golden directories do, and the
// entrypoint regenerates exactly those.
func reprovisionCommand(m *Manifest, testdataRoot string, stale map[string]bool) string {
	var targets []string
	if stale[pdfsStage] {
		targets = []string{testdataRoot}
	} else {
		for stage := range stale {
			targets = append(targets, filepath.Join(testdataRoot, filepath.FromSlash(stage)))
		}
		sort.Strings(targets)
	}
	return fmt.Sprintf("rm -rf %s && make corpus-init CORPUS_NAME=%s", strings.Join(targets, " "), m.Name)
}

// loadManifests loads the manifest of every named corpus, skipping the ones
// that fail: runCorpus reports the same failure for them.
func loadManifests(corpora []string) []*Manifest {
	manifests := make([]*Manifest, 0, len(corpora))
	for _, name := range corpora {
		if m, err := LoadManifest(name); err == nil {
			manifests = append(manifests, m)
		}
	}
	return manifests
}

// verifyVersions checks every corpus a run scores and reports to w. It
// returns false when a golden was recorded with other versions than the pins
// and allowDrift is off, in which case nothing may be scored. rootOf locates a
// corpus's testdata, which is [Manifest.TestdataRoot] outside tests.
func verifyVersions(w io.Writer, manifests []*Manifest, rootOf func(*Manifest) string, features []string, pins *resolvedPins, allowDrift bool) bool {
	for _, n := range pins.Notices {
		fmt.Fprintf(w, "versions: %s\n", n)
	}
	ok := true
	for _, m := range manifests {
		res := checkCorpusVersions(m, rootOf(m), features, pins)
		for _, line := range res.Warnings {
			fmt.Fprintf(w, "warning: %s\n", line)
		}
		if len(res.Refusals) == 0 {
			continue
		}
		prefix := "stale"
		if allowDrift {
			prefix = "stale (scored anyway, --allow-version-drift)"
		}
		for _, line := range res.Refusals {
			fmt.Fprintf(w, "%s: %s\n", prefix, line)
		}
		fmt.Fprintf(w, "  re-provision: %s\n", res.Remedy)
		if !allowDrift {
			ok = false
		}
	}
	if !ok {
		fmt.Fprintln(w, "refusing to score goldens made with other versions than test/integration/corpus/versions.env pins; "+
			"re-provision them, or pass --allow-version-drift (CORPUS_ALLOW_VERSION_DRIFT=1) to score anyway")
	}
	return ok
}

// featureNames returns the names of the features a run selects.
func featureNames(filter []string) []string {
	features := selectedFeatures(filter)
	names := make([]string, 0, len(features))
	for _, f := range features {
		names = append(names, f.Name())
	}
	return names
}

// runVersions prints the resolved pins, one KEY=VALUE per line, or with
// --build-args the `docker build` arguments that carry them.
func runVersions(args []string) int {
	fs := flag.NewFlagSet("versions", flag.ExitOnError)
	asBuildArgs := fs.Bool("build-args", false, "Print the docker build arguments instead of KEY=VALUE lines")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "parsing flags: %v\n", err)
		return 1
	}
	pins, err := loadPins()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	for _, n := range pins.Notices {
		fmt.Fprintf(os.Stderr, "versions: %s\n", n)
	}
	if *asBuildArgs {
		fmt.Println(strings.Join(pins.buildArgs(), " "))
		return 0
	}
	for _, e := range pins.Entries {
		fmt.Printf("%s=%s\n", e.Key, e.Value)
	}
	return 0
}
