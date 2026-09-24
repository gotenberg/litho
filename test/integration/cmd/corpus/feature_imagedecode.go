package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/gotenberg/litho/pkg/pdf"
)

// imageDecodeFeature decodes each image [pdf.Reader.ExtractImages] lists on a
// page through its Decode function and matches them against the images PDF.js
// paints on that page, compared by size and by a coarse signature of their
// pixels.
//
// It is the gate's reach into image decoding: no other feature decodes image
// samples, so a decoder could change its output while every other cell stayed
// identical.
type imageDecodeFeature struct {
	mu      sync.RWMutex
	goldens map[string]*imageGolden
	cache   sync.Map // pdfPath -> *cachedRun
}

func newImageDecodeFeature() *imageDecodeFeature {
	return &imageDecodeFeature{goldens: make(map[string]*imageGolden)}
}

// imageGolden is the per-fixture golden gen-images.mjs writes, and also the
// shape of the feature's own output.
type imageGolden struct {
	ID    string           `json:"id"`
	Pages []imagePageGroup `json:"pages"`
}

// imagePageGroup holds the images of one page.
type imagePageGroup struct {
	Page   int              `json:"page"`
	Images []imageSignature `json:"images"`
}

// imageSignature describes one decoded image: its size and the mean red,
// green and blue of each cell of a grid of at most maxSignatureCells by
// maxSignatureCells, row by row. Failed marks an image litho could not
// decode, which carries no signature.
type imageSignature struct {
	Width  int       `json:"width"`
	Height int       `json:"height"`
	Grid   [2]int    `json:"grid"`
	Sig    []float64 `json:"sig"`
	Name   string    `json:"name,omitempty"`
	Failed string    `json:"failed,omitempty"`
}

// maxSignatureCells is the grid size along each axis, matching gen-images.mjs.
const maxSignatureCells = 4

// imageTolerance is the largest difference, in 8-bit levels, a cell mean may
// show between litho and PDF.js for the images to match. Decoders round
// differently (the reconstruction of lossy JPEG 2000 and the inverse DCT of
// JPEG are the decoder's choice), which moves a cell mean by a level or two;
// a wrong filter, colour space, orientation or sample layout moves it by tens.
const imageTolerance = 3.0

func (f *imageDecodeFeature) Name() string         { return "imagedecode" }
func (f *imageDecodeFeature) GoldenSource() string { return "pdfjs" }

func (f *imageDecodeFeature) LoadGoldens(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ErrNoGoldens
	}
	loaded := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var g imageGolden
		if err := json.Unmarshal(data, &g); err != nil {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		f.mu.Lock()
		f.goldens[id] = &g
		f.mu.Unlock()
		loaded++
	}
	if loaded == 0 {
		return ErrNoGoldens
	}
	return nil
}

func (f *imageDecodeFeature) HasGolden(fixtureID string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, ok := f.goldens[baseFixtureID(fixtureID)]
	return ok
}

func (f *imageDecodeFeature) golden(fixtureID string) *imageGolden {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.goldens[baseFixtureID(fixtureID)]
}

// Run decodes the images of the pages the golden lists images on. Pages
// PDF.js paints no image on are not decoded: they have nothing to compare.
func (f *imageDecodeFeature) Run(ctx context.Context, pdfPath, fixtureID string) ([]byte, error) {
	if v, ok := f.cache.Load(pdfPath); ok {
		cr := v.(*cachedRun)
		return cr.output, cr.err
	}
	out, err := f.decodePages(ctx, pdfPath, f.golden(fixtureID))
	f.cache.Store(pdfPath, &cachedRun{output: out, err: err})
	return out, err
}

