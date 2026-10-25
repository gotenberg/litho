# litho

A pure-Go PDF Conforming Processor. Read, validate, remediate, and produce PDF documents with no C dependencies.

litho implements ISO 32000 (PDF 1.4 through 2.0) in pure Go. It extracts text and structured content, validates documents against PDF/A, PDF/UA, WTPDF, Factur-X, and ZUGFeRD, repairs non-conformant files, and applies transformations such as rotate, split, merge, stamp, encrypt, and sign. Two rules govern the codebase: every output is a valid PDF that viewers can open, and every public type honors its documented concurrency contract under `-race`.

- Module: `github.com/gotenberg/litho`
- Go: 1.27.0+
- No CGO required.

## Features

### Read and extract

- Text, with spatial layout inference and an optional OCR hand-off.
- Positioned text elements, tables, and stroked lines.
- Images, decoded on request to sRGB pixels through their ICC profiles.
- Annotations, form fields, and field values.
- Bookmarks, embedded files, and digital signatures with LTV verification.
- PDF 2.0 data: geospatial tie points, measurements, 3D models, MathML formulas, and pronunciation lexicons.
- Document metadata from the Info dictionary and the raw XMP packet.

### Validate

- PDF/A: every part and level (1a/1b, 2a/2b/2u, 3a/3b/3u, 4/4e/4f).
- PDF/UA: UA-1 and UA-2.
- WTPDF 1.0 (Well-Tagged PDF): the Accessibility and Reuse levels (`WTPDF-1.0-Accessibility`, `WTPDF-1.0-Reuse`), judged by the PDF/UA-2 rules each level states and the PDF Declaration it requires. Reuse drops the rules that serve assistive technology, such as a Figure's alternate description.
- Factur-X 1.0+ and ZUGFeRD 1.0 / 2.0 / 2.1, including the embedded Cross-Industry Invoice spine, its XSD, and the EN 16931 and XRechnung Schematron rules.
- Each violation carries a structured clause, test number, and severity.

### Remediate

- Repair PDF/A, PDF/UA, and WTPDF violations behind a regression-bounded integrity probe that rolls back a content-modifying fix if it degrades the document. A WTPDF remediation writes the PDF Declaration its level requires.
- Two modes: content-preserving by default, or aggressive via `WithAggressiveFixes`.
- A PDF/A-1 remediation flattens the transparency ISO 19005-1 forbids, in both modes, as `flatten-transparency` does, so each page keeps its look with its text kept as text. A region of images and shadings only, or the tiles of a region that only images and shadings reach, is encoded as JPEG at quality 90 when that is smaller. What flattening leaves (annotation appearances, Type 3 glyphs, a page it refuses) is set opaque.
- Embed the fonts a document leaves unembedded from programs you supply (`WithFontProvider`, `WithFontFiles`, `--font-dir`) or from the operating system's fonts (`WithSystemFonts`, `--system-fonts`). litho ships no font program: it embeds one only when it holds every glyph the document shows at the widths the document declares, subsets it to those glyphs (`WithWholeFontPrograms`, `--no-font-subset` embed the whole program, for text you will edit later), and reports every font it refuses.
- Build Factur-X and ZUGFeRD hybrid invoices by embedding a Cross-Industry Invoice payload and remediating the PDF/A-3 base.
- A remediation is the last edit on a document: it judges the document the edits queued before it produce, so its report describes the output. A PDF/A output is never encrypted, since ISO 19005 forbids it.

### Transform and compose

