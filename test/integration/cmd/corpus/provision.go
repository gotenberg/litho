package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

// The paths of the provisioning image (corpus/Dockerfile) `corpus provision`
// runs in.
const (
	// provisionOutput is where corpus init mounts the corpus testdata.
	provisionOutput = "/output"
	// imageVersions records the pins the image was built with, overrides
	// included.
	imageVersions = "/opt/litho/versions.env"
	// imagePDFJS is the pdf.js checkout the Node generators load.
	imagePDFJS = "/opt/pdfjs"
	// imageLib holds the Node generators.
	imageLib = "/work/lib"
	// imagePopplerFonts holds the fonts poppler draws unembedded fonts with.
	imagePopplerFonts = "/opt/poppler/fonts"
	imageMustang      = "/opt/mustang.jar"
	imageKoSIT        = "/opt/kosit.jar"
	imageKoSITConfig  = "/opt/kosit-config/scenarios.xml"
)

// defaultProvisionFeatures are the goldens a provisioning run with no
// feature list makes.
const defaultProvisionFeatures = "text,metadata,compliance,remediation,remediation-aggressive"

// runProvision is the provisioning image's entrypoint: it derives the
// goldens of one corpus from the PDFs corpus init put in the mounted
// testdata, running only the generators the requested features read.
//
//	corpus provision <corpus> <profiles_csv> [features_csv] [profile_mode]
//
// features_csv lists the features whose goldens the corpus needs (text,
// metadata, imagedecode, render, diff, bookmarks, merge, merge-empty,
// facturx-validity, facturx-schematron, compliance, remediation,
// remediation-aggressive). profile_mode "auto" validates each PDF only
// against the profile its XMP claims.
//
// Each stage is skipped when its output already exists, so a rerun after a
// partial failure resumes, and records the versions it ran with in
// versions.json (see [stagePins]).
func runProvision(ctx context.Context, args []string) int {
	if len(args) < 1 || args[0] == "" {
		fmt.Fprintln(os.Stderr, "usage: corpus provision <corpus> <profiles_csv> [features_csv] [profile_mode]")
		return 2
	}
	p := provisioner{corpus: args[0], out: provisionOutput, features: defaultProvisionFeatures, mode: ProfileModeExpand}
	if len(args) > 1 {
		p.profiles = args[1]
	}
	if len(args) > 2 && args[2] != "" {
		p.features = args[2]
	}
	if len(args) > 3 && args[3] != "" {
		p.mode = args[3]
	}
	if err := p.run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s: %v\n", p.corpus, err)
		return 1
	}
	fmt.Printf("==> %s: done.\n", p.corpus)
	return 0
}

// provisioner holds one provisioning run.
type provisioner struct {
	corpus, profiles, features, mode string
	out                              string
	pins                             map[string]string
	tools                            map[string]string
}

// wants reports whether the run makes the goldens of feature.
func (p *provisioner) wants(feature string) bool {
	return strings.Contains(","+p.features+",", ","+feature+",")
}

func (p *provisioner) run(ctx context.Context) error {
	pins, err := readEnvFile(imageVersions)
	if err != nil {
		return fmt.Errorf("%w: build the image through corpus init", err)
	}
	p.pins = pins
	p.tools = imageToolVersions(ctx)

	pdfs := filepath.Join(p.out, "pdfs")
	count, err := countPDFs(pdfs)
	if err != nil || count == 0 {
		return fmt.Errorf("no PDF in %s: corpus init installs or fetches them on the host first", pdfs)
	}
	fmt.Printf("==> %s: %d PDFs on disk.\n", p.corpus, count)

	claims := ""
	if p.mode == ProfileModeAuto {
		claims = filepath.Join(p.out, "claims.csv")
		if info, err := os.Stat(claims); err == nil && info.Size() > 0 {
			fmt.Println("==> reusing existing claims.csv.")
		} else {
			fmt.Println("==> scanning XMP claims (auto mode)...")
			if err := writeClaims(ctx, pdfs, claims, splitCSV(p.profiles), runtime.NumCPU()); err != nil {
				return err
			}
		}
	}

	for _, s := range []struct {
		name string
		on   bool
		run  func(context.Context, string) error
	}{
		{"text + metadata goldens", p.wants("text") || p.wants("metadata"), p.textGoldens},
		{"image goldens", p.wants("imagedecode"), p.imageGoldens},
		{"render goldens", p.wants("render"), p.renderGoldens},
		{"diff goldens", p.wants("diff"), p.diffGoldens},
		{"bookmarks goldens", p.wants("bookmarks"), p.bookmarkGoldens},
		{"merge goldens", p.wants("merge"), p.mergeGoldens},
		{"merge-empty goldens", p.wants("merge-empty"), p.mergeEmptyGoldens},
		{"facturx-validity goldens", p.wants("facturx-validity"), p.mustangGoldens},
		{"facturx-schematron goldens", p.wants("facturx-schematron"), p.kositGoldens},
		{"VeraPDF goldens", p.wants("compliance") || p.wants("remediation") || p.wants("remediation-aggressive"), func(ctx context.Context, pdfs string) error {
			return p.complianceGoldens(ctx, pdfs, claims)
		}},
	} {
		if !s.on {
			fmt.Printf("==> skipping %s (not requested by %s).\n", s.name, p.corpus)
			continue
		}
		if err := s.run(ctx, pdfs); err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
	}
	return nil
}

