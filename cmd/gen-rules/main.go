package main

import (
	"bytes"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// httpClient is the default HTTP client for downloading VeraPDF
// profiles. The 30 s timeout caps how long the codegen step blocks
// when the upstream raw.githubusercontent.com host is slow or
// unreachable; the bare http.Get default has no timeout.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// errGofmt signals that the generator produced syntactically invalid Go.
// main() catches this and writes the unformatted buffer to disk for
// debugging.
var errGofmt = errors.New("gofmt rejected generated source")

const (
	defaultRepo = "https://raw.githubusercontent.com/veraPDF/veraPDF-validation-profiles"
	defaultTag  = "integration"
)

type xmlProfile struct {
	XMLName xml.Name  `xml:"profile"`
	Flavour string    `xml:"flavour,attr"`
	Rules   []xmlRule `xml:"rules>rule"`
}

type xmlRule struct {
	Object      string    `xml:"object,attr"`
	ID          xmlRuleID `xml:"id"`
	Description string    `xml:"description"`
	Test        string    `xml:"test"`
}

type xmlRuleID struct {
	Specification string `xml:"specification,attr"`
	Clause        string `xml:"clause,attr"`
	TestNumber    int    `xml:"testNumber,attr"`
}

type ruleKey struct {
	Specification string
	Clause        string
	TestNumber    int
}

type ruleData struct {
	Key         ruleKey
	Object      string
	Description string
	VarName     string
}

type profileEntry struct {
	Flavour  string
	FuncName string
	RuleKeys []ruleKey
}

// profileFiles defines the XML files to parse and their order.
var profileFiles = []struct {
	Path    string // relative to profiles dir (and raw URL path)
	Flavour string // expected flavour attribute
}{
	{"PDF_A/PDFA-1A.xml", "PDFA_1_A"},
	{"PDF_A/PDFA-1B.xml", "PDFA_1_B"},
	{"PDF_A/PDFA-2A.xml", "PDFA_2_A"},
	{"PDF_A/PDFA-2B.xml", "PDFA_2_B"},
	{"PDF_A/PDFA-2U.xml", "PDFA_2_U"},
	{"PDF_A/PDFA-3A.xml", "PDFA_3_A"},
	{"PDF_A/PDFA-3B.xml", "PDFA_3_B"},
	{"PDF_A/PDFA-3U.xml", "PDFA_3_U"},
	{"PDF_A/PDFA-4.xml", "PDFA_4"},
	{"PDF_A/PDFA-4E.xml", "PDFA_4_E"},
	{"PDF_A/PDFA-4F.xml", "PDFA_4_F"},
	{"PDF_UA/PDFUA-1.xml", "PDFUA_1"},
	{"PDF_UA/PDFUA-2.xml", "PDFUA_2"},
}

// iso32005ProfilePath is the standalone ISO/TS 32005 profile. It is not part
// of [profileFiles]: its rules do not become Rule vars in rules.go. Instead
// [generateISO32005] derives the Table 5 parent-child inclusion matrix from
// its test expressions into rules_iso32005.go. veraPDF's ua2 flavour validates
// the combined PDFUA-2-ISO32005.xml profile, which is exactly PDFUA-2.xml plus
// the "Table 5. *" rules of this file (the clause 1, 5.2 and 7.2 rules are
// excluded from the combination), so litho mirrors that pairing.
const iso32005ProfilePath = "PDF_UA/ISO-32005-Tagged.xml"

var flavourToFuncName = map[string]string{
	"PDFA_1_A": "PDFA1aRules",
	"PDFA_1_B": "PDFA1bRules",
	"PDFA_2_A": "PDFA2aRules",
	"PDFA_2_B": "PDFA2bRules",
	"PDFA_2_U": "PDFA2uRules",
	"PDFA_3_A": "PDFA3aRules",
	"PDFA_3_B": "PDFA3bRules",
	"PDFA_3_U": "PDFA3uRules",
	"PDFA_4":   "PDFA4Rules",
	"PDFA_4_E": "PDFA4eRules",
	"PDFA_4_F": "PDFA4fRules",
	"PDFUA_1":  "PDFUA1Rules",
	"PDFUA_2":  "PDFUA2Rules",
}

var whitespaceRE = regexp.MustCompile(`\s+`)

func main() {
	download := flag.Bool("download", false, "download profiles from the VeraPDF GitHub repository before generating")
	tag := flag.String("tag", defaultTag, "Git ref (branch, tag, or commit) to download from")
	repo := flag.String("repo", defaultRepo, "base URL for raw profile downloads")
	flag.Parse()

	profilesDir := findProfilesDir()

	if *download {
		downloadProfiles(*repo, *tag, profilesDir)
	}

	allRules, profiles, err := parseProfiles(profilesDir)
	if err != nil {
		fatalf("%v", err)
	}

	formatted, err := generate(allRules, profiles)
	if err != nil {
		// Write the unformatted buffer to disk for debugging when gofmt
		// rejects the generated source.
		if errors.Is(err, errGofmt) && len(formatted) > 0 {
			if wErr := os.WriteFile("rules_gen_debug.go", formatted, 0o600); wErr != nil {
				fatalf("gofmt failed: %v (could not write debug file: %v)", err, wErr)
			}
			fatalf("gofmt failed: %v (unformatted output written to rules_gen_debug.go)", err)
		}
		fatalf("%v", err)
	}

	outPath := filepath.Join(findComplianceDir(), "rules.go")
	if err := os.WriteFile(outPath, formatted, 0o600); err != nil {
		fatalf("writing %s: %v", outPath, err)
	}

	fmt.Printf("Generated %s (%d rules, %d profiles)\n", outPath, len(allRules), len(profiles))

	isoOutPath := filepath.Join(findComplianceDir(), "rules_iso32005.go")
	pairCount, err := generateISO32005(filepath.Join(profilesDir, iso32005ProfilePath), isoOutPath)
	if err != nil {
		fatalf("%v", err)
	}
	fmt.Printf("Generated %s (%d Table 5 rules)\n", isoOutPath, pairCount)
}

// downloadProfiles fetches all 13 XML profiles plus the standalone
// ISO/TS 32005 profile from the VeraPDF repository.
func downloadProfiles(baseURL, tag, destDir string) {
	fmt.Printf("Downloading profiles from %s (ref: %s)\n", baseURL, tag)

	paths := make([]string, 0, len(profileFiles)+1)
	for _, pf := range profileFiles {
		paths = append(paths, pf.Path)
	}
	paths = append(paths, iso32005ProfilePath)

	for _, path := range paths {
		url := fmt.Sprintf("%s/%s/%s", baseURL, tag, path)
		destPath := filepath.Join(destDir, path)

		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			fatalf("creating directory for %s: %v", destPath, err)
		}

		resp, err := httpClient.Get(url) //nolint:gosec // URL is built from trusted constants + user flag
		if err != nil {
			fatalf("downloading %s: %v", url, err)
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			fatalf("downloading %s: HTTP %d", url, resp.StatusCode)
		}

		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			fatalf("reading response from %s: %v", url, err)
		}

		if err := os.WriteFile(destPath, data, 0o600); err != nil {
			fatalf("writing %s: %v", destPath, err)
		}

		fmt.Printf("  %s (%d bytes)\n", path, len(data))
	}

	fmt.Println("Download complete.")
}

