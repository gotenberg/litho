package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gotenberg/litho/pkg/pdf"
)

// runOracles runs veraPDF, poppler and pdfcpu on the staged files of the
// sampled fixtures, each tool once per batch, and records their verdicts
// on the targets. A tool that does not run marks every target it would have
// judged unavailable, which fails it.
func (f *targetsFeature) runOracles(ctx context.Context, staging string, results []*targetsStaged, workers int) error {
	var staged []*targetResult
	for _, res := range results {
		if !res.out.Sampled {
			continue
		}
		for i := range res.out.Targets {
			if r := &res.out.Targets[i]; r.file != "" {
				r.OracleChecked = true
				staged = append(staged, r)
			}
		}
	}
	if len(staged) == 0 {
		return nil
	}
	targets := make(map[string]outputTarget, len(f.targets))
	for _, t := range f.targets {
		targets[t.ID] = t
	}

	f.judgeWithVeraPDF(ctx, staging, staged, targets)
	if err := ctx.Err(); err != nil {
		return err
	}
	f.judgeWithPoppler(ctx, staging, staged, workers)
	if err := ctx.Err(); err != nil {
		return err
	}
	f.judgeWithPdfcpu(ctx, staging, staged, targets)
	return ctx.Err()
}

// judgeWithVeraPDF validates each staged file at each of its target's
// profiles, one veraPDF run per flavour, and holds the verdict to litho's.
func (f *targetsFeature) judgeWithVeraPDF(ctx context.Context, staging string, staged []*targetResult, targets map[string]outputTarget) {
	byFlavour := make(map[string][]*targetResult)
	var flavours []string
	for _, r := range staged {
		for _, p := range targets[r.ID].Profiles {
			if byFlavour[p.flavour] == nil {
				flavours = append(flavours, p.flavour)
			}
			byFlavour[p.flavour] = append(byFlavour[p.flavour], r)
		}
	}
	missing := ""
	if len(flavours) > 0 {
		if err := f.veraFound(); err != nil {
			missing = fmt.Sprintf("verapdf not on PATH: %v", err)
		}
	}
	type batch struct {
		verdicts map[string]veraVerdict
		err      error
	}
	batches := make([]batch, len(flavours))
	pool(veraPDFAtOnce, len(flavours), func(i int) {
		batches[i].verdicts, batches[i].err = f.veraBatch(ctx, staging, flavours[i], byFlavour[flavours[i]], missing)
	})
	for i, flavour := range flavours {
		verdicts, err := batches[i].verdicts, batches[i].err
		for _, r := range byFlavour[flavour] {
			if err != nil {
				r.VeraPDFUnavailable = true
				r.fail("veraPDF %s did not run: %v", flavour, err)
				continue
			}
			v, ok := verdicts[strings.TrimSuffix(r.file, ".pdf")]
			if !ok {
				r.VeraPDFUnavailable = true
				r.fail("veraPDF %s gave no verdict", flavour)
				continue
			}
			mine := r.litho[flavour]
			theirs := !v.parseFailed && v.count == 0
			if theirs != mine.clean {
				r.VeraPDFDisagrees = true
				r.fail("veraPDF %s finds %s, litho %s", flavour, veraReading(v), lithoReading(mine))
			}
		}
	}
}

// veraPDFAtOnce bounds the veraPDF runs, one per flavour, that run at once:
// each is a Java process of several hundred megabytes, and three keep the
// processors busy through the start-up each spends alone.
const veraPDFAtOnce = 3

// veraBatch links the files of rs into a directory of their own under
// staging and validates it at flavour.
func (f *targetsFeature) veraBatch(ctx context.Context, staging, flavour string, rs []*targetResult, missing string) (map[string]veraVerdict, error) {
	if missing != "" {
		return nil, errors.New(missing)
	}
	dir := filepath.Join(staging, "verapdf-"+flavour)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	for _, r := range rs {
		if err := os.Link(filepath.Join(staging, r.file), filepath.Join(dir, r.file)); err != nil {
			return nil, err
		}
	}
	return f.vera(ctx, dir, flavour)
}