// stage runs gen into dir unless dir already holds an entry, then records
// the versions it ran with under rel.
func (p *provisioner) stage(rel string, gen func(dir string) (map[string]any, error)) error {
	dir := filepath.Join(p.out, filepath.FromSlash(rel))
	if nonEmptyDir(dir) {
		fmt.Printf("==> %s already present, skipping.\n", rel)
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	fmt.Printf("==> generating %s...\n", rel)
	extra, err := gen(dir)
	if err != nil {
		return err
	}
	return p.record(rel, extra)
}

func (p *provisioner) textGoldens(ctx context.Context, pdfs string) error {
	text, meta := filepath.Join(p.out, "goldens", "text"), filepath.Join(p.out, "goldens", "metadata")
	if nonEmptyDir(text) && nonEmptyDir(meta) {
		fmt.Println("==> text + metadata goldens already present, skipping PDF.js generator.")
		return nil
	}
	for _, d := range []string{text, meta} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	fmt.Println("==> generating text + metadata goldens (PDF.js)...")
	if err := node(ctx, "gen-references.mjs", "--pdfs-dir", pdfs, "--text-out", text, "--meta-out", meta); err != nil {
		return err
	}
	if err := p.record("goldens/text", nil); err != nil {
		return err
	}
	return p.record("goldens/metadata", nil)
}

func (p *provisioner) imageGoldens(ctx context.Context, pdfs string) error {
	return p.stage("goldens/images", func(dir string) (map[string]any, error) {
		return nil, node(ctx, "gen-images.mjs", "--pdfs-dir", pdfs, "--out", dir)
	})
}

// renderGoldens renders the litho corpora whole, their fixtures small and
// each exercising a construct of its own, and the downloaded corpora one PDF
// in sixteen (see [renderSample]), which keeps their goldens to a few
// megabytes. It then copies poppler's fonts (see [copyPopplerFonts]).
func (p *provisioner) renderGoldens(ctx context.Context, pdfs string) error {
	sample := 16
	if strings.HasPrefix(p.corpus, "litho/") {
		sample = 256
	}
	if err := p.stage("goldens/render", func(dir string) (map[string]any, error) {
		return map[string]any{"render_sample_per_256": sample}, renderPDFs(ctx, pdfs, dir, sample, renderMaxPages, false)
	}); err != nil {
		return err
	}
	return copyPopplerFonts(filepath.Join(p.out, "goldens", "render", "fonts"))
}

// diffGoldens makes the diff goldens of the html and Office corpora: what
// the Chromium or LibreOffice reference PDFs hold, read the ways the diff
// feature reads litho's conversion of the same source. PDF.js extracts each
// page's text and lists its links, poppler draws every page of every PDF.
//
// An Office reference's annotations are left out of its pages: the notes
// LibreOffice exports have no appearance, so the icon drawn is the viewer's,
// and the form fields' appearances use fonts it does not embed.
func (p *provisioner) diffGoldens(ctx context.Context, pdfs string) error {
	diff := filepath.Join(p.out, "goldens", "diff")
	sub := []string{"text", "metadata", "render", "links"}
	if nonEmptyDir(filepath.Join(diff, "text")) && nonEmptyDir(filepath.Join(diff, "render")) && nonEmptyDir(filepath.Join(diff, "links")) {
		fmt.Println("==> diff goldens already present, skipping PDF.js and pdftoppm.")
		return nil
	}
	if err := os.RemoveAll(diff); err != nil {
		return err
	}
	for _, s := range sub {
		if err := os.MkdirAll(filepath.Join(diff, s), 0o755); err != nil {
			return err
		}
	}
	fmt.Println("==> generating diff goldens (PDF.js text and links, pdftoppm pages)...")
	if err := node(ctx, "gen-references.mjs", "--pdfs-dir", pdfs, "--text-out", filepath.Join(diff, "text"), "--meta-out", filepath.Join(diff, "metadata")); err != nil {
		return err
	}
	hide := officeFormats[filepath.Base(p.corpus)]
	if err := renderPDFs(ctx, pdfs, filepath.Join(diff, "render"), 256, 0, hide); err != nil {
		return err
	}
	if err := node(ctx, "gen-links.mjs", "--pdfs-dir", pdfs, "--out", filepath.Join(diff, "links")); err != nil {
		return err
	}
	return p.record("goldens/diff", nil)
}

// officeFormats are the formats of the Office corpora, named by the last
// element of the corpus name.
var officeFormats = map[string]bool{ //nolint:gochecknoglobals // static table
	"docx": true, "odt": true, "xlsx": true, "ods": true, "csv": true,
	"pptx": true, "odp": true, "doc": true, "xls": true, "ppt": true,
}

func (p *provisioner) bookmarkGoldens(ctx context.Context, pdfs string) error {
	return p.stage("goldens/bookmarks", func(dir string) (map[string]any, error) {
		return nil, bookmarkGoldens(ctx, pdfs, dir)
	})
}

func (p *provisioner) mergeGoldens(ctx context.Context, pdfs string) error {
	return p.stage("goldens/merges", func(dir string) (map[string]any, error) {
		return nil, mergeGoldens(ctx, pdfs, dir, "")
	})
}

func (p *provisioner) mergeEmptyGoldens(ctx context.Context, pdfs string) error {
	return p.stage("goldens/merge-empty", func(dir string) (map[string]any, error) {
		return nil, mergeGoldens(ctx, pdfs, dir, filepath.Join(pdfs, mergeEmptyPartner))
	})
}

func (p *provisioner) mustangGoldens(ctx context.Context, pdfs string) error {
	return p.stage("goldens/facturx-validity", func(dir string) (map[string]any, error) {
		return nil, mustangGoldens(ctx, pdfs, filepath.Join(dir, "jobs.json"))
	})
}

func (p *provisioner) kositGoldens(ctx context.Context, pdfs string) error {
	return p.stage("goldens/facturx-schematron", func(dir string) (map[string]any, error) {
		return nil, kositGoldens(ctx, pdfs, filepath.Join(dir, "jobs.json"))
	})
}

// complianceGoldens records, next to the versions, the build the veraPDF
// reports themselves name: it is what the corpus CLI reads back from a golden
// that predates the version record.
func (p *provisioner) complianceGoldens(ctx context.Context, pdfs, claims string) error {
	return p.stage("goldens/compliance", func(dir string) (map[string]any, error) {
		if err := veraPDFGoldens(ctx, pdfs, dir, splitCSV(p.profiles), claims); err != nil {
			return nil, err
		}
		reports, err := filepath.Glob(filepath.Join(dir, "*.json"))
		if err != nil || len(reports) == 0 {
			return nil, err
		}
		sort.Strings(reports)
		data, err := os.ReadFile(reports[0]) //nolint:gosec // a report this stage wrote
		if err != nil {
			return nil, err
		}
		var r struct {
			Report struct {
				BuildInformation struct {
					ReleaseDetails json.RawMessage `json:"releaseDetails"`
				} `json:"buildInformation"`
			} `json:"report"`
		}
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("decode %s: %w", reports[0], err)
		}
		build := r.Report.BuildInformation.ReleaseDetails
		if len(build) == 0 {
			build = json.RawMessage("null")
		}
		return map[string]any{"verapdf_build": build}, nil
	})
}