// parseProfiles reads all XML profile files and collects rules. Errors
// from missing or malformed files are returned rather than printed,
// so callers (including tests) can react to them.
func parseProfiles(profilesDir string) (map[ruleKey]*ruleData, []profileEntry, error) {
	allRules := make(map[ruleKey]*ruleData)
	profiles := make([]profileEntry, 0, len(profileFiles))

	for _, pf := range profileFiles {
		path := filepath.Join(profilesDir, pf.Path)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, fmt.Errorf("reading %s: %w", path, err)
		}

		var profile xmlProfile
		if err := xml.Unmarshal(data, &profile); err != nil {
			return nil, nil, fmt.Errorf("parsing %s: %w", path, err)
		}

		if profile.Flavour != pf.Flavour {
			return nil, nil, fmt.Errorf("%s: expected flavour %q, got %q", path, pf.Flavour, profile.Flavour)
		}

		funcName, ok := flavourToFuncName[profile.Flavour]
		if !ok {
			return nil, nil, fmt.Errorf("unknown flavour %q in %s", profile.Flavour, path)
		}

		entry := profileEntry{
			Flavour:  profile.Flavour,
			FuncName: funcName,
		}

		for _, r := range profile.Rules {
			key := ruleKey{
				Specification: r.ID.Specification,
				Clause:        r.ID.Clause,
				TestNumber:    r.ID.TestNumber,
			}

			desc := normalizeWhitespace(r.Description)

			if existing, ok := allRules[key]; ok {
				if existing.Object != r.Object {
					fmt.Fprintf(os.Stderr, "WARNING: rule %s has different objects: %q vs %q\n",
						existing.VarName, existing.Object, r.Object)
				}
			} else {
				allRules[key] = &ruleData{
					Key:         key,
					Object:      r.Object,
					Description: desc,
					VarName:     makeVarName(key),
				}
			}

			entry.RuleKeys = append(entry.RuleKeys, key)
		}

		profiles = append(profiles, entry)
	}

	return allRules, profiles, nil
}

