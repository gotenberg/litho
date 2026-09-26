package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// corpusSourceDir is the provisioning directory seen from this package.
const corpusSourceDir = "../../corpus"

func readPinsFile(t *testing.T) []pinEntry {
	t.Helper()
	f, err := os.Open(filepath.Join(corpusSourceDir, versionsFileName))
	if err != nil {
		t.Fatalf("open versions.env: %v", err)
	}
	defer f.Close()
	entries, err := parsePins(f)
	if err != nil {
		t.Fatalf("parse versions.env: %v", err)
	}
	return entries
}

func noEnv(string) (string, bool) { return "", false }

func envOf(vars map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := vars[k]
		return v, ok
	}
}

func TestParsePins(t *testing.T) {
	t.Parallel()

	good := "# comment\n\nA_VERSION=1.2.3\n  B_REVISION=0123abc  \nC=sha256:ab,cd+1/x@y\n"
	entries, err := parsePins(strings.NewReader(good))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []pinEntry{{"A_VERSION", "1.2.3"}, {"B_REVISION", "0123abc"}, {"C", "sha256:ab,cd+1/x@y"}}
	if !slices.Equal(entries, want) {
		t.Fatalf("entries = %v, want %v", entries, want)
	}

	for _, tc := range []struct {
		name, in, wantErr string
	}{
		{"no equals", "A_VERSION 1\n", "not KEY=VALUE"},
		{"lower-case key", "a_version=1\n", "upper-case"},
		{"empty value", "A=\n", "has no value"},
		{"space in value", "A=1 2\n", "character outside"},
		{"quote in value", "A=\"1\"\n", "character outside"},
		{"shell expansion", "A=$(id)\n", "character outside"},
		{"duplicate", "A=1\nA=2\n", "repeats line 1"},
		{"empty file", "# nothing\n", "no KEY=VALUE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parsePins(strings.NewReader(tc.in))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("parsePins(%q) error = %v, want one containing %q", tc.in, err, tc.wantErr)
			}
		})
	}
}

// TestVersionsFilePinsReleasesNotBranches keeps versions.env to the forms the
// provisioning scripts rely on. A corpus revision that is not a full SHA must
// be a tag, which fetch-revision.sh verifies against the remote; the pinned
// ones are all SHAs, and a branch name here would pass that check only if a
// tag of the same name existed.
func TestVersionsFilePinsReleasesNotBranches(t *testing.T) {
	t.Parallel()

	sha := regexp.MustCompile(`^[0-9a-f]{40}$`)
	digest := regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	for _, e := range readPinsFile(t) {
		switch {
		case strings.HasSuffix(e.Key, "_REVISION"):
			if !sha.MatchString(e.Value) {
				t.Errorf("%s=%s is not a full commit SHA", e.Key, e.Value)
			}
			if slices.Contains([]string{"main", "master", "staging", "develop", "HEAD"}, e.Value) {
				t.Errorf("%s names a branch", e.Key)
			}
		case strings.HasSuffix(e.Key, "_DIGEST"):
			if !digest.MatchString(e.Value) {
				t.Errorf("%s=%s is not a sha256 digest", e.Key, e.Value)
			}
			if _, paired := digestPins[e.Key]; !paired {
				t.Errorf("%s is a digest with no entry in digestPins, so overriding its version would keep the old build", e.Key)
			}
		}
	}
}

// TestVersionsFilePinsVeraPDF1302 pins the veraPDF release the litho goldens
// and every live check use. The verapdf and facturx goldens came from 1.30.1,
// the build verapdf/cli:latest carried when they were captured, and that gap
// is what the pin exists to close.
func TestVersionsFilePinsVeraPDF1302(t *testing.T) {
	t.Parallel()

	pins, err := resolvePins(readPinsFile(t), noEnv)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got := pins.Values["VERAPDF_VERSION"]; got != "1.30.2" {
		t.Errorf("VERAPDF_VERSION = %q, want 1.30.2", got)
	}
	if got := pins.Values["VERAPDF_IMAGE_DIGEST"]; got == "" {
		t.Error("VERAPDF_IMAGE_DIGEST is empty, so the tag alone picks the build")
	}
}

