package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// The CLDR data the marks come from: the cldr-misc-full package of CLDR
// 48, the release ICU 78 carries, which the pinned Chromium (156) builds
// with, and the digest of its archive.
const (
	pkgURL    = "https://registry.npmjs.org/cldr-misc-full/-/cldr-misc-full-48.0.0.tgz"
	pkgSHA256 = "c72d7aef0022206f0c65bbad9fb3d5524078c89066febb397d94cca9e2983176"
	outGoName = "quotes_gen.go"
	// localesCount is the number of locales the package holds
	// delimiters for.
	localesCount = 766
)

// maxPkgBytes caps the downloaded archive and maxEntryBytes one file of
// it. Anti-DoS guards for a build-time tool, not format limits: the
// archive is 572,948 bytes and a delimiters file under a kilobyte.
const (
	maxPkgBytes   = 16 << 20
	maxEntryBytes = 1 << 20
)

// httpClient bounds how long the generator waits for the download.
var httpClient = &http.Client{Timeout: 5 * time.Minute}

// basic is the marks Chromium prints for a language its locale data
// gives none: “ ” outside, ‘ ’ inside.
const basic = "“”‘’"

// chromiumOverrides are the locales whose marks the pinned Chromium
// prints otherwise than CLDR 48 gives them, as outer open, outer close,
// inner open, inner close. CSS Generated Content 3, 2.1 leaves the marks
// to the user agent, and litho follows Chromium: most of these print the
// basic marks, el-polyton prints Greek's.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 of gotenberg/gotenberg:9.4.1, given a q
//     inside a q under each of the 766 locales of the package, prints the
//     CLDR 48 marks for 651 and these for the other 115.
var chromiumOverrides = map[string]string{
	"agq": basic, "ast": basic, "az-Arab": basic, "az-Arab-IQ": basic, "az-Arab-TR": basic,
	"bas": basic, "be": basic, "be-tarask": basic, "blo": basic, "bm": basic, "br": basic,
	"bs": basic, "bs-Latn": basic, "bua": basic, "cv": basic, "dsb": basic, "dua": basic,
	"dyo": basic, "eo": basic, "eu": basic, "ewo": basic, "ff": basic, "ff-Latn": basic,
	"ff-Latn-BF": basic, "ff-Latn-CM": basic, "ff-Latn-GH": basic, "ff-Latn-GM": basic,
	"ff-Latn-GN": basic, "ff-Latn-GW": basic, "ff-Latn-LR": basic, "ff-Latn-MR": basic,
	"ff-Latn-NE": basic, "ff-Latn-NG": basic, "ff-Latn-SL": basic, "fur": basic, "gsw": basic,
	"gsw-FR": basic, "gsw-LI": basic, "hsb": basic, "ht": basic, "hy": basic, "ia": basic,
	"ie": basic, "is": basic, "jgo": basic, "ka": basic, "kab": basic, "kk": basic,
	"kk-Cyrl": basic, "kk-KZ": basic, "kkj": basic, "ksf": basic, "ksh": basic, "ky": basic,
	"lag": basic, "lb": basic, "lij": basic, "lld": basic, "luy": basic, "mg": basic, "mk": basic,
	"ms-Arab": basic, "ms-Arab-BN": basic, "mua": basic, "mzn": basic, "nds": basic,
	"nds-NL": basic, "nmg": basic, "nnh": basic, "oc": basic, "oc-ES": basic, "os": basic,
	"os-RU": basic, "pms": basic, "prg": basic, "rm": basic, "rn": basic, "rw": basic,
	"sah": basic, "sc": basic, "sdh": basic, "sdh-IQ": basic, "se": basic, "se-FI": basic,
	"se-SE": basic, "sg": basic, "sgs": basic, "shi": basic, "shi-Latn": basic, "shi-Tfng": basic,
	"sn": basic, "st": basic, "st-LS": basic, "syr": basic, "syr-SY": basic, "szl": basic,
	"ti": basic, "tk": basic, "tn": basic, "tn-BW": basic, "tyv": basic, "ug": basic, "uz": basic,
	"uz-Latn": basic, "wae": basic, "yav": basic, "yi": basic, "yue": basic, "yue-Hans": basic,
	"yue-Hant": basic, "yue-Hant-CN": basic, "yue-Hant-MO": basic, "zgh": basic,
	"el-polyton": "«»“”", "ti-ER": "‘’‘’",
}

func main() {
	src := flag.String("src", "", "local copy of the cldr-misc-full archive, instead of downloading it")
	out := flag.String("out", outGoName, "Go file to write")
	flag.Parse()
	if err := run(*src, *out); err != nil {
		fmt.Fprintln(os.Stderr, "gen-quotes:", err)
		os.Exit(1)
	}
}

// run reads the archive and writes the Go file.
func run(src, out string) error {
	data, err := readPkg(src)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != pkgSHA256 {
		return fmt.Errorf("archive digest %s, want %s", got, pkgSHA256)
	}
	locales, license, err := readLocales(data)
	if err != nil {
		return err
	}
	if len(locales) != localesCount {
		return fmt.Errorf("%d locales with delimiters, want %d", len(locales), localesCount)
	}
	for tag, marks := range chromiumOverrides {
		if _, ok := locales[tag]; !ok {
			return fmt.Errorf("override of %s, a locale the package lacks", tag)
		}
		locales[tag] = marks
	}
	code, err := generate(locales, license)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Clean(out), code, 0o600)
}