- Rotate, delete, insert, split, and merge pages.
- Stamp text, images, and PDF overlays for watermarks.
- Encrypt (RC4, AES-128, AES-256), decrypt, and sign (PKCS#7/CMS or PAdES B-B to B-LTA, with RFC 3161 timestamps, incremental).
- Flatten forms, set metadata and form fields, and manage attachments and bookmarks.
- Convert every colour to CMYK, RGB, grey, an ICC profile or the document's output intent, keeping spot colours.
- Chain operations through a `Pipeline` with managed intermediate storage.

## Install

```bash
go get github.com/gotenberg/litho
```

Build the CLI:

```bash
go build -o litho ./cmd/litho
```

## Library

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

func main() {
	ctx := context.Background()

	r, err := pdf.OpenFile(ctx, "input.pdf")
	if err != nil {
		log.Fatal(err)
	}
	defer r.Close()

	text, err := r.ExtractText(ctx, 1)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(text)

	// Validate against PDF/A-2b. Each violation reports its clause, test, and severity.
	violations, err := r.ValidatePDFAReport(ctx, models.PDFA2b)
	if err != nil {
		log.Fatal(err)
	}
	for _, v := range violations {
		fmt.Printf("[%s] clause %s test %d: %s\n", v.Severity, v.Clause, v.TestNumber, v.Description)
	}

	// Remediate, then write the result.
	w := pdf.NewWriter(r)
	defer w.Close()
	report, err := w.RemediatePDFA(ctx, models.PDFA2b)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("fixed %d, residual %d\n", len(report.Fixed), len(report.Residual))

	out, err := os.Create("output.pdf")
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()
	if err := w.WriteTo(ctx, out); err != nil {
		log.Fatal(err)
	}
}
```

The public surface lives in three types: `Reader` opens and extracts, `Writer` queues mutations and emits, and `Pipeline` chains operations. See the [godoc](https://pkg.go.dev/github.com/gotenberg/litho/pkg/pdf) for the full API.

`Reader` is safe for concurrent use after construction. `Writer` is single-use and not safe for concurrent use. A `Writer` never modifies the `Reader` it is built on, so several may share one `Reader` while other goroutines read it. Close a `Writer` when done: a remediated one holds its output in a temp file until `WriteTo` streams it or `Close` removes it.

A remediation is the last edit on a `Writer`. Queue every other edit before it: afterwards the `Writer` accepts `Sign`, `Timestamp`, `WriteTo` and `Close`, and refuses anything else, a second remediation included, with `ErrEditAfterRemediation`.

### Embedding fonts

Every PDF/A, PDF/UA, and WTPDF profile requires the fonts a document shows to be embedded. litho ships no font program, so a remediation embeds only the programs you supply, and the licence to embed them is yours. Three options supply them:

- `pdf.WithFontFiles(fsys)` reads every `.ttf`, `.otf`, `.ttc`, `.otc` and `.cff` file under an `fs.FS`, each face of a TrueType or OpenType Collection standing as a program of its own, such as `os.DirFS("/usr/share/fonts")` or an `embed.FS`. A font takes the program whose PostScript name is its `/BaseFont`, else one of its family and style, else one of a metric-compatible family: Helvetica, Arial, Arimo, Liberation Sans and Nimbus Sans stand for one another, as do Times, Times New Roman, Tinos, Liberation Serif and Nimbus Roman, and Courier, Courier New, Cousine, Liberation Mono and Nimbus Mono PS. Symbol takes Standard Symbols PS, and ZapfDingbats takes D050000L.
- `pdf.WithSystemFonts()` is `pdf.WithFontFiles` over the operating system's font directories: `~/Library/Fonts`, `/Library/Fonts` and `/System/Library/Fonts` on macOS; the per-user fonts directory and `%WINDIR%\Fonts` on Windows; `$XDG_DATA_HOME/fonts`, `~/.fonts`, the `fonts` directory of each `$XDG_DATA_DIRS` entry, `/usr/local/share/fonts` and `/usr/share/fonts` elsewhere. litho reads the files itself, in pure Go, with no fontconfig. The directories are indexed once, at the first font a remediation asks for, from the header tables of each file only. `WithFontProvider` and `WithFontFiles` are asked first, and the system's fonts serve the fonts they supply no program for. The output then depends on the fonts of the host (see [Reproducible output](#reproducible-output)).
- `pdf.WithFontProvider(p)` asks a `pdf.FontProvider` for each unembedded font. The `pdf.FontRequest` names the font and lists every code the document shows with it, with its glyph name, character and declared width. Return a TrueType, OpenType or bare CFF program, or `nil` to supply none. A returned error aborts the remediation. `pdf.FontProviderFunc` adapts a function.

```go
report, err := w.RemediatePDFA(ctx, models.PDFA2b, pdf.WithFontFiles(os.DirFS("/usr/share/fonts")))
if err != nil {
	log.Fatal(err)
}
for _, refusal := range report.FontRefusals {
	fmt.Println(refusal) // font LithoCustom (object 7): no font program supplied
}
```

litho embeds a program only when it serves the font as the document shows it, so no text moves. Every font it leaves unembedded is listed in `RemediationReport.FontRefusals` and stays in the residual. Match the reason with `errors.Is`:

| Reason                       | The font stays unembedded because                                                                                                                                                  |
| ---------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `ErrFontNotSupplied`         | No program was supplied.                                                                                                                                                           |
| `ErrFontProgramInvalid`      | The program is no readable TrueType, OpenType or CFF font, or exceeds the stream budget.                                                                                           |
| `ErrFontEmbeddingRestricted` | The program's OS/2 fsType forbids embedding its outlines.                                                                                                                          |
| `ErrFontKind`                | The outlines do not suit the font: CFF for a TrueType font, a symbolic font whose codes name no glyphs, a multiple master font, or a composite font whose CIDs name no characters. |
| `ErrFontCoverage`            | The program lacks a glyph for a code the document shows.                                                                                                                           |
| `ErrFontWidths`              | The program's advances disagree with the declared widths by more than one unit, or the font declares none.                                                                         |
| `ErrFontRejected`            | The embedded font would fail a rule of the profile the unembedded one passed.                                                                                                      |
| `ErrFontUnwritable`          | The font dictionary is written inline where no object number reaches it.                                                                                                           |
| `ErrFontRolledBack`          | The output failed the integrity checks and the embedding was undone with the other content repairs.                                                                                |

By default litho embeds a subset of each program (`WithWholeFontPrograms`, or `--no-font-subset` on the CLI, embeds the whole program instead, for text you will edit later): the glyphs the document shows with the font and the glyphs they are drawn from, under a fresh subset tag. At PDF/A-1 a Type 1 subset carries the `/CharSet` and a CIDFont subset the `/CIDSet` ISO 19005-1, 6.3.5 requires. Fonts given the same program share one subset. A program stays whole when its OS/2 fsType forbids subsetting, when its font descriptor serves another font the document shows, when the interactive form's default resources name the font, or when validation rejects the subset and accepts the whole program. `RemediationReport.Fixed` states which each font received:

```text
embedded ArialMT (subset of 9 glyphs, TrueType program as /FontFile2, 14292 bytes) for font Helvetica (object 5)
```

`ExampleWithFontFiles` in the [godoc](https://pkg.go.dev/github.com/gotenberg/litho/pkg/pdf#example-WithFontFiles) runs the whole flow.

### Reproducible output

The same input bytes and options write the same output bytes. A remediation that must give a document its first trailer `/ID` derives it from the document's bytes and the remediation options, so two runs over one file agree. `pdf.WithDocumentID` pins the `/ID` to a 16-byte value instead:

```go
report, err := w.RemediatePDFA(ctx, models.PDFA2b, pdf.WithDocumentID([]byte("0123456789abcdef")))
```

A value of any other length is ignored. `pdf.WithSystemFonts` (`--system-fonts`) makes the output depend on the fonts installed on the host as well: another machine, or a font update on the same one, can embed another program or none. It is off by default, and the derived `/ID` records that it was used, so an output made with it never shares an `/ID` with one made without. Supply the programs with `pdf.WithFontFiles` over a directory you control for output that depends on the input alone. An encrypted output is the exception: its keys, salts, IVs and `/ID` are random, as the cryptography requires. So is what a network service answers: an RFC 3161 token (`--tsa-url`, `litho timestamp`) carries the authority's time and nonce, and the OCSP responses and CRLs `--ltv` embeds carry their responders' times.

## CLI

`litho` exposes the same capabilities as subcommands.

```bash
# Inspect a document.
litho info -f json document.pdf

# Extract text and image metadata.
litho text document.pdf
litho images -f json document.pdf

# Draw pages 1 to 3 at 150 dpi to page-1.png, page-2.png and page-3.png.
litho render --pages 1-3 --dpi 150 -o page document.pdf

# List the text a page draws and never shows, then the text short of WCAG 2.x
# contrast against what lies under it (advisory: PDF/UA sets no contrast rule).
litho text --unseen document.pdf
litho contrast document.pdf

# Validate compliance.
litho validate --profile PDF/A-2b document.pdf
litho validate --profile WTPDF-1.0-Accessibility document.pdf
litho validate --profile Factur-X invoice.pdf

# Remediate.
litho remediate --profile PDF/A-2b -o fixed.pdf document.pdf
litho remediate --profile WTPDF-1.0-Reuse -o fixed.pdf document.pdf

# Remediate, embedding the unembedded fonts from a font directory, with a pinned /ID.
litho remediate --profile PDF/A-2b --font-dir /usr/share/fonts \
  --document-id 00112233445566778899aabbccddeeff -o fixed.pdf document.pdf

# Remediate, embedding the unembedded fonts from the operating system's fonts.
litho remediate --profile PDF/A-2b --system-fonts -o fixed.pdf document.pdf

# Transform.
litho rotate --angle 90 -o rotated.pdf document.pdf
litho merge -o merged.pdf a.pdf b.pdf
litho stamp-text --text "DRAFT" -o stamped.pdf document.pdf

# Shrink a command's rewrite: --compact packs objects and merges duplicate
# streams, --recompress-images recompresses images losslessly and costs more time,
# --subset-fonts cuts the embedded fonts down to the glyphs the document shows.
litho merge --compact --recompress-images --subset-fonts -o merged.pdf a.pdf b.pdf

# Chain operations in one command. A remediate stage runs last.
litho pipe document.pdf rotate --angle 90 stamp-text --text "DRAFT" -o out.pdf
litho pipe document.pdf rotate --angle 90 remediate --profile PDF/UA-1 -o out.pdf

# Convert every colour to CMYK, the built-in SWOP profile becoming the output intent.
litho convert-colors --target cmyk -o cmyk.pdf document.pdf

# Composite live transparency into opaque images at 300 dpi, text kept as text,
# transparent annotations drawn into the page first.
litho flatten-transparency -o flat.pdf document.pdf

# Redact, encrypt and sign.
litho redact --region 1:72,700,200,24 -o redacted.pdf document.pdf
litho encrypt --user-password secret -o encrypted.pdf document.pdf
litho sign --cert cert.pem --key key.pem --reason Approved -o signed.pdf document.pdf
```

Run `litho` with no arguments to list every subcommand, or `litho <command> -h` to see its flags.

`--compact` is `pdf.WithObjectStreams` and `pdf.WithDedup`, and `--recompress-images` is `pdf.WithImageRecompression`. Every command that rewrites the whole file takes all three, `pipe` included, alone or together. `--recompress-images` re-encodes uncompressed, Flate, LZW and RunLength images as Flate with a PNG predictor where that is smaller, rewrites an 8-bit RGB image of at most 256 colours as an Indexed image where that is smaller, and re-codes sequential JPEG images with Huffman tables fitted to each image and without restart intervals, which keeps their coefficients, and every recompressed image decodes to the same samples as before. Progressive JPEG, JPEG 2000, JBIG2 and CCITT images are left as they are. It is a separate flag because it can take seconds on an image-heavy file that `--compact` alone writes in milliseconds. Neither applies to an encrypted output. `--subset-fonts` is `pdf.WithFontSubsetting`: it subsets the embedded TrueType, CFF and Type 1 fonts to the glyphs the document's pages, forms, patterns and annotation appearances can show, keeping every glyph ID, so every page renders and extracts as before and the compliance verdicts do not move. A font is left whole when litho cannot prove it saw every use of it, a form field types with it, or its program is OpenType. It is a separate flag because a subset font has no glyph for text added later, and it does apply to an encrypted output. For a PDF/A-1 target, `--compact` merges duplicate streams and does not pack objects, since ISO 19005-1 forbids the cross-reference stream packing needs.

`--font-dir` is `pdf.WithFontFiles` over a directory: `remediate` and the `pipe` remediate stage search it recursively and embed a subset of the program each unembedded font takes (see [Embedding fonts](#embedding-fonts)). `--system-fonts` is `pdf.WithSystemFonts`: the operating system's fonts serve the fonts `--font-dir` supplies no program for, or every font without `--font-dir`. `remediate` lists every font left unembedded, with the reason, under `Fonts left unembedded`, or `refused_fonts` with `-f json`. `pipe` prints no report: `pipe --strict` exits `2` when a font stays unembedded. `--document-id` is `pdf.WithDocumentID`: 32 hex digits pin the trailer `/ID` a remediation writes when the document has no valid one, so two runs over the same file write the same bytes. A directory that does not exist, or a value that is not 32 hex digits, exits `1` and writes nothing.

### Redaction

`litho redact` removes the text, images and paths each `--region page:x,y,width,height` covers, cuts every path reaching outside a region at its edge (the geometry inside is removed, the rest is painted under a clip that excludes the region), clears the copies of the removed text that annotations, form fields, outline titles and the metadata hold, then scans the output for every removed string. It refuses, and writes nothing, when content cannot be removed exactly or a copy survives in the redacted page's own resources and annotations, the structure tree, the metadata or any other object. The scan does not read the samples of images, inline ones included, embedded font programs, CMaps or ICC profiles: no reader extracts text from them, and their bytes spell a short word by chance. A copy that only pages outside the regions draw, in their content, forms or annotation appearances, is a warning: a region removes what it covers on its own page, as a Redact annotation applies to the page it sits on (ISO 32000-2, 12.5.6.23). The command prints one warning per copy on stderr, naming the pages and object, writes the output, and lists the copies under `copiesOnOtherPages` with `-f json`. So is a copy the redacted page itself still shows as visible text outside every region, a running head or a repeated word: the warning names the page and the boxes of the runs, and `-f json` lists it under `copiesOnRedactedPages`. A copy on the redacted page that no viewer shows (invisible, white, clipped, off the page, in a layer that is off) or that meets a region still refuses, and so does one a drawing of the redacted page does not show, painted over by a later fill, image or text. Removed digits the page holds only as numbers of operators that draw no text (a coordinate matching a date) warn the same way. `--strict` refuses all of these copies too. In Go, `Writer.Redact` reports them in `RedactReport.CopiesOnOtherPages` and `RedactReport.CopiesOnRedactedPages`, and `pdf.WithStrictRedaction` refuses them. It also renders each redacted page before and after at 72 dpi and refuses, writing nothing, unless the regions show only paper and blanked images and nothing outside them changed. `--no-pixel-check` (`pdf.WithoutRedactionPixelCheck`) skips that check; `--pixel-check` and `pdf.WithRedactionPixelCheck` are still accepted and change nothing. A blanked image is rewritten losslessly under Flate, so a JPEG or JPEG 2000 photograph grows; `--jpeg-quality Q` (`pdf.WithRedactionJPEGQuality`) redraws such a photograph as one lossless image with JPEG tiles over the parts no region reaches, which keeps every covered sample the blank exactly, and encodes the photographic parts of the `--raster-fallback` images the same way.

### Colour conversion

`litho convert-colors` converts every colour a document paints to one target, `Writer.ConvertColors` in Go: the colour operators of every page, form, pattern, Type 3 glyph and annotation appearance, their colour space resources, shadings, image XObjects and inline images. `--target` names it: `output-intent` (the default, the document's own output intent), `cmyk` (the built-in CGATS TR 001 profile, or `--cmyk-profile`), `rgb` (sRGB), `gray`, or `icc` with `--profile profile.icc`, which writes an ICCBased space. Colours convert through ICC profiles with litho's own colour engine, and a device colour reads as the renderer reads it: through the default colour space its resources give it, else the document's output intent of its component count, else the built-in profiles. The rendering intent is relative colorimetric unless `--intent` names another, and a rendering intent the content states wins for what it governs, as viewers read it: the `ri` operator and an ExtGState `/RI` for the colours, images and shadings painted after them, an image's `/Intent` for the image (ISO 32000-1, 8.6.5.8). Black points are compensated, as the renderer reads them, unless `--no-bpc` (`pdf.WithoutBlackPointCompensation`) turns it off (`--bpc` is still accepted and changes nothing). A graphics state's PDF 2.0 `/UseBlackPtComp` of `/ON` or `/OFF` wins for the colours, images and shadings painted under it, and `/Default` follows that choice (ISO 32000-2, Table 57). Under a CMYK target, `--k-only-black` (`pdf.WithKOnlyBlack`) writes DeviceGray colours and DeviceRGB black on the black ink alone (`k = 1 - gray`, ISO 32000-1, 10.3.3) instead of a four-colour black, so text and line art print on one plate; images, shadings and calibrated colours still convert through their profiles, and 100% K draws a dark grey on screen. Spot colours, Separation and DeviceN, keep their colorants, which the report lists, and convert only their alternate space; `--convert-spots` converts them to process colour. A target whose profile is an output or display profile becomes the document's PDF/A output intent unless `--no-output-intent` is given, and an intent of another subtype, a PDF/X one among them, is kept as it was. A CMYK target refuses a document that keeps such an intent over another CMYK profile (`pdf.ErrOutputIntentConflict`, naming its printing condition), since viewers would read the converted colours through it; `--replace-output-intents` (`pdf.WithReplacedOutputIntents`) points those intents at the target profile instead, keeping their subtype and printing condition. Images are re-encoded losslessly under Flate by default, so a JPEG or JPEG 2000 image grows; `--jpeg-quality 1-100` (`pdf.WithJPEGQuality`) re-encodes a converted JPEG or JPEG 2000 image as a baseline JPEG at that quality instead, with litho's own encoder, and keeps an image lossless where Flate holds it in fewer bytes. A JPEG 2000 image deeper than 8 bits a component stays lossless. A JPEG 2000 image that carries its soft mask in its data (`/SMaskInData`) keeps it as an explicit `/SMask` image. The conversion refuses, and writes nothing, when anything the document paints cannot be converted: an image whose samples do not decode, or a colour space litho cannot read. It also runs as a `litho pipe` stage, which takes the same flags but `-f` and discards the report.

### Transparency flattening

`litho flatten-transparency` removes a document's live transparency, `Writer.FlattenTransparency` in Go: constant alpha, blend modes, soft masks and transparency groups (ISO 32000-2, 11). Each region where a transparent object paints, with what it overlaps and must take along, is composited by litho's renderer into one opaque sRGB image at `--dpi` (300 by default, lowered to fit `--max-render-pixels`), drawn where the region's first object was. Everything outside those regions stays vector content. Text is never rasterised: the images are composited without it, and every text object stays in the content as it was, above the image, with its marked content, so the page stays searchable and its tags survive. Text that content drawn after it hid stays hidden: the image is drawn again over the pixels the source did not show. The report counts the text whose look this approximates: text a transparent object left visible now draws above it untinted, and text that was transparent itself draws opaque in its own colour. Annotations are left as they are and counted; run `flatten-form` first to bake form fields into the page. Each image is lossless under Flate by default; `--jpeg-quality Q` (`pdf.WithFlattenJPEGQuality`) encodes as JPEG at quality Q a region that paints only images and shadings and holds at least 256 colours, unless Flate is no larger, with chroma at half resolution below 90. A region that mixes photographs with vector art is cut into tiles that abut without seams: the tiles only images and shadings reach are JPEG and the others stay lossless, unless one lossless image is smaller. `--cmyk-profile profile.icc` (`pdf.WithDefaultCMYKProfile`) names the profile DeviceCMYK a document leaves uncharacterised is composited through, as for `render`. It also runs as a `litho pipe` stage, which takes the same flags but `-f` and discards the report; a stage's `--cmyk-profile` sets how every stage of the pipeline reads DeviceCMYK.

### Signing

`litho sign` signs with an X.509 certificate and its private key, RSA or ECDSA on P-256, P-384 or P-521. Pass them as two PEM files, `--cert` and `--key`, the key unencrypted, in PKCS #8, PKCS #1 or SEC 1 form, or as a PKCS #12 key store (`.p12`, `.pfx`) with `--p12`. The store's password comes from the first line of `--p12-password-file` or from the environment variable `--p12-password-env` names, never from the command line, and is empty without either. litho reads the stores current exporters write (PBES2 with AES) and the `-legacy` ones older tools write (triple DES and RC2). `--chain` adds intermediate certificates to `--cert`, `--reason`, `--location`, `--contact` and `--name` fill the signature dictionary, `--field` signs an existing empty signature field or names the new one, and `--signing-time` fixes the time for reproducible output. In `pipe`, a `sign` stage runs last, after any `remediate` stage. An encrypted document or PEM key is refused: decrypt it first.

The signature is a detached CMS SignedData (`adbe.pkcs7.detached`) over SHA-256, with the ESS signing-certificate-v2 attribute PAdES asks for, appended in an incremental update.

`--pades` writes a PAdES baseline signature instead (`ETSI.CAdES.detached`, ETSI EN 319 142-1, level B-B): no signing-time attribute, the `/M` entry carrying the time. `--tsa-url URL` timestamps the signature with an RFC 3161 timestamp authority (B-T), and `--ltv` adds the signer's and the authority's certificate chains, with an OCSP response or CRL for each certificate fetched from the addresses the certificates list, to the document security store in an update of its own (B-LT). `litho timestamp --tsa-url URL -o out.pdf in.pdf` adds an RFC 3161 document timestamp, and a `timestamp` stage after a `sign` stage in `pipe` makes the signature B-LTA. Each network exchange has a timeout and a size cap. A timestamp authority that gives no usable token, a certificate no OCSP response or CRL vouches for, and a revoked one fail the command, which then writes nothing. A field signed in place gets what its `/Lock` and `/SV` seed values ask for: the fields it locks turn read-only, and a seed value litho cannot meet is refused with the reason.

In Go, `pdf.NewSigner` builds the same signer from a certificate and any `crypto.Signer`, `pdf.ParseSignerPEM` from PEM data, and `pdf.ParseSignerPKCS12` from a PKCS #12 key store and its password. `pdf.WithPAdES`, `pdf.WithTimestampAuthority`, `pdf.WithLTV` and `Writer.Timestamp` do what the flags above do, and `pdf.WithValidationData` supplies the validation data so signing reaches no network. Implement `pdf.Signer` for anything else, such as a signature a remote service computes whole.

### Rendering

<<<<<<< HEAD
`litho render` draws pages with litho's own renderer, `Reader.RenderPage` in Go, which returns an `*image.NRGBA` at the resolution `pdf.WithRenderDPI` sets, over the colour `pdf.WithRenderBackground` sets, white by default. It draws paths, strokes, clipping, images with their masks, shadings, shading and tiling patterns, form XObjects, text in every rendering mode, Type 3 fonts included, and the appearance streams of annotations: those a viewer shows on screen, as pdftoppm draws them, or with `--print` (`pdf.WithRenderTarget(pdf.RenderForPrint)`) those it prints. Colours convert to sRGB as litho decodes images, the document's output intents honoured, and DeviceCMYK a document leaves uncharacterised reads through the profile `--cmyk-profile profile.icc` names (`pdf.WithDefaultCMYKProfile`), else through litho's built-in SWOP profile. litho bundles no font program: a font the document does not embed draws with a program `--font-dir DIR` or `--system-fonts` supplies (`pdf.WithRenderFontFiles`, `pdf.WithRenderSystemFonts`, `pdf.WithRenderFontProvider` in Go), matched by name, family or metric-compatible family, and otherwise as boxes as wide as the widths it declares. It draws transparency: constant alpha, the sixteen blend modes, soft masks, isolated and knockout transparency groups and text knockout, blending in sRGB. An annotation without an appearance stream draws as ISO 32000-1 defines it from its dictionary for the Square, Circle, Line, Polygon, PolyLine, Ink, text markup and Link types, as litho's own drawing of the predefined icon its `/Name` names for the Text type (each viewer draws its own), and not at all for the other icon and text types (FreeText, Stamp and the like). `pdf.WithRenderContent` draws only some kinds of content, text, paths, images, shadings or annotations, as Ghostscript's `-dFILTERTEXT`, `-dFILTERIMAGE` and `-dFILTERVECTOR` leave the others out. `--report` (`Reader.RenderPageReport` in Go, which returns a `pdf.RenderReport` with the image) prints a JSON report instead of the file names: for each page, its image and what the image approximates, the glyphs drawn as boxes, the annotations drawn from an appearance litho built or not drawn for want of one, the widgets drawn from appearances the form marks stale, the transparency drawn otherwise to fit the render budget, and the damaged constructs left out. `--max-render-pixels` bounds the pixels of one page.
=======
`litho render` draws pages with litho's own renderer, `Reader.RenderPage` in Go, which returns an `*image.NRGBA` at the resolution `pdf.WithRenderDPI` sets, over the colour `pdf.WithRenderBackground` sets, white by default. It draws paths, strokes, clipping, images with their masks, shadings, shading and tiling patterns, form XObjects, text in every rendering mode, Type 3 fonts included, and the appearance streams of annotations: those a viewer shows on screen, as pdftoppm draws them, or with `--print` (`pdf.WithRenderTarget(pdf.RenderForPrint)`) those it prints. Colours convert to sRGB as litho decodes images, the document's output intents honoured. litho bundles no font program: a font the document does not embed draws with a program `--font-dir DIR` or `--system-fonts` supplies (`pdf.WithRenderFontFiles`, `pdf.WithRenderSystemFonts`, `pdf.WithRenderFontProvider` in Go), matched by name, family or metric-compatible family, and otherwise as boxes as wide as the widths it declares. It draws transparency: constant alpha, the sixteen blend modes, soft masks, isolated and knockout transparency groups and text knockout, blending in sRGB. An annotation without an appearance stream draws as ISO 32000-1 defines it from its dictionary for the Square, Circle, Line, Polygon, PolyLine, Ink, text markup and Link types, as litho's own drawing of the predefined icon its `/Name` names for the Text type (each viewer draws its own), and not at all for the other icon and text types (FreeText, Stamp and the like). `pdf.WithRenderContent` draws only some kinds of content, text, paths, images, shadings or annotations, as Ghostscript's `-dFILTERTEXT`, `-dFILTERIMAGE` and `-dFILTERVECTOR` leave the others out. `--max-render-pixels` bounds the pixels of one page. `--box` (`pdf.WithRenderBox`) draws another page boundary than the crop box: `media`, `bleed`, `trim` or `art`, each read as a viewer reads it, so content outside the crop box shows where a wider box reaches it. `--color-model cmyk` (`pdf.WithRenderColorModel(pdf.RenderCMYK)`, an `*image.CMYK` in Go) draws for a CMYK device and writes each page as a separated TIFF, `PREFIX-N.tif`, of inks over paper: transparency blends in the device's CMYK, DeviceCMYK and DeviceGray colours reach it unchanged, DeviceGray as black ink alone, as do spot colours whose alternate is DeviceCMYK, and every other colour converts through its sRGB into the device's profile. The device is the document's CMYK output intent, else the built-in SWOP profile, unless `--output-profile profile.icc` (`pdf.WithRenderCMYKProfile`) names another.

> > > > > > > 2beeca8a (feat(render): draw a selected page boundary and render for a CMYK device)

### Resource limits

Every document is read under six budgets that keep a crafted file from asking for unbounded memory or work. Global flags, placed before the command, change them for every document the command reads, each input of a multi-input command and each `pipe` stage included:

| Flag                        | Bounds                                              | Default     |
| --------------------------- | --------------------------------------------------- | ----------- |
| `--max-stream-bytes`        | Decoded bytes of one stream                         | `256MiB`    |
| `--max-image-pixels`        | Pixels of one image                                 | `100000000` |
| `--max-embedded-file-bytes` | Size of an embedded PDF that PDF/A validation opens | `50MiB`     |
| `--max-form-depth`          | Form XObjects nested below a page                   | `10`        |
| `--max-cached-objects`      | Parsed objects kept in memory                       | `50000`     |
| `--max-render-pixels`       | Pixels of one page `litho render` draws             | `50000000`  |

A byte count takes a plain integer or a `KiB`, `MiB` or `GiB` suffix, the others a plain integer, and `0` keeps the default. A value that does not parse, or one past its ceiling, exits `1` before any document opens.

`--max-stream-bytes` also bounds the content of one page, summed across its content streams, at a quarter of its value: raising it is how to read a page heavier than `64MiB`.

```bash
litho --max-stream-bytes 64MiB --max-image-pixels 25000000 images document.pdf
```

The library takes the same budgets through `pdf.WithLimits`; `pdf.Limits` documents what each one bounds and the error a document past it returns.

### Encrypted documents

Editing an encrypted document produces an unprotected one. litho opens the file with the password supplied, writes out a decrypted object graph, and emits no `/Encrypt` dictionary unless one is requested. No command preserves the source's encryption. Every command that writes a document warns on stderr when the input carried protection the output will not.

`decrypt` is the deliberate exception and stays silent, because producing an unprotected document is what it was asked to do.

Re-protect the result in one of three ways. `split` and `merge` accept `--user-password` directly. `pipe` accepts an `encrypt` stage, which applies to the document it writes. Any other command can be followed by `litho encrypt`. A PDF/A output is the exception: ISO 19005 forbids encryption, so `pipe` refuses an `encrypt` stage beside a PDF/A `remediate` stage, and encrypting the result costs its conformance.

### Exit codes

| Code  | Meaning                                                                      |
| ----- | ---------------------------------------------------------------------------- |
| `0`   | Success.                                                                     |
| `1`   | The command failed: a missing file, an invalid flag, an unreadable document. |
| `2`   | The document did not conform.                                                |
| `124` | The `--timeout` deadline expired, the status `timeout(1)` reports.           |

`2` is deliberately distinct from `1`, so a caller can tell "litho could not run" from "the document is not conformant". `validate` reports it for a file with violations, and `remediate --strict` and `pipe --strict` for one they could not fully repair; those two still write their output, since the status is a verdict rather than a refusal to produce a document.

A command that reached a verdict before its deadline expired keeps that verdict: only a failure becomes `124`.

## Filter plugins

litho decodes the standard PDF stream filters in pure Go, JBIG2Decode and JPXDecode included. A stream behind a filter litho cannot decode, such as an unknown one, is reported through `Reader.UndecodableStreams`, or `litho info --streams`. A damaged stream, one cut short or with a wrong or missing checksum, decodes as a viewer renders it; `Reader.SalvagedStreams` and `litho info --streams` list it, and `pdf.WithStrictStreams` refuses it with `pdf.ErrDamagedStream`. To decode such a filter, or to replace a built-in decoder, implement `plugins.FilterDecoder` and register it when opening a document; it takes precedence over the built-in decoder for its filter name:

```go
r, err := pdf.OpenFile(ctx, "doc.pdf", pdf.WithFilterPlugin(myDecoder))
```

## Development

```bash
make fmt && make lint # zero-warnings policy, pinned golangci-lint
make test-unit        # go test -race ./...
make bench            # go test -bench=. -benchmem ./...
make corpus           # score against the veraPDF, PDF.js, and pdfcpu corpora
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the coding, safety, and documentation rules.

## Acknowledgments

litho's text extraction, CMap handling, and font resolution draw on Mozilla's [PDF.js](https://github.com/mozilla/pdf.js). Its PDF/A, PDF/UA, and WTPDF rule set and clause numbering follow [veraPDF](https://github.com/veraPDF). Grateful acknowledgment to both communities.