// TestPinDependencyTablesNameRealPins catches a typo in stagePins,
// sourcePins, digestPins or localToolPins: a key no pin carries compares ""
// with "" forever and never refuses anything.
func TestPinDependencyTablesNameRealPins(t *testing.T) {
	t.Parallel()

	keys := make(map[string]bool)
	for _, e := range readPinsFile(t) {
		keys[e.Key] = true
	}
	check := func(table, key string) {
		if !keys[key] {
			t.Errorf("%s names %s, which versions.env does not pin", table, key)
		}
	}
	for stage, list := range stagePins {
		for _, k := range list {
			check("stagePins["+stage+"]", k)
		}
	}
	for source, list := range sourcePins {
		for _, k := range list {
			check("sourcePins["+source+"]", k)
		}
	}
	for digest, named := range digestPins {
		check("digestPins", digest)
		for _, k := range named {
			check("digestPins["+digest+"]", k)
		}
	}
	for k := range localToolPins {
		check("localToolPins", k)
	}
}

func TestResolvePinsOverrides(t *testing.T) {
	t.Parallel()

	file := []pinEntry{
		{"VERAPDF_VERSION", "1.30.2"},
		{"VERAPDF_IMAGE_DIGEST", "sha256:aaaa"},
		{"NODE_VERSION", "24.21.0"},
		{"NODE_DEBIAN_RELEASE", "bookworm"},
		{"NODE_IMAGE_DIGEST", "sha256:bbbb"},
		{"PDFCPU_VERSION", "0.12.1"},
	}

	t.Run("no environment keeps the file", func(t *testing.T) {
		t.Parallel()
		pins, err := resolvePins(file, noEnv)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(pins.Entries, file) || len(pins.Notices) != 0 {
			t.Fatalf("entries %v notices %v, want the file unchanged and no notice", pins.Entries, pins.Notices)
		}
	})

	t.Run("an equal variable is no override", func(t *testing.T) {
		t.Parallel()
		pins, err := resolvePins(file, envOf(map[string]string{"VERAPDF_VERSION": "1.30.2"}))
		if err != nil {
			t.Fatal(err)
		}
		if len(pins.Notices) != 0 || pins.Values["VERAPDF_IMAGE_DIGEST"] != "sha256:aaaa" {
			t.Fatalf("notices %v digest %q: an unchanged version must keep its digest", pins.Notices, pins.Values["VERAPDF_IMAGE_DIGEST"])
		}
	})

	t.Run("a version override drops its digest", func(t *testing.T) {
		t.Parallel()
		pins, err := resolvePins(file, envOf(map[string]string{"VERAPDF_VERSION": "1.30.3"}))
		if err != nil {
			t.Fatal(err)
		}
		if got := pins.Values["VERAPDF_VERSION"]; got != "1.30.3" {
			t.Errorf("VERAPDF_VERSION = %q, want the override 1.30.3", got)
		}
		if got := pins.Values["VERAPDF_IMAGE_DIGEST"]; got != "" {
			t.Errorf("VERAPDF_IMAGE_DIGEST = %q, want it dropped so the 1.30.3 tag decides", got)
		}
		if got := pins.Values["NODE_IMAGE_DIGEST"]; got != "sha256:bbbb" {
			t.Errorf("NODE_IMAGE_DIGEST = %q, an unrelated digest must stay", got)
		}
		if len(pins.Notices) != 2 {
			t.Errorf("notices = %v, want the override and the dropped digest", pins.Notices)
		}
	})

	t.Run("a release override drops the node digest", func(t *testing.T) {
		t.Parallel()
		pins, err := resolvePins(file, envOf(map[string]string{"NODE_DEBIAN_RELEASE": "trixie"}))
		if err != nil {
			t.Fatal(err)
		}
		if got := pins.Values["NODE_IMAGE_DIGEST"]; got != "" {
			t.Errorf("NODE_IMAGE_DIGEST = %q, want it dropped", got)
		}
	})

	t.Run("overriding both keeps the new digest", func(t *testing.T) {
		t.Parallel()
		pins, err := resolvePins(file, envOf(map[string]string{
			"VERAPDF_VERSION":      "1.30.3",
			"VERAPDF_IMAGE_DIGEST": "sha256:cccc",
		}))
		if err != nil {
			t.Fatal(err)
		}
		if got := pins.Values["VERAPDF_IMAGE_DIGEST"]; got != "sha256:cccc" {
			t.Errorf("VERAPDF_IMAGE_DIGEST = %q, want the overriding digest", got)
		}
	})

	t.Run("a digest may be emptied", func(t *testing.T) {
		t.Parallel()
		pins, err := resolvePins(file, envOf(map[string]string{"VERAPDF_IMAGE_DIGEST": ""}))
		if err != nil {
			t.Fatal(err)
		}
		if got := pins.Values["VERAPDF_IMAGE_DIGEST"]; got != "" {
			t.Errorf("VERAPDF_IMAGE_DIGEST = %q, want empty", got)
		}
	})

	for _, tc := range []struct {
		name string
		env  map[string]string
	}{
		{"an empty version", map[string]string{"PDFCPU_VERSION": ""}},
		{"a value with a space", map[string]string{"PDFCPU_VERSION": "0.12 .1"}},
		{"a value with a shell expansion", map[string]string{"PDFCPU_VERSION": "$(id)"}},
	} {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := resolvePins(file, envOf(tc.env)); err == nil {
				t.Fatalf("resolvePins accepted %v", tc.env)
			}
		})
	}
}