// generate produces the rules.go source from parsed profile data.
// On gofmt failure, it returns the unformatted buffer alongside [errGofmt]
// so the caller can persist it for debugging.
func generate(allRules map[ruleKey]*ruleData, profiles []profileEntry) ([]byte, error) {
	sortedRules := make([]*ruleData, 0, len(allRules))
	for _, r := range allRules {
		sortedRules = append(sortedRules, r)
	}
	sort.Slice(sortedRules, func(i, j int) bool {
		return sortedRules[i].VarName < sortedRules[j].VarName
	})

	var buf bytes.Buffer
	buf.WriteString("// Code generated by cmd/gen-rules; DO NOT EDIT.\n\n")
	buf.WriteString("package compliance\n\n")

	buf.WriteString("// Rule represents a single VeraPDF validation rule.\n")
	buf.WriteString("type Rule struct {\n")
	buf.WriteString("\t// Specification is the ISO standard identifier (e.g., \"ISO_19005_1\").\n")
	buf.WriteString("\tSpecification string\n\n")
	buf.WriteString("\t// Clause is the standard clause number (e.g., \"6.1.2\").\n")
	buf.WriteString("\tClause string\n\n")
	buf.WriteString("\t// TestNumber identifies the specific test within a clause.\n")
	buf.WriteString("\tTestNumber int\n\n")
	buf.WriteString("\t// Object is the VeraPDF object type (e.g., \"CosDocument\").\n")
	buf.WriteString("\tObject string\n\n")
	buf.WriteString("\t// Description is the normative requirement text from the ISO standard.\n")
	buf.WriteString("\tDescription string\n")
	buf.WriteString("}\n\n")

	buf.WriteString("//nolint:gochecknoglobals // package-level rule definitions are the core design.\n")
	buf.WriteString("var (\n")

	for _, r := range sortedRules {
		fmt.Fprintf(&buf, "\t%s = Rule{\n", r.VarName)
		fmt.Fprintf(&buf, "\t\tSpecification: %q,\n", r.Key.Specification)
		fmt.Fprintf(&buf, "\t\tClause:        %q,\n", r.Key.Clause)
		fmt.Fprintf(&buf, "\t\tTestNumber:    %d,\n", r.Key.TestNumber)
		fmt.Fprintf(&buf, "\t\tObject:        %q,\n", r.Object)
		fmt.Fprintf(&buf, "\t\tDescription:   %q,\n", r.Description)
		buf.WriteString("\t}\n\n")
	}

	buf.WriteString(")\n\n")

	for _, p := range profiles {
		fmt.Fprintf(&buf, "// %s returns the rule set for %s (%d rules).\n", p.FuncName, flavourLabel(p.Flavour), len(p.RuleKeys))
		fmt.Fprintf(&buf, "func %s() []Rule {\n", p.FuncName)
		buf.WriteString("\treturn []Rule{\n")
		for _, key := range p.RuleKeys {
			r := allRules[key]
			fmt.Fprintf(&buf, "\t\t%s,\n", r.VarName)
		}
		buf.WriteString("\t}\n")
		buf.WriteString("}\n\n")
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return buf.Bytes(), fmt.Errorf("%w: %v", errGofmt, err)
	}
	return formatted, nil
}

