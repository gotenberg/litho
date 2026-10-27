package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The RFC 3454 text the tables come from, as the RFC Editor publishes it. An
// RFC never changes once published, so the digest pins the input exactly.
const (
	rfcURL    = "https://www.rfc-editor.org/rfc/rfc3454.txt"
	rfcSHA256 = "eb722fa698fb7e8823b835d9fd263e4cdb8f1c7b0d234edf7f0e3bd2ccbb2c79"
	outGoName = "tables.go"
)

// maxRFCBytes caps the downloaded text. Anti-DoS guard for a build-time tool,
// not a format limit: RFC 3454 is 138684 bytes, so 1 MiB refuses a runaway
// response with room to spare.
const maxRFCBytes = 1 << 20

// httpClient bounds how long the generator waits for the RFC Editor. The bare
// http.Get default has no timeout.
var httpClient = &http.Client{Timeout: 30 * time.Second}

var (
	errDigestMismatch = errors.New("SHA-256 differs from the pinned digest")
	errTooLarge       = errors.New("larger than the download cap")
	errBadLine        = errors.New("table line is not a code point or a range")
	errMissingTable   = errors.New("table not found")
	errUnsorted       = errors.New("table is not in ascending code point order")
)

// set is one table of the generated file: the Go variable it becomes, its
// doc comment, and the RFC 3454 tables whose union it holds.
type set struct {
	Name   string
	Doc    string
	Tables []string
}

// sets lists the tables RFC 4013, 2 names for SASLprep. prohibited is the
// union RFC 4013, 2.3 lists.
var sets = []set{
	{"unassigned", "unassigned holds the code points RFC 3454, Table A.1 lists as unassigned in Unicode 3.2, which RFC 4013, 2.5 adopts.", []string{"A.1"}},
	{"mappedToNothing", "mappedToNothing holds RFC 3454, Table B.1, the characters RFC 4013, 2.1 maps to nothing.", []string{"B.1"}},
	{"nonASCIISpace", "nonASCIISpace holds RFC 3454, Table C.1.2, the non-ASCII spaces RFC 4013, 2.1 maps to U+0020.", []string{"C.1.2"}},
	{"prohibited", "prohibited holds the union of RFC 3454, Tables C.1.2, C.2.1, C.2.2, C.3, C.4, C.5, C.6, C.7, C.8 and C.9, the output RFC 4013, 2.3 prohibits.", []string{"C.1.2", "C.2.1", "C.2.2", "C.3", "C.4", "C.5", "C.6", "C.7", "C.8", "C.9"}},
	{"randALCat", "randALCat holds RFC 3454, Table D.1, the characters of bidirectional category R or AL.", []string{"D.1"}},
	{"lCat", "lCat holds RFC 3454, Table D.2, the characters of bidirectional category L.", []string{"D.2"}},
}

// copyrightNotice is the notice RFC 3454's Full Copyright Statement asks every
// derivative work to carry, with the paragraph granting the permission.
const copyrightNotice = `Copyright (C) The Internet Society (2002). All Rights Reserved. This ` +
	`document and translations of it may be copied and furnished to others, and derivative ` +
	`works that comment on or otherwise explain it or assist in its implementation may be ` +
	`prepared, copied, published and distributed, in whole or in part, without restriction ` +
	`of any kind, provided that the above copyright notice and this paragraph are included ` +
	`on all such copies and derivative works.`

func main() {
	src := flag.String("src", "", "read the RFC text from this file instead of downloading it")
	out := flag.String("out", "", "saslprep package directory (default: pkg/internal/crypto/saslprep)")
	flag.Parse()

	pkgDir := *out
	if pkgDir == "" {
		pkgDir = findPkgDir()
	}
	text, err := fetch(*src)
	if err != nil {
		fatalf("%v", err)
	}
	tables, err := parseTables(text)
	if err != nil {
		fatalf("%v", err)
	}
	code, err := generate(tables)
	if err != nil {
		fatalf("%v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, outGoName), code, 0o600); err != nil {
		fatalf("writing %s: %v", outGoName, err)
	}
	fmt.Printf("Generated %s from %s\n", outGoName, rfcURL)
}

// fetch returns the RFC text, read from path when it is set and downloaded
// otherwise, after checking it against [rfcSHA256].
func fetch(path string) ([]byte, error) {
	var data []byte
	if path != "" {
		b, err := os.ReadFile(path) //nolint:gosec // the path is the developer's own command-line flag
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		data = b
	} else {
		b, err := download(rfcURL)
		if err != nil {
			return nil, err
		}
		data = b
	}
	if err := verify(data, rfcSHA256); err != nil {
		return nil, err
	}
	return data, nil
}

// download returns the body at url, refusing one past [maxRFCBytes].
func download(url string) ([]byte, error) {
	resp, err := httpClient.Get(url) //nolint:gosec // the URL is a package constant
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: HTTP %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxRFCBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", url, err)
	}
	if len(data) > maxRFCBytes {
		return nil, fmt.Errorf("downloading %s: %w", url, errTooLarge)
	}
	return data, nil
}

// verify reports whether data has the pinned digest.
func verify(data []byte, want string) error {
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != want {
		return fmt.Errorf("RFC 3454: %w: got %s, want %s", errDigestMismatch, got, want)
	}
	return nil
}

