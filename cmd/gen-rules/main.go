// Command gen-rules generates rules.go from VeraPDF validation profile XML files.
//
// Usage:
//
//	go run ./cmd/gen-rules                          # use local profiles/
//	go run ./cmd/gen-rules --download               # download latest profiles first
//	go run ./cmd/gen-rules --download --tag v1.28.2  # download specific release
//	go generate ./pkg/internal/compliance/...
package main

import (
	"bytes"
	"encoding/xml"
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
)

const (
	defaultRepo = "https://raw.githubusercontent.com/veraPDF/veraPDF-validation-profiles"
	defaultTag  = "integration"
)

// --- XML schema types matching VeraPDF validation profiles ---

type xmlProfile struct {
	XMLName xml.Name  `xml:"profile"`
	Flavour string    `xml:"flavour,attr"`
	Rules   []xmlRule `xml:"rules>rule"`
}

type xmlRule struct {
	Object      string    `xml:"object,attr"`
	ID          xmlRuleID `xml:"id"`
	Description string    `xml:"description"`
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

	allRules, profiles := parseProfiles(profilesDir)
	generate(allRules, profiles)
}

// downloadProfiles fetches all 13 XML profiles from the VeraPDF repository.
func downloadProfiles(baseURL, tag, destDir string) {
	fmt.Printf("Downloading profiles from %s (ref: %s)\n", baseURL, tag)

	for _, pf := range profileFiles {
		url := fmt.Sprintf("%s/%s/%s", baseURL, tag, pf.Path)
		destPath := filepath.Join(destDir, pf.Path)

		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			fatalf("creating directory for %s: %v", destPath, err)
		}

		resp, err := http.Get(url) //nolint:gosec // URL is built from trusted constants + user flag
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

		if err := os.WriteFile(destPath, data, 0o644); err != nil {
			fatalf("writing %s: %v", destPath, err)
		}

		fmt.Printf("  %s (%d bytes)\n", pf.Path, len(data))
	}

	fmt.Println("Download complete.")
}

// parseProfiles reads all XML profile files and collects rules.
func parseProfiles(profilesDir string) (map[ruleKey]*ruleData, []profileEntry) {
	allRules := make(map[ruleKey]*ruleData)
	profiles := make([]profileEntry, 0, len(profileFiles))

	for _, pf := range profileFiles {
		path := filepath.Join(profilesDir, pf.Path)
		data, err := os.ReadFile(path)
		if err != nil {
			fatalf("reading %s: %v", path, err)
		}

		var profile xmlProfile
		if err := xml.Unmarshal(data, &profile); err != nil {
			fatalf("parsing %s: %v", path, err)
		}

		if profile.Flavour != pf.Flavour {
			fatalf("%s: expected flavour %q, got %q", path, pf.Flavour, profile.Flavour)
		}

		funcName, ok := flavourToFuncName[profile.Flavour]
		if !ok {
			fatalf("unknown flavour %q in %s", profile.Flavour, path)
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

	return allRules, profiles
}

// generate writes the rules.go file from parsed profile data.
func generate(allRules map[ruleKey]*ruleData, profiles []profileEntry) {
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
		if wErr := os.WriteFile("rules_gen_debug.go", buf.Bytes(), 0o600); wErr != nil {
			fatalf("gofmt failed: %v (could not write debug file: %v)", err, wErr)
		}
		fatalf("gofmt failed: %v (unformatted output written to rules_gen_debug.go)", err)
	}

	outPath := filepath.Join(findComplianceDir(), "rules.go")
	if err := os.WriteFile(outPath, formatted, 0o600); err != nil {
		fatalf("writing %s: %v", outPath, err)
	}

	fmt.Printf("Generated %s (%d rules, %d profiles)\n", outPath, len(sortedRules), len(profiles))
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