// record writes the versions a stage ran with into versions.json, keyed by
// the directory it filled: every pin of the image and what its tools say
// about themselves, so a golden's provenance reads without the image that
// made it. The corpus CLI refuses to score a golden whose record disagrees
// with versions.env.
func (p *provisioner) record(rel string, extra map[string]any) error {
	entry := map[string]any{"recorded_at": time.Now().UTC().Format(time.RFC3339), "pins": p.pins, "tools": p.tools}
	for k, v := range extra {
		entry[k] = v
	}
	return mergeStageRecord(p.out, rel, entry)
}

// mergeStageRecord sets one stage's entry of the versions.json under root,
// keeping the others.
func mergeStageRecord(root, rel string, entry map[string]any) error {
	path := filepath.Join(root, versionRecordName)
	rec := map[string]any{}
	if data, err := os.ReadFile(path); err == nil { //nolint:gosec // the corpus testdata's own record
		if err := json.Unmarshal(data, &rec); err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
	}
	stages, _ := rec["stages"].(map[string]any)
	if stages == nil {
		stages = map[string]any{}
	}
	stages[rel] = entry
	rec["format"], rec["stages"] = versionRecordFormat, stages
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("encode versions record: %w", err)
	}
	return writeFileAtomic(path, append(data, '\n'))
}