func TestBuildArgs(t *testing.T) {
	t.Parallel()

	file := []pinEntry{
		{"VERAPDF_VERSION", "1.30.2"},
		{"VERAPDF_IMAGE_DIGEST", "sha256:aaaa"},
		{"QPDF_VERSION", "12.4.1"},
		{"PDFCPU_VERSION", "0.12.1"},
	}
	pins, err := resolvePins(file, envOf(map[string]string{"VERAPDF_VERSION": "1.30.3"}))
	if err != nil {
		t.Fatal(err)
	}
	got := pins.buildArgs()
	want := []string{
		"--build-arg", "VERAPDF_VERSION=1.30.3",
		"--build-arg", "VERAPDF_IMAGE_DIGEST=",
		"--build-arg", "PDFCPU_VERSION=0.12.1",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("buildArgs = %q, want %q (file order, local-tool pins left out, dropped digest empty)", got, want)
	}

	cmd := dockerBuildArgs("img", "ctx", pins)
	if cmd[0] != "build" || cmd[len(cmd)-1] != "ctx" || !slices.Contains(cmd, "VERAPDF_VERSION=1.30.3") {
		t.Fatalf("dockerBuildArgs = %q, want build ... ctx carrying every pin", cmd)
	}
}

// TestDockerfileDeclaresEveryImagePinWithoutDefault ties the Dockerfile to
// versions.env: every pin the image consumes is an ARG, no ARG is a pin the
// file lacks, and no ARG carries a default that could drift from the file.
// A pin missing from the Dockerfile would be passed and ignored, and one
// missing from the file would build with an empty value.
func TestDockerfileDeclaresEveryImagePinWithoutDefault(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(corpusSourceDir, "Dockerfile"))
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	// BuildKit fills these itself.
	automatic := map[string]bool{"TARGETARCH": true}
	argLine := regexp.MustCompile(`(?m)^ARG\s+(\S+)`)
	declared := make(map[string]bool)
	for _, m := range argLine.FindAllStringSubmatch(string(data), -1) {
		name, def, hasDefault := strings.Cut(m[1], "=")
		if hasDefault {
			t.Errorf("ARG %s carries the default %q; versions.env is the only source", name, def)
		}
		if !automatic[name] {
			declared[name] = true
		}
	}
	if len(declared) < 10 {
		t.Fatalf("parsed only %d ARGs, so the Dockerfile was not read: %v", len(declared), declared)
	}

	wanted := make(map[string]bool)
	for _, e := range readPinsFile(t) {
		if !localToolPins[e.Key] {
			wanted[e.Key] = true
		}
	}
	for k := range wanted {
		if !declared[k] {
			t.Errorf("versions.env pins %s but the Dockerfile declares no ARG for it", k)
		}
	}
	for k := range declared {
		if !wanted[k] {
			t.Errorf("the Dockerfile declares ARG %s but versions.env does not pin it", k)
		}
	}
}