// veraReading says what veraPDF found.
func veraReading(v veraVerdict) string {
	switch {
	case v.parseFailed:
		return "a file it cannot parse"
	case v.count == 0:
		return "no violation"
	}
	return "violations of " + listRules(v.rules)
}

// lithoReading says what litho's validator found.
func lithoReading(v lithoVerdict) string {
	if v.clean {
		return "none"
	}
	if len(v.rules) == 0 {
		return "no verdict"
	}
	return listRules(v.rules)
}

// judgeWithPoppler draws the first pages of each staged file with the
// pinned poppler, in one container, and compares each with litho's drawing
// of the same file under the render cell's rule.
func (f *targetsFeature) judgeWithPoppler(ctx context.Context, staging string, staged []*targetResult, workers int) {
	var jobs []popplerJob
	for _, r := range staged {
		for p := 1; p <= r.pages; p++ {
			jobs = append(jobs, popplerJob{PDF: r.file, Page: p, PNG: popplerPNGRoot(r.file, p), DPI: popplerDPI(ctx, filepath.Join(staging, r.file), p)})
		}
	}
	if len(jobs) == 0 {
		return
	}
	unavailable := ""
	version, err := f.poppler(ctx, staging, jobs)
	switch {
	case err != nil:
		unavailable = err.Error()
	case f.popplerPin() != "" && version != f.popplerPin():
		unavailable = fmt.Sprintf("%v: the image runs poppler %s, versions.env pins %s", errPopplerUnavailable, version, f.popplerPin())
	}
	if unavailable != "" {
		fmt.Fprintf(os.Stderr, "targets: %s\n", unavailable)
	}
	var (
		mu      sync.Mutex
		matched = map[popplerDifferenceKey]bool{}
		used    = map[popplerDifferenceKey]bool{}
	)
	pool(workers, len(staged), func(i int) {
		r := staged[i]
		if r.pages == 0 {
			return
		}
		if unavailable != "" {
			r.PopplerUnavailable = true
			r.fail("poppler did not run: %s", unavailable)
			return
		}
		for p := 1; p <= r.pages; p++ {
			j := f.judgePopplerPage(staging, r, p)
			if j.entry {
				mu.Lock()
				key := popplerDifferenceKey{r.fixture, p}
				matched[key] = matched[key] || j.matched
				used[key] = used[key] || !j.matched
				mu.Unlock()
			}
			if j.reason != "" && !r.RendererDisagrees {
				r.RendererDisagrees = true
				r.fail("poppler draws page %d apart from litho: %s", p, j.reason)
			}
		}
	})
	for _, r := range staged {
		for p := 1; p <= r.pages; p++ {
			key := popplerDifferenceKey{r.fixture, p}
			if matched[key] && !used[key] {
				r.fail("poppler draws page %d as litho does on every target: its accepted difference is stale", p)
			}
		}
	}
}

// popplerDPI returns the resolution poppler draws page of the file at path
// at so that its drawing is litho's: 72 dpi times the page's /UserUnit. A
// PDF/A-2 or PDF/A-3 remediation writes a page past 14,400 units in larger
// units (ISO 32000-1, 7.7.3.3, Table 30), which litho's renderer draws at its
// true size and pdftoppm ignores, drawing it 1/UserUnit as large: the oracle
// is to judge the drawing, not that known gap of poppler's.
func popplerDPI(ctx context.Context, path string, page int) float64 {
	r, err := pdf.OpenFile(ctx, path)
	if err != nil {
		return 72
	}
	defer r.Close()
	pg, err := r.Page(ctx, page)
	if err != nil || pg.UserUnit <= 0 {
		return 72
	}
	return 72 * pg.UserUnit
}

// popplerPNGRoot is the PNG path, without its extension, poppler draws page
// of a staged file into.
func popplerPNGRoot(file string, page int) string {
	return fmt.Sprintf("%s-poppler-p%d", strings.TrimSuffix(file, ".pdf"), page)
}