func (f *imageDecodeFeature) decodePages(ctx context.Context, pdfPath string, golden *imageGolden) ([]byte, error) {
	var result imageGolden
	if golden == nil || len(golden.Pages) == 0 {
		return json.Marshal(result)
	}
	file, err := os.Open(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat: %w", err)
	}
	r, err := pdf.Open(ctx, file, info.Size())
	if err != nil {
		return nil, fmt.Errorf("pdf.Open: %w", err)
	}
	defer r.Close()
	for _, gp := range golden.Pages {
		metas, err := r.ExtractImages(ctx, gp.Page)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			result.Pages = append(result.Pages, imagePageGroup{Page: gp.Page})
			continue
		}
		group := imagePageGroup{Page: gp.Page}
		for _, m := range metas {
			img, err := m.Decode(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				group.Images = append(group.Images, imageSignature{Width: m.Width, Height: m.Height, Name: m.Filename, Failed: err.Error()})
				continue
			}
			sig := signatureOf(img)
			sig.Name = m.Filename
			group.Images = append(group.Images, sig)
		}
		result.Pages = append(result.Pages, group)
	}
	return json.Marshal(result)
}

// signatureOf computes the signature gen-images.mjs computes for PDF.js's
// decoded image data.
func signatureOf(img image.Image) imageSignature {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	gw, gh := min(maxSignatureCells, w), min(maxSignatureCells, h)
	sums := make([]float64, gw*gh*3)
	counts := make([]float64, gw*gh)
	for y := range h {
		cy := y * gh / h
		for x := range w {
			cx := x * gw / w
			r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			c := cy*gw + cx
			sums[3*c] += float64(r >> 8)
			sums[3*c+1] += float64(g >> 8)
			sums[3*c+2] += float64(bl >> 8)
			counts[c]++
		}
	}
	sig := make([]float64, len(sums))
	for i := range sig {
		sig[i] = math.Round(sums[i]/counts[i/3]*10) / 10
	}
	return imageSignature{Width: w, Height: h, Grid: [2]int{gw, gh}, Sig: sig}
}

// Compare pairs each golden image with a distinct image litho decoded on the
// same page, of the same size, whose signature lies within imageTolerance.
func (f *imageDecodeFeature) Compare(fixtureID string, output []byte) Verdict {
	golden := f.golden(fixtureID)
	if golden == nil {
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: "no golden"}
	}
	total := 0
	for _, gp := range golden.Pages {
		total += len(gp.Images)
	}
	if total == 0 {
		return Verdict{FixtureID: fixtureID, Skipped: true, Reason: "PDF.js paints no image XObject"}
	}
	var got imageGolden
	if err := json.Unmarshal(output, &got); err != nil {
		return Verdict{FixtureID: fixtureID, Reason: fmt.Sprintf("decode output: %v", err)}
	}
	byPage := make(map[int][]imageSignature, len(got.Pages))
	for _, gp := range got.Pages {
		byPage[gp.Page] = gp.Images
	}
	matched, failed := 0, 0
	var mismatches []json.RawMessage
	for _, gp := range golden.Pages {
		litho := byPage[gp.Page]
		for _, im := range litho {
			if im.Failed != "" {
				failed++
			}
		}
		for i, j := range pairImages(gp.Images, litho) {
			if j >= 0 {
				matched++
				continue
			}
			want := gp.Images[i]
			best, bestDiff := nearestImage(want, litho)
			mismatches = append(mismatches, imageMismatch(gp.Page, want, litho, best, bestDiff))
		}
	}
	v := Verdict{
		FixtureID:  fixtureID,
		Match:      matched == total,
		Score:      float64(matched) / float64(total),
		Metrics:    map[string]int{"images_match": matched, "decode_failed": failed},
		Mismatches: mismatches,
	}
	if !v.Match {
		v.Reason = fmt.Sprintf("%d of %d images match", matched, total)
	}
	return v
}