// TestEntrypointRecordsEveryGoldenDir checks that the entrypoint writes a
// version record for every golden directory a manifest declares, and that
// stagePins knows which pins each depends on. A directory with no record
// would warn "unknown" forever; one missing from stagePins would never be
// checked.
func TestEntrypointRecordsEveryGoldenDir(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(corpusSourceDir, "entrypoint.sh"))
	if err != nil {
		t.Fatalf("read entrypoint.sh: %v", err)
	}
	call := regexp.MustCompile(`(?m)^\s*record_stage\s+(\S+)`)
	recorded := make(map[string]bool)
	for _, m := range call.FindAllStringSubmatch(string(data), -1) {
		recorded[m[1]] = true
	}
	if !recorded[pdfsStage] || len(recorded) < 5 {
		t.Fatalf("parsed record_stage calls %v, so the entrypoint was not read", recorded)
	}
	for stage := range stagePins {
		if !recorded[stage] {
			t.Errorf("stagePins lists %s but the entrypoint never records it", stage)
		}
	}

	for _, m := range readManifests(t) {
		for feature, rel := range m.Goldens {
			stage := filepath.ToSlash(filepath.Clean(rel))
			if !recorded[stage] {
				t.Errorf("%s/%s reads %s, which the entrypoint never records", m.Name, feature, stage)
			}
			if _, ok := stagePins[stage]; !ok {
				t.Errorf("%s/%s reads %s, which stagePins does not map to its pins", m.Name, feature, stage)
			}
		}
	}
}

// TestEverySourceReadsItsPin checks that each corpus's download step reads the
// revision sourcePins says its PDFs come from. The pdfjs corpus is the one
// whose PDFs come from the image's pdf.js checkout, so its pin is read by the
// Dockerfile.
func TestEverySourceReadsItsPin(t *testing.T) {
	t.Parallel()

	dockerfile, err := os.ReadFile(filepath.Join(corpusSourceDir, "Dockerfile"))
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	for _, m := range readManifests(t) {
		keys, ok := sourcePins[m.Source]
		if !ok {
			t.Errorf("corpus %s (source %s) has no sourcePins entry", m.Name, m.Source)
			continue
		}
		script, err := os.ReadFile(filepath.Join(corpusSourceDir, m.Source, "download.sh"))
		if err != nil {
			t.Fatalf("read %s/download.sh: %v", m.Source, err)
		}
		for _, k := range keys {
			use := "${" + k
			if !bytes.Contains(script, []byte(use)) && !bytes.Contains(dockerfile, []byte(use)) {
				t.Errorf("%s/download.sh never expands %s, so the pin does not reach the download", m.Source, k)
			}
		}
		if bytes.Contains(script, []byte("git clone")) {
			t.Errorf("%s/download.sh still clones a branch; fetch the pinned revision instead", m.Source)
		}
	}
}