// comparePoppler compares poppler's drawing of page of a staged file with
// litho's, "" when they match under the render cell's rule, and returns
// the count of pixels that differ when both drawings were read at one
// size.
func comparePoppler(staging, file string, page int) (detail string, pixels int) {
	theirs, err := readPNG(filepath.Join(staging, popplerPNGRoot(file, page)+".png"))
	if err != nil {
		return "poppler renders no page", 0
	}
	mine, err := readPNG(filepath.Join(staging, lithoPNG(file, page)))
	if err != nil {
		return fmt.Sprintf("litho's drawing: %v", err), 0
	}
	d := comparePage(mine, theirs)
	switch {
	case d.Error != "":
		return d.Error, 0
	case !d.Match:
		return fmt.Sprintf("%d of %d pixels differ", d.DiffPixels, d.Pixels), d.DiffPixels
	}
	return "", d.DiffPixels
}

// judgeWithPdfcpu validates the staged files of the version targets with
// pdfcpu's strict mode, in one container.
func (f *targetsFeature) judgeWithPdfcpu(ctx context.Context, staging string, staged []*targetResult, targets map[string]outputTarget) {
	var rs []*targetResult
	var files []string
	for _, r := range staged {
		if targets[r.ID].versionOnly() {
			rs = append(rs, r)
			files = append(files, r.file)
		}
	}
	if len(files) == 0 {
		return
	}
	unavailable := ""
	verdicts, version, err := f.pdfcpu(ctx, staging, files)
	switch {
	case err != nil:
		unavailable = err.Error()
	case f.pdfcpuPin() != "" && version != f.pdfcpuPin():
		unavailable = fmt.Sprintf("%v: the image runs pdfcpu %s, versions.env pins %s", errPdfcpuUnavailable, version, f.pdfcpuPin())
	}
	if unavailable != "" {
		fmt.Fprintf(os.Stderr, "targets: %s\n", unavailable)
	}
	for _, r := range rs {
		v, ok := verdicts[r.file]
		switch {
		case unavailable != "":
			r.PdfcpuUnavailable = true
			r.fail("pdfcpu did not run: %s", unavailable)
		case !ok:
			r.PdfcpuUnavailable = true
			r.fail("pdfcpu gave no verdict")
		case !v.ok:
			r.StrictFailed = true
			r.fail("pdfcpu validate -m strict: %s", v.message)
		}
	}
}

// pdfcpuVerdict is pdfcpu's strict validation of one file.
type pdfcpuVerdict struct {
	ok      bool
	message string
}

// errPdfcpuUnavailable reports that the corpus image's pdfcpu could not
// validate: no Docker, no image, or a container that failed.
var errPdfcpuUnavailable = errors.New("pdfcpu unavailable")

// pdfcpuValidateScript validates every file pdfcpu-files.txt lists with
// pdfcpu's strict mode, writing each answer beside the file and its exit
// status after it. 120 seconds bound one file.
const pdfcpuValidateScript = `#!/bin/bash
set -u
pdfcpu version > /rr/pdfcpu-version.txt 2>&1 || true
check() {
  timeout 120 pdfcpu validate -m strict "/rr/$1" > "/rr/$1.pdfcpu" 2>&1
  echo $? > "/rr/$1.pdfcpu-rc"
}
export -f check
parallel -j "$(nproc)" check {} :::: /rr/pdfcpu-files.txt
`

// pdfcpuBatchTimeout caps the one container that validates a run's files.
// Anti-hang guard, not a performance target: pdfcpu validates a converted
// file in well under a second, and the script bounds each file.
const pdfcpuBatchTimeout = 30 * time.Minute