func makeVarName(key ruleKey) string {
	spec := strings.TrimPrefix(key.Specification, "ISO_")
	clause := strings.ReplaceAll(key.Clause, ".", "_")
	return fmt.Sprintf("Rule_%s_%s_t%02d", spec, clause, key.TestNumber)
}

func normalizeWhitespace(s string) string {
	return strings.TrimSpace(whitespaceRE.ReplaceAllString(s, " "))
}

func flavourLabel(flavour string) string {
	labels := map[string]string{
		"PDFA_1_A": "PDF/A-1a", "PDFA_1_B": "PDF/A-1b",
		"PDFA_2_A": "PDF/A-2a", "PDFA_2_B": "PDF/A-2b", "PDFA_2_U": "PDF/A-2u",
		"PDFA_3_A": "PDF/A-3a", "PDFA_3_B": "PDF/A-3b", "PDFA_3_U": "PDF/A-3u",
		"PDFA_4": "PDF/A-4", "PDFA_4_E": "PDF/A-4e", "PDFA_4_F": "PDF/A-4f",
		"PDFUA_1": "PDF/UA-1", "PDFUA_2": "PDF/UA-2",
	}
	if l, ok := labels[flavour]; ok {
		return l
	}
	return flavour
}

func findProfilesDir() string {
	for _, c := range []string{"profiles", "pkg/internal/compliance/profiles"} {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c
		}
	}
	fatalf("cannot find profiles directory; run from repo root or pkg/internal/compliance/")
	return ""
}

func findComplianceDir() string {
	for _, c := range []string{".", "pkg/internal/compliance"} {
		if _, err := os.Stat(filepath.Join(c, "check.go")); err == nil {
			return c
		}
	}
	fatalf("cannot find compliance package directory")
	return ""
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "gen-rules: "+format+"\n", args...)
	os.Exit(1)
}

// iso32005Shape classifies a Table 5 test expression into one of the eight
// mechanical shapes the ISO/TS 32005 profile uses. Anything else is
// reported and skipped so a profile update cannot silently emit a wrong
// check.
type iso32005Entry struct {
	Clause      string // verbatim, e.g. "Table 5. Annot-Document"
	TestNumber  int
	Object      string
	Description string
	Kind        string // forbiddenParent, forbiddenParentHn, kidZeroUnlessGrouping, kidMaxOne, kidMaxOneIfGrouping, kidZeroUnlessGroupingHn, kidExactlyOne, noContent
	Parent      string // standard type from the clause's "Parent-Child" pair ("Hn" for numbered headings)
	Child       string // "" for noContent; "Hn" for numbered headings; "content" for content rules
}

var iso32005ShapeRes = []struct {
	kind string
	re   *regexp.Regexp
}{
	{"forbiddenParent", regexp.MustCompile(`^parentStandardType != '([^']+)'$`)},
	{"forbiddenParentHn", regexp.MustCompile(`^/\^H\[1-9\]\[0-9\]\*\$/\.test\(parentStandardType\) == false$`)},
	{"kidZeroUnlessGrouping", regexp.MustCompile(`^isGrouping == true \|\| kidsStandardTypes\.split\('&'\)\.filter\(elem => elem == '([^']+)'\)\.length == 0$`)},
	{"kidMaxOne", regexp.MustCompile(`^kidsStandardTypes\.split\('&'\)\.filter\(elem => elem == '([^']+)'\)\.length <= 1$`)},
	{"kidMaxOneIfGrouping", regexp.MustCompile(`^isGrouping == false \|\| kidsStandardTypes\.split\('&'\)\.filter\(elem => elem == '([^']+)'\)\.length <= 1$`)},
	{"kidZeroUnlessGroupingHn", regexp.MustCompile(`^isGrouping == true \|\| kidsStandardTypes\.split\('&'\)\.filter\(elem => /\^H\[1-9\]\[0-9\]\*\$/\.test\(elem\)\)\.length == 0$`)},
	{"kidExactlyOne", regexp.MustCompile(`^kidsStandardTypes\.split\('&'\)\.filter\(elem => elem == '([^']+)'\)\.length == 1$`)},
	{"noContent", regexp.MustCompile(`^hasContentItems == false$`)},
}