// pairImages pairs the golden images of a page with distinct decoded images
// of the same size whose signatures lie within imageTolerance, as many as can
// be, and returns, for each golden image, the index of its partner or -1.
// Identical signatures pair first and each remaining golden image takes its
// nearest free partner; augmenting paths then re-pair wherever that leaves a
// golden image out that a different pairing admits. Taking each golden image's
// nearest partner in turn instead lets an early image take the only partner of
// a later one.
//
// # Corpus evidence
//
//   - govdocs thread_200_200823: pairing each golden image with its nearest
//     partner in turn left 34 of its tiny strips unmatched against images
//     litho decodes within tolerance.
func pairImages(want, got []imageSignature) []int {
	pair := make([]int, len(want))
	owner := make([]int, len(got))
	for i := range pair {
		pair[i] = -1
	}
	for j := range owner {
		owner[j] = -1
	}
	link := func(i, j int) {
		pair[i], owner[j] = j, i
	}
	key := func(s imageSignature) string {
		return fmt.Sprint(s.Width, s.Height, s.Grid, s.Sig)
	}
	exact := make(map[string][]int)
	for j, im := range got {
		if im.Failed == "" {
			exact[key(im)] = append(exact[key(im)], j)
		}
	}
	for i, w := range want {
		if js := exact[key(w)]; len(js) > 0 {
			link(i, js[0])
			exact[key(w)] = js[1:]
		}
	}
	// neighbours lists the decoded images within tolerance of golden image i,
	// nearest first, computed once per image and only for the images the
	// search reaches: a page can hold thousands of identical strips, which the
	// exact pass pairs without ever listing them.
	cache := make(map[int][]int)
	neighbours := func(i int) []int {
		if js, ok := cache[i]; ok {
			return js
		}
		type near struct {
			d float64
			j int
		}
		var ns []near
		w := want[i]
		for j, im := range got {
			if im.Failed != "" || im.Width != w.Width || im.Height != w.Height || im.Grid != w.Grid {
				continue
			}
			if d := signatureDistance(im.Sig, w.Sig); d <= imageTolerance {
				ns = append(ns, near{d, j})
			}
		}
		sort.SliceStable(ns, func(a, b int) bool { return ns[a].d < ns[b].d })
		js := make([]int, len(ns))
		for k, n := range ns {
			js[k] = n.j
		}
		cache[i] = js
		return js
	}
	for i := range want {
		if pair[i] >= 0 {
			continue
		}
		for _, j := range neighbours(i) {
			if owner[j] < 0 {
				link(i, j)
				break
			}
		}
	}
	var visited []bool
	var augment func(i int) bool
	augment = func(i int) bool {
		for _, j := range neighbours(i) {
			if visited[j] {
				continue
			}
			visited[j] = true
			if owner[j] < 0 || augment(owner[j]) {
				link(i, j)
				return true
			}
		}
		return false
	}
	for i := range want {
		if pair[i] < 0 {
			visited = make([]bool, len(got))
			augment(i)
		}
	}
	return pair
}

// nearestImage returns the decoded image of want's size whose signature lies
// nearest, paired or not, and its distance, or -1 when none decoded.
func nearestImage(want imageSignature, got []imageSignature) (int, float64) {
	best, bestDiff := -1, math.Inf(1)
	for j, im := range got {
		if im.Failed != "" || im.Width != want.Width || im.Height != want.Height || im.Grid != want.Grid {
			continue
		}
		if d := signatureDistance(im.Sig, want.Sig); d < bestDiff {
			best, bestDiff = j, d
		}
	}
	return best, bestDiff
}

// signatureDistance returns the largest difference between two signatures'
// cell means.
func signatureDistance(a, b []float64) float64 {
	if len(a) != len(b) {
		return math.Inf(1)
	}
	d := 0.0
	for i := range a {
		d = math.Max(d, math.Abs(a[i]-b[i]))
	}
	return d
}

// imageMismatch describes a golden image nothing matched: the nearest image
// litho decoded at its size, and an image of its size litho failed to decode,
// which is the likelier partner when there is one.
func imageMismatch(page int, want imageSignature, litho []imageSignature, best int, bestDiff float64) json.RawMessage {
	entry := map[string]any{"page": page, "width": want.Width, "height": want.Height}
	if best >= 0 {
		entry["closest"] = litho[best].Name
		entry["distance"] = math.Round(bestDiff*10) / 10
	}
	for _, im := range litho {
		if im.Failed != "" && im.Width == want.Width && im.Height == want.Height {
			entry["failed_image"] = im.Name
			entry["failed"] = im.Failed
			break
		}
	}
	if _, ok := entry["closest"]; !ok {
		if _, failed := entry["failed"]; !failed {
			entry["closest"] = "no decoded image of that size"
		}
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return json.RawMessage(`{"error":"marshal"}`)
	}
	return data
}