// validateWithPdfcpu validates files, relative to dir, with the pdfcpu of
// [corpusImage] in strict mode, in one container with dir bind-mounted at
// /rr, and returns each verdict by file and the pdfcpu version the image
// runs. The container runs without a network and as the calling user.
func validateWithPdfcpu(ctx context.Context, dir string, files []string) (map[string]pdfcpuVerdict, string, error) {
	docker, err := exec.LookPath("docker")
	if err != nil {
		return nil, "", fmt.Errorf("%w: docker not on PATH", errPdfcpuUnavailable)
	}
	image, err := corpusImage()
	if err != nil {
		return nil, "", fmt.Errorf("%w: %w", errPdfcpuUnavailable, err)
	}
	inspect := exec.CommandContext(ctx, docker, "image", "inspect", "--format", "{{.Id}}", image) //nolint:gosec // docker from LookPath, fixed arguments
	if err := inspect.Run(); err != nil {
		return nil, "", fmt.Errorf("%w: image %s not built (make corpus-goldens): %w", errPdfcpuUnavailable, image, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pdfcpu-files.txt"), []byte(strings.Join(files, "\n")+"\n"), 0o600); err != nil {
		return nil, "", fmt.Errorf("write pdfcpu files: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pdfcpu.sh"), []byte(pdfcpuValidateScript), 0o600); err != nil {
		return nil, "", fmt.Errorf("write pdfcpu script: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, pdfcpuBatchTimeout)
	defer cancel()
	args := []string{"run", "--rm", "--network", "none", "-e", "HOME=/tmp"}
	if uid, gid := os.Getuid(), os.Getgid(); uid >= 0 && gid >= 0 {
		args = append(args, "--user", fmt.Sprintf("%d:%d", uid, gid))
	}
	args = append(args, "-v", dir+":/rr", "--entrypoint", "/bin/bash", image, "/rr/pdfcpu.sh")
	run := exec.CommandContext(ctx, docker, args...) //nolint:gosec // docker from LookPath, dir is our staging directory
	if out, err := run.CombinedOutput(); err != nil {
		return nil, "", fmt.Errorf("%w: docker run: %w: %s", errPdfcpuUnavailable, err, firstLine(out))
	}
	out, err := os.ReadFile(filepath.Join(dir, "pdfcpu-version.txt")) //nolint:gosec // our staging directory
	if err != nil {
		return nil, "", fmt.Errorf("%w: no version report: %w", errPdfcpuUnavailable, err)
	}
	version, _, err := parseDottedVersion(out)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %w", errPdfcpuUnavailable, err)
	}
	verdicts := make(map[string]pdfcpuVerdict, len(files))
	for _, file := range files {
		rc, err := os.ReadFile(filepath.Join(dir, file+".pdfcpu-rc")) //nolint:gosec // our staging directory
		if err != nil {
			continue
		}
		answer, err := os.ReadFile(filepath.Join(dir, file+".pdfcpu")) //nolint:gosec // our staging directory
		if err != nil {
			continue
		}
		verdicts[file] = readPdfcpuVerdict(strings.TrimSpace(string(rc)), answer)
	}
	return verdicts, version, nil
}

// readPdfcpuVerdict reads pdfcpu validate's exit status and answer: the file
// passes when pdfcpu exits 0 and says "validation ok"; otherwise the message
// is its "validation error" line, or its last line.
func readPdfcpuVerdict(rc string, answer []byte) pdfcpuVerdict {
	code, err := strconv.Atoi(rc)
	text := string(answer)
	if err == nil && code == 0 && strings.Contains(text, "validation ok") {
		return pdfcpuVerdict{ok: true}
	}
	var last string
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "validation error") {
			return pdfcpuVerdict{message: line}
		}
		if line != "" && !strings.HasPrefix(line, "*") {
			last = line
		}
	}
	if last == "" {
		last = "exit status " + rc
	}
	return pdfcpuVerdict{message: last}
}

// pdfcpuPin returns the pdfcpu release versions.env pins, the one the
// corpus image installs, or "" when the pins do not load, which accepts
// the image's.
func pdfcpuPin() string {
	pins, err := loadPins()
	if err != nil {
		return ""
	}
	return pins.Values["PDFCPU_VERSION"]
}