// generateISO32005 parses the standalone ISO/TS 32005 profile and emits
// rules_iso32005.go: the Table 5 parent-child inclusion entries as one
// sorted table the PDF/UA-2 structure walk consumes. Returns the entry
// count.
func generateISO32005(profilePath, outPath string) (int, error) {
	data, err := os.ReadFile(profilePath) //nolint:gosec // path is repo-internal
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", profilePath, err)
	}
	var profile xmlProfile
	if err := xml.Unmarshal(data, &profile); err != nil {
		return 0, fmt.Errorf("parsing %s: %w", profilePath, err)
	}

	var entries []iso32005Entry
	skipped := 0
	for _, r := range profile.Rules {
		if !strings.HasPrefix(r.ID.Clause, "Table 5. ") {
			continue // clause 1 / 5.2 / 7.2 rules are covered elsewhere
		}
		pair := strings.TrimPrefix(r.ID.Clause, "Table 5. ")
		dash := strings.LastIndex(pair, "-")
		if dash < 0 {
			skipped++
			continue
		}
		parent, child := pair[:dash], pair[dash+1:]
		test := strings.Join(strings.Fields(r.Test), " ")

		kind := ""
		for _, sh := range iso32005ShapeRes {
			if m := sh.re.FindStringSubmatch(test); m != nil {
				kind = sh.kind
				break
			}
		}
		if kind == "" {
			fmt.Printf("  iso32005: unrecognized test shape for %q: %s\n", r.ID.Clause, test)
			skipped++
			continue
		}
		entries = append(entries, iso32005Entry{
			Clause:      r.ID.Clause,
			TestNumber:  r.ID.TestNumber,
			Object:      r.Object,
			Description: strings.Join(strings.Fields(r.Description), " "),
			Kind:        kind,
			Parent:      parent,
			Child:       child,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Clause != entries[j].Clause {
			return entries[i].Clause < entries[j].Clause
		}
		return entries[i].TestNumber < entries[j].TestNumber
	})

	var buf bytes.Buffer
	buf.WriteString("// Code generated by cmd/gen-rules. DO NOT EDIT.\n")
	buf.WriteString("//\n")
	buf.WriteString("// ISO/TS 32005 Table 5 structure-type inclusion entries, derived from\n")
	buf.WriteString("// the veraPDF profile PDF_UA/ISO-32005-Tagged.xml. veraPDF's ua2\n")
	buf.WriteString("// flavour validates PDFUA-2-ISO32005.xml, which is PDFUA-2.xml plus\n")
	buf.WriteString("// these rules, so litho mirrors that pairing for PDF/UA-2 runs.\n")
	buf.WriteString("package compliance\n\n")
	buf.WriteString("// iso32005Entry is one Table 5 rule: a parent-child inclusion\n")
	buf.WriteString("// constraint (or content-item prohibition) plus the Rule it emits.\n")
	buf.WriteString("type iso32005Entry struct {\n")
	buf.WriteString("\tKind   string\n")
	buf.WriteString("\tParent string\n")
	buf.WriteString("\tChild  string\n")
	buf.WriteString("\tRule   Rule\n")
	buf.WriteString("}\n\n")
	buf.WriteString("//nolint:gochecknoglobals // generated spec table\n")
	buf.WriteString("var iso32005Entries = []iso32005Entry{\n")
	for _, e := range entries {
		fmt.Fprintf(&buf, "\t{Kind: %q, Parent: %q, Child: %q, Rule: Rule{Specification: %q, Clause: %q, TestNumber: %d, Object: %q, Description: %q}},\n",
			e.Kind, e.Parent, e.Child, "ISO_32005", e.Clause, e.TestNumber, e.Object, e.Description)
	}
	buf.WriteString("}\n")

	if err := os.WriteFile(outPath, buf.Bytes(), 0o644); err != nil { //nolint:gosec // generated source file
		return 0, fmt.Errorf("writing %s: %w", outPath, err)
	}
	if skipped > 0 {
		fmt.Printf("  iso32005: skipped %d rules (unparsed shapes)\n", skipped)
	}
	return len(entries), nil
}
