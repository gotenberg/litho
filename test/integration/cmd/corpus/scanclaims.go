package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// runScanClaims walks a flat pdfs/ directory, reads each PDF's XMP
// claim, and writes one CSV line per claimed PDF. The CSV is consumed
// by the provisioning Docker entrypoint to drive per-claim VeraPDF
// validation (one PDF, one profile) instead of cartesian (every PDF
// against every profile).
//
// Format: <basename>,<profile_dir>
//
// Files with no recognised XMP claim do not appear in the CSV. The
// entrypoint treats absent rows as "no compliance/remediation
// fixture", which lines up with the runner's auto-mode behavior.
func runScanClaims(args []string) int {
	fs := flag.NewFlagSet("scan-claims", flag.ExitOnError)
	pdfsDir := fs.String("pdfs", "", "Directory holding the flat pdfs/ tree (required)")
	out := fs.String("out", "", "Output CSV path; empty writes to stdout")
	allow := fs.String("profiles", "", "Comma-separated allow-set; claims outside this set are dropped (empty = accept all)")
	workers := fs.Int("workers", runtime.NumCPU(), "Parallel scan workers")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "parsing flags: %v\n", err)
		return 1
	}
	if *pdfsDir == "" {
		fmt.Fprintln(os.Stderr, "scan-claims: --pdfs is required")
		return 1
	}
	if *workers < 1 {
		*workers = 1
	}

	allowed := splitCSV(*allow)
	allowSet := make(map[string]bool, len(allowed))
	for _, p := range allowed {
		allowSet[p] = true
	}

	pdfs, err := collectPDFs(*pdfsDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scan-claims: %v\n", err)
		return 1
	}

	rows := scanClaimsParallel(pdfs, allowSet, *workers)
	sort.Slice(rows, func(i, j int) bool { return rows[i].base < rows[j].base })

	w := os.Stdout
	if *out != "" {
		f, err := os.Create(*out) //nolint:gosec // out path supplied by trusted CLI/provisioning script
		if err != nil {
			fmt.Fprintf(os.Stderr, "scan-claims: create %s: %v\n", *out, err)
			return 1
		}
		defer f.Close()
		w = f
	}
	bw := bufio.NewWriter(w)
	for _, r := range rows {
		if _, err := fmt.Fprintf(bw, "%s,%s\n", r.base, r.profile); err != nil {
			fmt.Fprintf(os.Stderr, "scan-claims: write: %v\n", err)
			return 1
		}
	}
	if err := bw.Flush(); err != nil {
		fmt.Fprintf(os.Stderr, "scan-claims: flush: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "scan-claims: scanned %d PDFs, %d claimed a profile\n", len(pdfs), len(rows))
	return 0
}

type claimRow struct {
	base    string
	profile string
}

// collectPDFs returns every *.pdf file under dir. A walk error fails the scan,
// as in [walkPDFs]: skipping it wrote a claims.csv that omits the files it
// could not reach, or an empty one for a missing dir, and a later run reads
// that file as the corpus's claims.
func collectPDFs(dir string) ([]string, error) {
	var out []string
	if err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".pdf") {
			return nil
		}
		out = append(out, path)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("walk %s: %w", dir, err)
	}
	return out, nil
}

func scanClaimsParallel(pdfs []string, allow map[string]bool, workers int) []claimRow {
	results := make([]claimRow, len(pdfs))
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i, path := range pdfs {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			profile := detectClaimedProfile(context.Background(), path)
			if profile == "" {
				return
			}
			if len(allow) > 0 && !allow[profile] {
				return
			}
			base := strings.TrimSuffix(filepath.Base(path), ".pdf")
			results[i] = claimRow{base: base, profile: profile}
		}()
	}
	wg.Wait()

	rows := results[:0]
	for _, r := range results {
		if r.base != "" {
			rows = append(rows, r)
		}
	}
	return rows
}

// maxClaimsLineBytes bounds one line of claims.csv. It is an anti-DoS guard
// for test tooling, not a format limit: bufio.Scanner refuses a longer line
// rather than growing its buffer without end, and loadClaimsCSV then returns
// the read error. A line is a PDF basename and a profile directory name, so
// a megabyte is thousands of times past any line runScanClaims writes.
//
// # Corpus evidence
//
//   - facturx: the longest of its 150 claims.csv lines is 111 bytes.
const maxClaimsLineBytes = 1024 * 1024

// loadClaimsCSV reads a claims.csv produced by runScanClaims and
// returns a basename->profile map. Lines without a comma or with empty
// fields are skipped silently so partially-corrupted CSVs degrade to
// "no claim" rather than aborting the corpus run.
func loadClaimsCSV(path string) (map[string]string, error) {
	f, err := os.Open(path) //nolint:gosec // path resolved from manifest testdata root
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := make(map[string]string)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxClaimsLineBytes)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		base, profile, ok := strings.Cut(line, ",")
		if !ok || base == "" || profile == "" {
			continue
		}
		out[base] = profile
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return out, nil
}
