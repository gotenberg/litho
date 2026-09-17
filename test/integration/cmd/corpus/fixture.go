package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/pdf"
	"github.com/gotenberg/litho/pkg/plugins/jbig2"
	"github.com/gotenberg/litho/pkg/plugins/jpx"
)

// Fixture is one PDF plus the identifier the goldens are keyed on.
type Fixture struct {
	// ID is the key used to look up goldens. Stable across runs.
	ID string

	// PDFPath is the absolute path to the fixture PDF on disk.
	PDFPath string

	// SkipReason is non-empty when the manifest marks the fixture as
	// skip-worthy (infinite loop, decompression bomb). The runner omits
	// skipped fixtures from every feature.
	SkipReason string
}

// LoadFixtures walks the manifest's flat pdfs/ tree and emits fixtures
// according to the manifest's profile mode. See [Manifest] for the
// per-mode contract. ID format also varies by mode: "expand" yields
// "<profile>/<basename>"; "none" yields "<basename>"; "auto" yields
// "<profile>/<basename>" when the PDF claims a profile via XMP and
// "<basename>" otherwise.
func LoadFixtures(m *Manifest) ([]Fixture, error) {
	pdfs, err := walkPDFs(m)
	if err != nil {
		return nil, err
	}

	switch m.EffectiveProfileMode() {
	case ProfileModeExpand:
		return loadFixturesExpand(m, pdfs)
	case ProfileModeNone:
		return loadFixturesNone(m, pdfs), nil
	case ProfileModeAuto:
		return loadFixturesAuto(m, pdfs)
	default:
		return nil, fmt.Errorf("unknown profile_mode %q", m.ProfileMode)
	}
}

// walkPDFs collects every *.pdf file under the manifest's pdfRoot.
func walkPDFs(m *Manifest) ([]string, error) {
	pdfRoot := m.TestdataRoot()
	if m.PDFRoot != "" {
		pdfRoot = filepath.Join(m.TestdataRoot(), m.PDFRoot)
	}
	var pdfs []string
	if err := filepath.Walk(pdfRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".pdf") {
			return nil
		}
		pdfs = append(pdfs, path)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("walk %s: %w", pdfRoot, err)
	}
	return pdfs, nil
}

func loadFixturesExpand(m *Manifest, pdfs []string) ([]Fixture, error) {
	if len(m.Profiles) == 0 {
		return nil, errors.New("manifest profiles list is empty (profile_mode=expand)")
	}
	fixtures := make([]Fixture, 0, len(pdfs)*len(m.Profiles))
	for _, path := range pdfs {
		base := strings.TrimSuffix(filepath.Base(path), ".pdf")
		for _, profile := range m.Profiles {
			id := profile + "/" + base
			fixtures = append(fixtures, Fixture{
				ID:         id,
				PDFPath:    path,
				SkipReason: m.Skips[id],
			})
		}
	}
	return fixtures, nil
}

func loadFixturesNone(m *Manifest, pdfs []string) []Fixture {
	fixtures := make([]Fixture, 0, len(pdfs))
	for _, path := range pdfs {
		base := strings.TrimSuffix(filepath.Base(path), ".pdf")
		fixtures = append(fixtures, Fixture{
			ID:         base,
			PDFPath:    path,
			SkipReason: m.Skips[base],
		})
	}
	return fixtures
}

func loadFixturesAuto(m *Manifest, pdfs []string) ([]Fixture, error) {
	allow := make(map[string]bool, len(m.Profiles))
	for _, p := range m.Profiles {
		allow[p] = true
	}

	// When provisioning produced a claims.csv next to the testdata
	// root, trust it and skip the live XMP scan. Govdocs has ~1500
	// PDFs and the live scan adds ~30s to every run; the CSV cuts
	// that to a single fopen.
	claims, _ := loadClaimsCSV(filepath.Join(m.TestdataRoot(), "claims.csv"))

	fixtures := make([]Fixture, len(pdfs))
	sem := make(chan struct{}, runtime.NumCPU())
	var wg sync.WaitGroup
	for i, path := range pdfs {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			base := strings.TrimSuffix(filepath.Base(path), ".pdf")
			var profile string
			if claims != nil {
				profile = claims[base]
			} else {
				profile = detectClaimedProfile(context.Background(), path)
			}
			if profile != "" && len(allow) > 0 && !allow[profile] {
				profile = ""
			}
			id := base
			if profile != "" {
				id = profile + "/" + base
			}
			fixtures[i] = Fixture{
				ID:         id,
				PDFPath:    path,
				SkipReason: m.Skips[id],
			}
		}()
	}
	wg.Wait()
	return fixtures, nil
}