// imageToolVersions reports what the image's tools say about themselves.
func imageToolVersions(ctx context.Context) map[string]string {
	line := func(text string, n int) string {
		lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
		if n > len(lines) {
			n = len(lines)
		}
		return strings.Join(lines[:n], "\n")
	}
	// output runs a tool for its version, with its standard error when
	// stderr: a tool that fails reports what it wrote, or nothing.
	output := func(stderr bool, name string, args ...string) string {
		cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // the image's fixed tools
		var out []byte
		if stderr {
			out, _ = cmd.CombinedOutput() //nolint:errcheck // a failing tool's record is what it wrote
		} else {
			out, _ = cmd.Output() //nolint:errcheck // a failing tool's record is what it wrote
		}
		return strings.TrimRight(string(out), "\n")
	}
	verapdf := strings.ReplaceAll(line(output(false, "verapdf", "--version"), 2), "\n", " ")
	javaLines := strings.Split(output(true, "java", "-version"), "\n")
	java := ""
	if len(javaLines) > 1 {
		java = javaLines[1]
	} else if len(javaLines) == 1 {
		java = javaLines[0]
	}
	pdfjs := ""
	if data, err := os.ReadFile(filepath.Join(imagePDFJS, "build", "version.json")); err == nil {
		var b bytes.Buffer
		if json.Compact(&b, data) == nil {
			pdfjs = b.String()
		}
	}
	pdfcpu := ""
	for _, l := range strings.Split(output(false, "pdfcpu", "version"), "\n") {
		if strings.HasPrefix(l, "pdfcpu:") {
			pdfcpu = l
			break
		}
	}
	return map[string]string{
		"verapdf": verapdf,
		"java":    java,
		"node":    output(false, "node", "--version"),
		"pdfjs":   pdfjs,
		"pdfcpu":  pdfcpu,
		"poppler": line(output(true, "pdftoppm", "-v"), 1),
	}
}

// node runs one of the image's Node generators against its pdf.js.
func node(ctx context.Context, script string, args ...string) error {
	cmd := exec.CommandContext(ctx, "node", append([]string{filepath.Join(imageLib, script), "--pdfjs-dir", imagePDFJS}, args...)...) //nolint:gosec // fixed scripts of the image
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("node %s: %w", script, err)
	}
	return nil
}

// readEnvFile reads the KEY=value lines of an env file, the last of a
// repeated key winning.
func readEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path) //nolint:gosec // the image's fixed version record
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if m := envLine.FindStringSubmatch(sc.Text()); m != nil {
			out[m[1]] = m[2]
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s holds no pin", path)
	}
	return out, nil
}

var envLine = regexp.MustCompile(`^([A-Z][A-Z0-9_]*)=(.*)$`)

// nonEmptyDir reports whether dir exists and holds at least one entry.
func nonEmptyDir(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}

// topPDFs returns the paths of the files at the top of dir whose name ends in
// ".pdf", or in any case of it when anyCase, sorted.
func topPDFs(dir string, anyCase bool) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() {
			continue
		}
		if strings.HasSuffix(name, ".pdf") || (anyCase && strings.HasSuffix(strings.ToLower(name), ".pdf")) {
			out = append(out, filepath.Join(dir, name))
		}
	}
	sort.Strings(out)
	return out, nil
}

// countPDFs counts the PDFs at the top of dir.
func countPDFs(dir string) (int, error) {
	pdfs, err := topPDFs(dir, false)
	return len(pdfs), err
}

// runQuiet runs a command, returning its exit error and its standard output
// and error combined, trailing newlines trimmed as a shell's command
// substitution trims them.
func runQuiet(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // the image's fixed tools
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimRight(string(out), "\n"), err
}