func readManifests(t *testing.T) []*Manifest {
	t.Helper()
	entries, err := os.ReadDir(corpusSourceDir)
	if err != nil {
		t.Fatalf("read corpus dir: %v", err)
	}
	var out []*Manifest
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(corpusSourceDir, e.Name(), "manifest.json"))
		if err != nil {
			continue
		}
		var m Manifest
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("parse %s manifest: %v", e.Name(), err)
		}
		if m.Source == "" {
			m.Source = e.Name()
		}
		out = append(out, &m)
	}
	if len(out) < 7 {
		t.Fatalf("found %d manifests, want the 7 corpora", len(out))
	}
	return out
}

// testPins returns resolved pins for the checks below.
func testPins(t *testing.T, env map[string]string) *resolvedPins {
	t.Helper()
	file := []pinEntry{
		{"VERAPDF_VERSION", "1.30.2"},
		{"VERAPDF_IMAGE_DIGEST", "sha256:aaaa"},
		{"JRE_VERSION", "17.0.20.1+1"},
		{"NODE_VERSION", "24.21.0"},
		{"PDFJS_REVISION", "1111111111111111111111111111111111111111"},
		{"PDFCPU_VERSION", "0.12.1"},
		{"VERAPDF_CORPUS_REVISION", "2222222222222222222222222222222222222222"},
	}
	pins, err := resolvePins(file, envOf(env))
	if err != nil {
		t.Fatal(err)
	}
	return pins
}

