package main

import (
	"bytes"
	"compress/gzip"
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
	"strings"
	"time"
)

// The mapping-resources-pdf revision the resources come from. The download
// uses the commit, which cannot move.
const (
	adobeRepo   = "https://github.com/adobe-type-tools/mapping-resources-pdf"
	adobeCommit = "2dd5e53fb74a01718b9dfd448a0d1cce6fff2aa5"
	rawBaseURL  = "https://raw.githubusercontent.com/adobe-type-tools/mapping-resources-pdf"
	sourceDir   = "pdf2unicode"
	licenseName = "LICENSE.txt"
	outDirName  = "adobeucs2"
	outGoName   = "adobe_ucs2_sources.go"
)

// maxResourceBytes caps one downloaded file. Anti-DoS guard for a build-time
// tool, not a format limit: the largest resource, Adobe-Japan1-UCS2, is
// 294111 bytes, so 1 MiB leaves room for upstream growth while refusing a
// runaway response.
const maxResourceBytes = 1 << 20

// httpClient bounds how long the generator waits for GitHub. The bare
// http.Get default has no timeout.
var httpClient = &http.Client{Timeout: 30 * time.Second}

var (
	errDigestMismatch = errors.New("SHA-256 differs from the pinned digest")
	errTooLarge       = errors.New("larger than the download cap")
)

// resource names one file of the pinned commit and its SHA-256.
type resource struct {
	Name   string // file name, also the CMap name
	SHA256 string // hex digest of the file as published
}

// resources lists the pdf2unicode CMaps veraPDF 1.30 bundles, one per Adobe
// character collection it resolves a /ToUnicode base CMap for. The digests are
// those of the files at [adobeCommit]; veraPDF's copies are byte-identical
// except Adobe-Japan1-UCS2, whose copy declares 41 entries for a bfrange
// block of 42.
var resources = []resource{
	{"Adobe-CNS1-UCS2", "8375afd535e153a7e3bcf448be93e5012b26229547a20d52520e46e3c386351f"},
	{"Adobe-GB1-UCS2", "368b40ec05568faf323cfbbddf23c60d566bbc88e366981eb6541b026381a3b8"},
	{"Adobe-Japan1-UCS2", "6a9693361647a37996312cc57071bb79f8c06411207be7c730a83fda1254cd82"},
	{"Adobe-KR-UCS2", "6861a3208f331ecd73369c5d51eacf3b72c820fe3c87524f79c9ac1bb16830ec"},
	{"Adobe-Korea1-UCS2", "45bcf869acdcec2507f75836919e589e7137b9f9dbaa8e4975747035024248ef"},
}

// license is the repository's license file, copied beside the resources.
var license = resource{licenseName, "feb8b068a417681821823ef2e8995072aa111121fa11f8664038f78717f4497c"}

func main() {
	src := flag.String("src", "", "read the files from this directory instead of downloading them at the pinned commit")
	out := flag.String("out", "", "cmap package directory (default: pkg/internal/typography/cmap)")
	flag.Parse()

	pkgDir := *out
	if pkgDir == "" {
		pkgDir = findCMapDir()
	}
	dataDir := filepath.Join(pkgDir, outDirName)
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		fatalf("creating %s: %v", dataDir, err)
	}

	for _, r := range resources {
		data, err := fetch(*src, sourceDir+"/"+r.Name, r)
		if err != nil {
			fatalf("%v", err)
		}
		gz, err := compress(data)
		if err != nil {
			fatalf("compressing %s: %v", r.Name, err)
		}
		if err := os.WriteFile(filepath.Join(dataDir, r.Name+".gz"), gz, 0o600); err != nil {
			fatalf("writing %s: %v", r.Name, err)
		}
	}
	lic, err := fetch(*src, licenseName, license)
	if err != nil {
		fatalf("%v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, licenseName), lic, 0o600); err != nil {
		fatalf("writing %s: %v", licenseName, err)
	}
	code, err := generate()
	if err != nil {
		fatalf("%v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, outGoName), code, 0o600); err != nil {
		fatalf("writing %s: %v", outGoName, err)
	}
	fmt.Printf("Generated %s and %s/ from %s at %s\n", outGoName, outDirName, adobeRepo, adobeCommit)
}

// fetch returns the file at path of the pinned commit, read from dir when it
// is set and downloaded otherwise, after checking it against r's digest.
func fetch(dir, path string, r resource) ([]byte, error) {
	var data []byte
	if dir != "" {
		local := filepath.Join(dir, filepath.Base(path))
		b, err := os.ReadFile(local) //nolint:gosec // the directory is the developer's own command-line flag
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", local, err)
		}
		data = b
	} else {
		b, err := download(rawBaseURL + "/" + adobeCommit + "/" + path)
		if err != nil {
			return nil, err
		}
		data = b
	}
	if err := verify(data, r); err != nil {
		return nil, err
	}
	return data, nil
}

// download returns the body at url, refusing one past [maxResourceBytes].
func download(url string) ([]byte, error) {
	resp, err := httpClient.Get(url) //nolint:gosec // the URL is built from package constants
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: HTTP %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResourceBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", url, err)
	}
	if len(data) > maxResourceBytes {
		return nil, fmt.Errorf("downloading %s: %w", url, errTooLarge)
	}
	return data, nil
}

// verify reports whether data is the file r pins.
func verify(data []byte, r resource) error {
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != r.SHA256 {
		return fmt.Errorf("%s: %w: got %s, want %s", r.Name, errDigestMismatch, got, r.SHA256)
	}
	return nil
}

// compress gzips data with a zero header, so the output depends on the input
// and the compressor alone.
func compress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, fmt.Errorf("creating gzip writer: %w", err)
	}
	if _, err := zw.Write(data); err != nil {
		return nil, fmt.Errorf("compressing: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("closing gzip writer: %w", err)
	}
	return buf.Bytes(), nil
}

// generate renders the gofmt-formatted Go file recording where the resources
// come from and their digests.
func generate() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("// Code generated by cmd/gen-adobe-ucs2; DO NOT EDIT.\n\n")
	buf.WriteString(comment(fmt.Sprintf("The %s directory holds gzip copies of the %s resources of Adobe's "+
		"mapping-resources-pdf (%s), commit %s, unchanged. Copyright Adobe, licensed under the "+
		"BSD 3-Clause License: see %s/%s.", outDirName, sourceDir, adobeRepo, adobeCommit, outDirName, licenseName)))
	buf.WriteString("\npackage cmap\n\n")
	buf.WriteString(comment("adobeUCS2Digests maps each CMap the " + outDirName + " directory holds to the " +
		"SHA-256 of the file as Adobe publishes it, before compression."))
	buf.WriteString("var adobeUCS2Digests = map[string]string{\n")
	for _, r := range resources {
		fmt.Fprintf(&buf, "\t%q: %q,\n", r.Name, r.SHA256)
	}
	buf.WriteString("}\n")
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

func findCMapDir() string {
	for _, c := range []string{".", "pkg/internal/typography/cmap"} {
		if _, err := os.Stat(filepath.Join(c, "usecmap.go")); err == nil {
			return c
		}
	}
	fatalf("cannot find the cmap package directory; run from the repo root or pkg/internal/typography/cmap/")
	return ""
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "gen-adobe-ucs2: "+format+"\n", args...)
	os.Exit(1)
}
