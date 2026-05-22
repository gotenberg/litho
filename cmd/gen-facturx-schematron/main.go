package main

import (
	"encoding/xml"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// codedbXML mirrors the upstream FACTUR-X_*_codedb.xml structure:
// a root <codedb> with one <cl id="N"> per code list, each
// containing one <enumeration value="V"/> per allowed value.
type codedbXML struct {
	XMLName xml.Name `xml:"codedb"`
	Lists   []clXML  `xml:"cl"`
}

type clXML struct {
	ID    string           `xml:"id,attr"`
	Items []enumerationXML `xml:"enumeration"`
}

type enumerationXML struct {
	Value string `xml:"value,attr"`
}

// patternRe matches one in-test code-list lookup. The pattern lives
// inside Schematron @test attributes and is anchored to the upstream
// shape: document('<file>')/codedb/cl[@id=N]/enumeration[@value=EXPR].
//
// EXPR is always a $varN reference in the bundled rule set (verified
// at preprocessing time); the regex is conservative and stops at the
// closing predicate bracket.
var patternRe = regexp.MustCompile(`document\('([^']+)'\)/codedb/cl\[@id=(\d+)\]/enumeration\[@value=([^\]]+)\]`)

// errExitf prints to stderr and exits with code 1. The generator
// runs offline as part of the maintainer workflow, so a fail-fast
// posture is fine - no need to thread error returns out.
func errExitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func main() {
	codedbDir := flag.String("codedb-dir", "", "directory containing FACTUR-X_*_codedb.xml files")
	schDir := flag.String("sch-dir", "pkg/internal/compliance/cii_schematron/facturx_profiles", "directory containing FACTUR-X_*.sch files to rewrite in place")
	flag.Parse()

	if *codedbDir == "" {
		errExitf("--codedb-dir is required")
	}

	codedb, err := loadCodedbs(*codedbDir)
	if err != nil {
		errExitf("loading codedb files: %v", err)
	}
	if len(codedb) == 0 {
		errExitf("no FACTUR-X_*_codedb.xml files found in %s", *codedbDir)
	}

	matches, err := filepath.Glob(filepath.Join(*schDir, "FACTUR-X_*.sch"))
	if err != nil {
		errExitf("globbing %s: %v", *schDir, err)
	}
	if len(matches) == 0 {
		errExitf("no FACTUR-X_*.sch files found in %s", *schDir)
	}
	sort.Strings(matches)

	totalSubs := 0
	for _, path := range matches {
		subs, err := rewriteFile(path, codedb)
		if err != nil {
			errExitf("rewriting %s: %v", path, err)
		}
		totalSubs += subs
		fmt.Printf("%s: %d substitutions\n", filepath.Base(path), subs)
	}
	fmt.Printf("total substitutions: %d\n", totalSubs)
}

// loadCodedbs scans dir for FACTUR-X_*_codedb.xml files and returns
// a {filename → {cl-id → space-joined value list}} map ready for
// pattern substitution.
func loadCodedbs(dir string) (map[string]map[string]string, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "FACTUR-X_*_codedb.xml"))
	if err != nil {
		return nil, fmt.Errorf("globbing codedb files: %w", err)
	}

	out := make(map[string]map[string]string, len(paths))
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		var doc codedbXML
		if err := xml.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		lists := make(map[string]string, len(doc.Lists))
		for _, cl := range doc.Lists {
			vals := make([]string, 0, len(cl.Items))
			for _, e := range cl.Items {
				if err := validateCodedbValue(e.Value); err != nil {
					return nil, fmt.Errorf("%s cl[@id=%s]: %w", filepath.Base(path), cl.ID, err)
				}
				vals = append(vals, e.Value)
			}
			lists[cl.ID] = strings.Join(vals, " ")
		}
		out[filepath.Base(path)] = lists
	}
	return out, nil
}

// validateCodedbValue refuses any enumeration value that would break
// the contains-based membership test. The space-joined haystack form
// requires that values themselves contain neither spaces nor the
// single-quote delimiter litho emits around the haystack literal.
//
// Inspection of all five upstream codedb files shows the only
// allowed characters in practice are [A-Za-z0-9.:#/-]; this guard
// is defensive so that a future codedb bump fails the generator
// loudly rather than silently breaking rule evaluation.
func validateCodedbValue(v string) error {
	if v == "" {
		return fmt.Errorf("empty enumeration value")
	}
	if strings.ContainsAny(v, " \t\n\r'\"") {
		return fmt.Errorf("enumeration value %q contains whitespace or quote", v)
	}
	return nil
}

// rewriteFile applies the codedb substitution to one .sch file and
// overwrites it in place. Returns the number of substitutions made.
func rewriteFile(path string, codedb map[string]map[string]string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("reading: %w", err)
	}

	var (
		subs   int
		failed error
	)
	rewritten := patternRe.ReplaceAllStringFunc(string(raw), func(match string) string {
		groups := patternRe.FindStringSubmatch(match)
		if len(groups) != 4 {
			failed = fmt.Errorf("unexpected match shape: %q", match)
			return match
		}
		file, id, expr := groups[1], groups[2], groups[3]
		lists, ok := codedb[file]
		if !ok {
			failed = fmt.Errorf("missing codedb file %q referenced by %s", file, filepath.Base(path))
			return match
		}
		values, ok := lists[id]
		if !ok {
			failed = fmt.Errorf("missing cl[@id=%s] in %s", id, file)
			return match
		}
		subs++
		return fmt.Sprintf("contains(' %s ', concat(' ', %s, ' '))", values, expr)
	})
	if failed != nil {
		return 0, failed
	}

	if err := os.WriteFile(path, []byte(rewritten), 0o600); err != nil { //nolint:gosec // path comes from a maintainer-supplied --sch-dir flag, not untrusted input.
		return 0, fmt.Errorf("writing: %w", err)
	}
	return subs, nil
}