// writeRecord writes versions.json with every stage recorded at the given
// pins.
func writeRecord(t *testing.T, root string, pins map[string]string, stages ...string) {
	t.Helper()
	rec := versionRecord{Format: versionRecordFormat, Stages: make(map[string]stageRecord)}
	for _, s := range stages {
		rec.Stages[s] = stageRecord{RecordedAt: "2026-09-26T00:00:00Z", Pins: pins}
	}
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, versionRecordName), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// writeVeraReport writes a compliance golden whose build header names core.
func writeVeraReport(t *testing.T, root, name, core string) {
	t.Helper()
	dir := filepath.Join(root, "goldens", "compliance")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"report":{"buildInformation":{"releaseDetails":[{
  "id" : "core",
  "version" : "` + core + `",
  "buildDate" : 1780483380000
},{
  "id" : "apps",
  "version" : "` + core + `",
  "buildDate" : 1780484100000
}]},"jobs":[]}}`
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func veraManifest() *Manifest {
	return &Manifest{
		Name: "verapdf", Source: "verapdf", PDFRoot: "pdfs",
		Goldens: map[string]string{
			"compliance":  "goldens/compliance",
			"remediation": "goldens/compliance",
			"bookmarks":   "goldens/bookmarks",
		},
	}
}

func TestCheckCorpusVersions(t *testing.T) {
	t.Parallel()

	matching := map[string]string{
		"VERAPDF_VERSION":         "1.30.2",
		"VERAPDF_IMAGE_DIGEST":    "sha256:aaaa",
		"JRE_VERSION":             "17.0.20.1+1",
		"PDFCPU_VERSION":          "0.12.1",
		"VERAPDF_CORPUS_REVISION": "2222222222222222222222222222222222222222",
	}
	withValue := func(k, v string) map[string]string {
		out := make(map[string]string, len(matching))
		for key, val := range matching {
			out[key] = val
		}
		out[k] = v
		return out
	}
	all := []string{"compliance", "remediation", "bookmarks"}

	t.Run("a matching record passes", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeRecord(t, root, matching, pdfsStage, "goldens/compliance", "goldens/bookmarks")
		writeVeraReport(t, root, "PDF_A-1b.json", "1.30.2")
		got := checkCorpusVersions(veraManifest(), root, all, testPins(t, nil))
		if len(got.Refusals)+len(got.Warnings) != 0 {
			t.Fatalf("refusals %v warnings %v, want none", got.Refusals, got.Warnings)
		}
	})

	t.Run("a stale golden is refused and only its directory re-provisioned", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeRecord(t, root, withValue("JRE_VERSION", "17.0.19+10"), pdfsStage, "goldens/compliance", "goldens/bookmarks")
		got := checkCorpusVersions(veraManifest(), root, all, testPins(t, nil))
		if len(got.Refusals) != 1 || !strings.Contains(got.Refusals[0], "goldens/compliance was provisioned with JRE_VERSION=17.0.19+10, the pin is 17.0.20.1+1") {
			t.Fatalf("refusals = %v, want one naming the stage, the key, the recorded and the pinned value", got.Refusals)
		}
		wantRemedy := "rm -rf " + filepath.Join(root, "goldens", "compliance") + " && make corpus-init CORPUS_NAME=verapdf"
		if got.Remedy != wantRemedy {
			t.Fatalf("remedy = %q, want %q", got.Remedy, wantRemedy)
		}
	})

	t.Run("a stale source re-provisions the whole corpus", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeRecord(t, root, withValue("VERAPDF_CORPUS_REVISION", "3333333333333333333333333333333333333333"),
			pdfsStage, "goldens/compliance", "goldens/bookmarks")
		got := checkCorpusVersions(veraManifest(), root, all, testPins(t, nil))
		if len(got.Refusals) != 1 || !strings.Contains(got.Refusals[0], "pdfs was provisioned with VERAPDF_CORPUS_REVISION") {
			t.Fatalf("refusals = %v, want the download stage refused", got.Refusals)
		}
		if got.Remedy != "rm -rf "+root+" && make corpus-init CORPUS_NAME=verapdf" {
			t.Fatalf("remedy = %q, want the whole testdata root removed", got.Remedy)
		}
	})

	t.Run("an unread golden is not checked", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeRecord(t, root, withValue("PDFCPU_VERSION", "0.11.0"), pdfsStage, "goldens/compliance", "goldens/bookmarks")
		got := checkCorpusVersions(veraManifest(), root, []string{"compliance"}, testPins(t, nil))
		if len(got.Refusals) != 0 {
			t.Fatalf("refusals = %v; the run reads no bookmarks golden, so its pdfcpu version is irrelevant", got.Refusals)
		}
		got = checkCorpusVersions(veraManifest(), root, all, testPins(t, nil))
		if len(got.Refusals) != 1 {
			t.Fatalf("refusals = %v, want the bookmarks golden refused once the run reads it", got.Refusals)
		}
	})

	t.Run("an environment override must match the record", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeRecord(t, root, withValue("VERAPDF_VERSION", "1.30.3"), "goldens/compliance")
		stale := checkCorpusVersions(veraManifest(), root, []string{"compliance"}, testPins(t, nil))
		if len(stale.Refusals) == 0 {
			t.Fatal("goldens recorded with an override passed against the file's pins")
		}
		// The override drops the digest the record also lacks.
		rec := withValue("VERAPDF_VERSION", "1.30.3")
		rec["VERAPDF_IMAGE_DIGEST"] = ""
		writeRecord(t, root, rec, "goldens/compliance")
		writeVeraReport(t, root, "PDF_A-1b.json", "1.30.3")
		got := checkCorpusVersions(veraManifest(), root, []string{"compliance"}, testPins(t, map[string]string{"VERAPDF_VERSION": "1.30.3"}))
		if len(got.Refusals) != 0 {
			t.Fatalf("refusals = %v, want none under the same override", got.Refusals)
		}
	})

	t.Run("testdata without a record warns but still reads the veraPDF header", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeVeraReport(t, root, "PDF_A-1b.json", "1.30.1")
		writeVeraReport(t, root, "PDF_A-2b.json", "1.30.1")
		writeVeraReport(t, root, "WTPDF-1.0-Reuse.json", "1.30.2")
		got := checkCorpusVersions(veraManifest(), root, all, testPins(t, nil))
		if len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], "has no versions.json") {
			t.Fatalf("warnings = %v, want one saying the versions are unknown", got.Warnings)
		}
		if len(got.Refusals) != 1 || !strings.Contains(got.Refusals[0], "2 compliance golden(s) report veraPDF core 1.30.1") ||
			!strings.Contains(got.Refusals[0], "PDF_A-1b.json, PDF_A-2b.json") {
			t.Fatalf("refusals = %v, want the two 1.30.1 reports named", got.Refusals)
		}
	})

	t.Run("a stage with no record warns", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeRecord(t, root, matching, pdfsStage)
		got := checkCorpusVersions(veraManifest(), root, []string{"bookmarks"}, testPins(t, nil))
		if len(got.Refusals) != 0 || len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], "no versions for goldens/bookmarks") {
			t.Fatalf("refusals %v warnings %v, want one warning for the unrecorded stage", got.Refusals, got.Warnings)
		}
	})

	t.Run("a corrupt record is refused", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, versionRecordName), []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		got := checkCorpusVersions(veraManifest(), root, all, testPins(t, nil))
		if len(got.Refusals) != 1 || got.Remedy == "" {
			t.Fatalf("refusals %v remedy %q, want the record refused with a remedy", got.Refusals, got.Remedy)
		}
	})

	t.Run("an unprovisioned corpus is not checked", func(t *testing.T) {
		t.Parallel()
		got := checkCorpusVersions(veraManifest(), filepath.Join(t.TempDir(), "absent"), all, testPins(t, nil))
		if len(got.Refusals)+len(got.Warnings) != 0 {
			t.Fatalf("refusals %v warnings %v, want none", got.Refusals, got.Warnings)
		}
	})
}

func TestVerifyVersionsRefusesUnlessAllowed(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeVeraReport(t, root, "PDF_A-1b.json", "1.30.1")
	manifests := []*Manifest{veraManifest()}
	rootOf := func(*Manifest) string { return root }

	var out bytes.Buffer
	if verifyVersions(&out, manifests, rootOf, []string{"compliance"}, testPins(t, nil), false) {
		t.Fatalf("verifyVersions passed goldens made with veraPDF 1.30.1:\n%s", out.String())
	}
	for _, want := range []string{"stale: verapdf: 1 compliance golden(s) report veraPDF core 1.30.1", "re-provision: rm -rf", "--allow-version-drift"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}

	out.Reset()
	if !verifyVersions(&out, manifests, rootOf, []string{"compliance"}, testPins(t, nil), true) {
		t.Fatalf("verifyVersions refused under --allow-version-drift:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "scored anyway") {
		t.Errorf("output does not say the drift is scored anyway:\n%s", out.String())
	}
}

func TestVeraPDFReportCoreReadsRealHeader(t *testing.T) {
	t.Parallel()

	// The exact layout veraPDF 1.30 writes: pretty-printed objects inside a
	// compact wrapper.
	p := filepath.Join(t.TempDir(), "r.json")
	body := "{\"report\":{\"buildInformation\":{\"releaseDetails\":[{\n  \"id\" : \"core\",\n  \"version\" : \"1.30.2\",\n  \"buildDate\" : 1780476120000\n},{\n  \"id\" : \"validation-model\",\n  \"version\" : \"1.30.2\"\n}]},\"jobs\":[]}}"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := veraPDFReportCore(p)
	if err != nil || got != "1.30.2" {
		t.Fatalf("veraPDFReportCore = %q, %v; want 1.30.2", got, err)
	}

	if err := os.WriteFile(p, []byte(`{"report":{"jobs":[]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := veraPDFReportCore(p); err == nil {
		t.Fatal("a report with no build header returned a version")
	}
}

func TestCorpusStagesDeduplicatesSharedGoldens(t *testing.T) {
	t.Parallel()

	got := corpusStages(veraManifest(), []string{"remediation", "compliance", "text"})
	want := []string{pdfsStage, "goldens/compliance"}
	sort.Strings(got[1:])
	if !slices.Equal(got, want) {
		t.Fatalf("corpusStages = %v, want %v (download first, shared directory once, features without goldens skipped)", got, want)
	}
}
