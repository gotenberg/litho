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
//
// # Corpus evidence
//
//   - pdfjs issue10339_reduced, issue2761, govdocs thread_300_300253: their Lab
//     images (two indexed over Lab in each pdfjs file, one direct in govdocs)
//     mismatch on PDF.js's side. litho converts Lab with Bradford adaptation to
//     D65 and the sRGB curve (ISO 32000-1, 8.6.5.4) and matches the pinned
//     poppler within one level; PDF.js skips the adaptation and approximates the
//     curve with a square root, 7 to 10 levels away.
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
//
// Rows is set only on a golden whose PDF.js buffer held fewer than Height
// complete rows: the cells reaching past it carry no mean (see
// [cellMeans]).
type imageSignature struct {
	Width  int       `json:"width"`
	Height int       `json:"height"`
	Grid   [2]int    `json:"grid"`
	Sig    cellMeans `json:"sig"`
	Rows   int       `json:"rows,omitempty"`
	Name   string    `json:"name,omitempty"`
	Failed string    `json:"failed,omitempty"`
}

// cellMeans holds a signature's cell means. A JSON null, which gen-images.mjs
// writes for a cell PDF.js supplied no complete samples for, decodes to NaN
// instead of the 0 encoding/json would store, so the cell cannot pass for a
// black one. [imageDecodeFeature.LoadGoldens] keeps NaN only where the
// golden's Rows accounts for it, and [signatureDistance] skips it there.
type cellMeans []float64

// UnmarshalJSON decodes a null cell as NaN.
func (m *cellMeans) UnmarshalJSON(data []byte) error {
	var cells []*float64
	if err := json.Unmarshal(data, &cells); err != nil {
		return fmt.Errorf("decode cell means: %w", err)
	}
	if cells == nil {
		*m = nil
		return nil
	}
	out := make(cellMeans, len(cells))
	for i, c := range cells {
		if c == nil {
			out[i] = math.NaN()
			continue
		}
		out[i] = *c
	}
	*m = out
	return nil
}

// MarshalJSON encodes a NaN cell as null, which encoding/json refuses to
// encode as a number.
func (m cellMeans) MarshalJSON() ([]byte, error) {
	if m == nil {
		return []byte("null"), nil
	}
	cells := make([]*float64, len(m))
	for i := range m {
		if !math.IsNaN(m[i]) {
			cells[i] = &m[i]
		}
	}
	data, err := json.Marshal(cells)
	if err != nil {
		return nil, fmt.Errorf("encode cell means: %w", err)
	}
	return data, nil
}

// sanctionGaps keeps a golden image's NaN cells only where PDF.js did not
// supply them: Rows lies below Height and the cell's row of the grid reaches
// row Rows or past it, by the split gen-images.mjs makes. Any other NaN, and
// every NaN of an image with no supplied cell left, becomes +Inf, which no
// decoded image lies within tolerance of: an unexplained gap fails the image
// rather than vanish from the comparison.
func sanctionGaps(im *imageSignature) {
	gw, gh := im.Grid[0], im.Grid[1]
	short := im.Rows > 0 && im.Rows < im.Height && gw > 0 && gh > 0 && len(im.Sig) == gw*gh*3
	supplied := 0
	for i, v := range im.Sig {
		if !math.IsNaN(v) {
			supplied++
			continue
		}
		cy := i / 3 / max(gw, 1)
		// The last image row of grid row cy is ceil((cy+1)*Height/gh)-1.
		if short && ((cy+1)*im.Height+gh-1)/gh-1 >= im.Rows {
			continue
		}
		im.Sig[i] = math.Inf(1)
	}
	if supplied == 0 {
		for i := range im.Sig {
			im.Sig[i] = math.Inf(1)
		}
	}
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
		for p := range g.Pages {
			for i := range g.Pages[p].Images {
				sanctionGaps(&g.Pages[p].Images[i])
			}
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
	// The decoder returns Gray and RGBA images, whose stored bytes are what
	// RGBA()>>8 reports, and NRGBA ones (below), so their rows are summed
	// from Pix, one run of columns per cell: going through At boxed every
	// pixel in a color.Color and divided for its cell, which took 7 of the 18 seconds a file of 23
	// page-sized scans spent, past the per-file budget. The sums are of
	// integers, so they come out as the per-pixel loop's.
	//
	// Column x lies in cell floor(x*gw/w), so cell cx starts at
	// ceil(cx*w/gw).
	starts := make([]int, gw+1)
	for cx := range starts {
		starts[cx] = (cx*w + gw - 1) / gw
	}
	switch m := img.(type) {
	case *image.Gray:
		for y := range h {
			base := (y * gh / h) * gw
			row := m.Pix[m.PixOffset(b.Min.X, b.Min.Y+y):]
			for cx := range gw {
				var s uint64
				for _, v := range row[starts[cx]:starts[cx+1]] {
					s += uint64(v)
				}
				c := base + cx
				sums[3*c] += float64(s)
				sums[3*c+1] += float64(s)
				sums[3*c+2] += float64(s)
				counts[c] += float64(starts[cx+1] - starts[cx])
			}
		}
	case *image.RGBA, *image.NRGBA:
		// An NRGBA image is a JPEG 2000 image with the opacity of its data
		// (ISO 32000-1, Table 89 /SMaskInData) as alpha, its colour straight.
		// PDF.js hands such an image over as RGBA too, and the signature reads
		// its colour bytes as they are, alpha aside, so the Pix bytes are
		// summed here rather than At's premultiplied colour.
		var pix []uint8
		var off func(x, y int) int
		switch m := m.(type) {
		case *image.RGBA:
			pix, off = m.Pix, m.PixOffset
		case *image.NRGBA:
			pix, off = m.Pix, m.PixOffset
		}
		for y := range h {
			base := (y * gh / h) * gw
			row := pix[off(b.Min.X, b.Min.Y+y):]
			for cx := range gw {
				var r, g, bl uint64
				run := row[4*starts[cx] : 4*starts[cx+1]]
				for i := 0; i+3 < len(run); i += 4 {
					r += uint64(run[i])
					g += uint64(run[i+1])
					bl += uint64(run[i+2])
				}
				c := base + cx
				sums[3*c] += float64(r)
				sums[3*c+1] += float64(g)
				sums[3*c+2] += float64(bl)
				counts[c] += float64(starts[cx+1] - starts[cx])
			}
		}
	default:
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

// signatureDistance returns the largest difference between a decoded
// image's cell means, got, and a golden's, want. A NaN cell of want is one
// PDF.js did not supply (see [sanctionGaps]) and takes no part.
func signatureDistance(got, want []float64) float64 {
	if len(got) != len(want) {
		return math.Inf(1)
	}
	d := 0.0
	for i := range got {
		if math.IsNaN(want[i]) {
			continue
		}
		d = math.Max(d, math.Abs(got[i]-want[i]))
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