// detectClaimedProfile opens pdfPath, reads its XMP packet, and returns
// the profile directory name the document claims via pdfaid:part /
// pdfaid:conformance / pdfuaid:part. Empty when the document has no
// claim or when reading fails (the runner treats no-claim PDFs as
// profile-agnostic, so reading errors degrade to "no claim" rather than
// aborting the whole load).
func detectClaimedProfile(ctx context.Context, pdfPath string) string {
	f, err := os.Open(pdfPath) //nolint:gosec // pdfPath comes from the corpus testdata walk
	if err != nil {
		return ""
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ""
	}
	r, err := pdf.Open(ctx, f, info.Size(),
		pdf.WithFilterPlugin(jbig2.New()),
		pdf.WithFilterPlugin(jpx.New()),
	)
	if err != nil {
		return ""
	}
	defer r.Close()
	open, err := r.XMPMetadata(ctx)
	if err != nil || open == nil {
		return ""
	}
	rc, err := open(ctx)
	if err != nil || rc == nil {
		return ""
	}
	defer rc.Close()
	const limit = 1 << 20 // 1 MiB cap; XMP packets larger than this are pathological
	data, err := io.ReadAll(io.LimitReader(rc, limit))
	if err != nil {
		return ""
	}
	return profileFromXMP(data)
}

var (
	xmpPDFAPart        = regexp.MustCompile(`pdfaid:part\s*(?:=\s*"([^"]+)"|>\s*([^<\s]+))`)
	xmpPDFAConformance = regexp.MustCompile(`pdfaid:conformance\s*(?:=\s*"([^"]+)"|>\s*([^<\s]+))`)
	xmpPDFARev         = regexp.MustCompile(`pdfaid:rev\s*(?:=\s*"([^"]+)"|>\s*([^<\s]+))`)
	xmpPDFUAPart       = regexp.MustCompile(`pdfuaid:part\s*(?:=\s*"([^"]+)"|>\s*([^<\s]+))`)
)

// profileFromXMP extracts the claimed profile directory from an XMP
// packet. Recognises pdfaid:part with optional pdfaid:conformance
// (PDF/A-1..3), pdfaid:part with pdfaid:rev (PDF/A-4*), and
// pdfuaid:part (PDF/UA). Returns "" when the packet has no claim.
func profileFromXMP(xmp []byte) string {
	if pua := matchOne(xmpPDFUAPart, xmp); pua != "" {
		return "PDF_UA-" + pua
	}
	part := matchOne(xmpPDFAPart, xmp)
	if part == "" {
		return ""
	}
	conf := strings.ToLower(matchOne(xmpPDFAConformance, xmp))
	rev := matchOne(xmpPDFARev, xmp)
	switch part {
	case "1", "2", "3":
		// PDF/A-1..3 require a conformance level.
		switch conf {
		case "a", "b", "u":
			return "PDF_A-" + part + conf
		default:
			return ""
		}
	case "4":
		// PDF/A-4 has flavours -4, -4e, -4f distinguished by pdfaid:rev
		// and presence of the "e"/"f" suffix on conformance. Accept any
		// of: no rev/conf -> "4"; conf=e -> "4e"; conf=f -> "4f".
		switch conf {
		case "e":
			return "PDF_A-4e"
		case "f":
			return "PDF_A-4f"
		}
		_ = rev
		return "PDF_A-4"
	}
	return ""
}

func matchOne(re *regexp.Regexp, src []byte) string {
	m := re.FindSubmatch(src)
	if m == nil {
		return ""
	}
	for _, g := range m[1:] {
		if len(g) > 0 {
			return string(g)
		}
	}
	return ""
}

// isProfileDir reports whether a directory name matches a VeraPDF
// profile convention (PDF_A-*, PDF_UA-* or WTPDF-*).
//
// The predicate gates two things: [remediationFeature.Prepare] groups
// fixtures by it, so a profile it does not recognise never reaches the
// remediators and every cell reports "no precomputed verdict"; and
// [baseFixtureID] strips the directory so profile-agnostic features key
// their goldens on the basename.
func isProfileDir(name string) bool {
	return strings.HasPrefix(name, "PDF_A-") ||
		strings.HasPrefix(name, "PDF_UA-") ||
		strings.HasPrefix(name, "WTPDF-")
}