// readPkg returns the archive from src, or downloaded.
func readPkg(src string) ([]byte, error) {
	if src != "" {
		return os.ReadFile(filepath.Clean(src))
	}
	resp, err := httpClient.Get(pkgURL)
	if err != nil {
		return nil, fmt.Errorf("downloading the package: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // a read-only response body has nothing to flush
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading the package: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxPkgBytes+1))
	if err != nil {
		return nil, fmt.Errorf("downloading the package: %w", err)
	}
	if len(data) > maxPkgBytes {
		return nil, errors.New("the package is larger than expected")
	}
	return data, nil
}

// delimitersFile is the part of a main/<tag>/delimiters.json file the
// generator reads.
type delimitersFile struct {
	Main map[string]struct {
		Delimiters struct {
			QuotationStart          string `json:"quotationStart"`
			QuotationEnd            string `json:"quotationEnd"`
			AlternateQuotationStart string `json:"alternateQuotationStart"`
			AlternateQuotationEnd   string `json:"alternateQuotationEnd"`
		} `json:"delimiters"`
	} `json:"main"`
}

// readLocales returns the marks of every locale of the archive, as outer
// open, outer close, inner open, inner close, keyed by tag, and the
// package's licence.
func readLocales(data []byte) (map[string]string, string, error) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("reading the package: %w", err)
	}
	tr := tar.NewReader(zr)
	locales := map[string]string{}
	license := ""
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, "", fmt.Errorf("reading the package: %w", err)
		}
		if h.Size > maxEntryBytes {
			return nil, "", fmt.Errorf("%s is larger than expected", h.Name)
		}
		name := path.Clean(h.Name)
		isLicense := name == "package/LICENSE"
		dir, file := path.Split(name)
		if !isLicense && (file != "delimiters.json" || path.Dir(path.Clean(dir)) != "package/main") {
			continue
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			return nil, "", fmt.Errorf("reading %s: %w", name, err)
		}
		if isLicense {
			license = string(body)
			continue
		}
		var f delimitersFile
		if err := json.Unmarshal(body, &f); err != nil {
			return nil, "", fmt.Errorf("reading %s: %w", name, err)
		}
		tag := path.Base(dir)
		d, ok := f.Main[tag]
		if !ok {
			return nil, "", fmt.Errorf("%s holds no locale %s", name, tag)
		}
		marks := []string{d.Delimiters.QuotationStart, d.Delimiters.QuotationEnd, d.Delimiters.AlternateQuotationStart, d.Delimiters.AlternateQuotationEnd}
		for _, m := range marks {
			if utf8.RuneCountInString(m) != 1 {
				return nil, "", fmt.Errorf("%s: mark %q is not one character", name, m)
			}
		}
		locales[tag] = strings.Join(marks, "")
	}
	if license == "" {
		return nil, "", errors.New("the package holds no LICENSE")
	}
	return locales, license, nil
}

// generate writes the Go file: the distinct sets of marks, and the set of
// each locale by its lower-case tag.
func generate(locales map[string]string, license string) ([]byte, error) {
	tags := make([]string, 0, len(locales))
	for tag := range locales {
		tags = append(tags, tag)
	}
	slices.Sort(tags)
	var sets []string
	index := map[string]int{}
	for _, tag := range tags {
		m := locales[tag]
		if _, ok := index[m]; !ok {
			index[m] = len(sets)
			sets = append(sets, m)
		}
	}
	var b bytes.Buffer
	b.WriteString("// Code generated by gen-quotes; DO NOT EDIT.\n\n")
	b.WriteString("// The marks come from the Unicode CLDR 48 (cldr-misc-full 48.0.0), under\n// this notice:\n//\n")
	for line := range strings.SplitSeq(strings.TrimSpace(license), "\n") {
		b.WriteString(strings.TrimRight("// "+line, " ") + "\n")
	}
	b.WriteString("\npackage html\n\n")
	b.WriteString("// quoteSets are the distinct pairs of quotes: auto, outer then inner.\n")
	b.WriteString("var quoteSets = [...][2][2]string{\n")
	for _, m := range sets {
		r := []rune(m)
		fmt.Fprintf(&b, "\t{{%q, %q}, {%q, %q}},\n", string(r[0]), string(r[1]), string(r[2]), string(r[3]))
	}
	b.WriteString("}\n\n")
	b.WriteString("// quoteLocales maps a lower-case BCP 47 tag to its index in quoteSets.\n")
	b.WriteString("// Immutable lookup table.\n")
	b.WriteString("var quoteLocales = map[string]uint8{\n")
	for _, tag := range tags {
		fmt.Fprintf(&b, "\t%q: %d,\n", strings.ToLower(tag), index[locales[tag]])
	}
	b.WriteString("}\n")
	if len(sets) > 256 {
		return nil, fmt.Errorf("%d sets of marks, more than a uint8 indexes", len(sets))
	}
	return format.Source(b.Bytes())
}