// codeRange is an inclusive range of code points.
type codeRange struct{ Lo, Hi rune }

var (
	startRe = regexp.MustCompile(`^----- Start Table (\S+) -----$`)
	endRe   = regexp.MustCompile(`^----- End Table (\S+) -----$`)
	// entryRe is one table line: a code point or a range of them, then, in
	// the B and C tables, a semicolon and the rest of the row.
	entryRe = regexp.MustCompile(`^([0-9A-F]{4,6})(?:-([0-9A-F]{4,6}))?(?:;.*)?$`)
)

// isPageBreak reports whether line belongs to the page footer and header the
// RFC text interleaves with the tables.
func isPageBreak(line string) bool {
	return line == "" || strings.HasPrefix(line, "Hoffman & Blanchet") || strings.HasPrefix(line, "RFC 3454")
}

// parseTables returns every table of the RFC text by name, each range in the
// order the RFC lists it.
func parseTables(text []byte) (map[string][]codeRange, error) {
	tables := map[string][]codeRange{}
	var cur string
	for n, raw := range strings.Split(string(text), "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r\f"))
		line = strings.TrimLeft(line, "\f")
		if m := startRe.FindStringSubmatch(line); m != nil {
			cur = m[1]
			tables[cur] = nil
			continue
		}
		if m := endRe.FindStringSubmatch(line); m != nil {
			cur = ""
			continue
		}
		if cur == "" || isPageBreak(line) {
			continue
		}
		m := entryRe.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("table %s, line %d %q: %w", cur, n+1, line, errBadLine)
		}
		lo, err := strconv.ParseUint(m[1], 16, 32)
		if err != nil {
			return nil, fmt.Errorf("table %s, line %d: %w", cur, n+1, err)
		}
		hi := lo
		if m[2] != "" {
			if hi, err = strconv.ParseUint(m[2], 16, 32); err != nil {
				return nil, fmt.Errorf("table %s, line %d: %w", cur, n+1, err)
			}
		}
		if hi < lo || hi > 0x10FFFF {
			return nil, fmt.Errorf("table %s, line %d %q: %w", cur, n+1, line, errBadLine)
		}
		tables[cur] = append(tables[cur], codeRange{rune(lo), rune(hi)}) //nolint:gosec // both bounds are checked against 0x10FFFF above
	}
	return tables, nil
}

// union merges the named tables into sorted, disjoint ranges, joining ranges
// that touch. Each table must list its own ranges in ascending order, as the
// RFC does, so a misread line shows up as an ordering error.
func union(tables map[string][]codeRange, names []string) ([]codeRange, error) {
	var all []codeRange
	for _, name := range names {
		t, ok := tables[name]
		if !ok || len(t) == 0 {
			return nil, fmt.Errorf("table %s: %w", name, errMissingTable)
		}
		for i := 1; i < len(t); i++ {
			if t[i].Lo <= t[i-1].Hi {
				return nil, fmt.Errorf("table %s at %04X: %w", name, t[i].Lo, errUnsorted)
			}
		}
		all = append(all, t...)
	}
	slices.SortFunc(all, func(a, b codeRange) int { return int(a.Lo - b.Lo) })
	var merged []codeRange
	for _, r := range all {
		if k := len(merged) - 1; k >= 0 && r.Lo <= merged[k].Hi+1 {
			merged[k].Hi = max(merged[k].Hi, r.Hi)
			continue
		}
		merged = append(merged, r)
	}
	return merged, nil
}

// generate renders the gofmt-formatted tables file.
func generate(tables map[string][]codeRange) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("// Code generated by cmd/gen-saslprep; DO NOT EDIT.\n\n")
	buf.WriteString(comment(fmt.Sprintf("Transcribed from RFC 3454, Appendices A to D (%s, SHA-256 %s). %s",
		rfcURL, rfcSHA256, copyrightNotice)))
	buf.WriteString("\npackage saslprep\n\n")
	for _, s := range sets {
		ranges, err := union(tables, s.Tables)
		if err != nil {
			return nil, err
		}
		buf.WriteString(comment(s.Doc))
		fmt.Fprintf(&buf, "var %s = []runeRange{\n", s.Name)
		for _, r := range ranges {
			fmt.Fprintf(&buf, "\t{0x%04X, 0x%04X},\n", r.Lo, r.Hi)
		}
		buf.WriteString("}\n\n")
	}
	code, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("formatting generated source: %w", err)
	}
	return code, nil
}

// commentWidth is the column the generated comments wrap at.
const commentWidth = 76

// comment renders text as "//" comment lines wrapped at [commentWidth].
func comment(text string) string {
	var b strings.Builder
	line := "//"
	for word := range strings.FieldsSeq(text) {
		if len(line) > len("//") && len(line)+1+len(word) > commentWidth {
			b.WriteString(line + "\n")
			line = "//"
		}
		line += " " + word
	}
	b.WriteString(line + "\n")
	return b.String()
}

func findPkgDir() string {
	for _, c := range []string{".", "pkg/internal/crypto/saslprep"} {
		if _, err := os.Stat(filepath.Join(c, "saslprep.go")); err == nil {
			return c
		}
	}
	fatalf("cannot find the saslprep package directory; run from the repo root or pkg/internal/crypto/saslprep/")
	return ""
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "gen-saslprep: "+format+"\n", args...)
	os.Exit(1)
}
