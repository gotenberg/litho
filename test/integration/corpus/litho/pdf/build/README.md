# litho/pdf fixture builders

Each fixture in `../pdfs/` has one Go builder here, `<fixture>.go` in package
`build`, that returns its exact bytes. `TestBuildersMatchCommitted` builds
every registered fixture and compares it with the committed PDF byte for
byte, so a builder and its PDF always change together.

The builders write through `test/integration/fixture/pdfgen`: a small PDF
object model and a writer that serializes exactly as qpdf 12.2 does for
pikepdf 9.11's save options (deterministic /ID, uncompressed streams). It
uses the standard library only and none of litho's `pkg/`, so no fixture is
produced by the code under test. `pdfgen/zdeflate` is a byte-exact port of C
zlib 1.2.12's compressor. The builders were ported from pikepdf scripts,
removed since; the Go builder is the fixture's source.

## Adding or changing a fixture

1. Write `<fixture>.go` (the fixture name in lower camel case for the
   function):

   ```go
   package build

   import "github.com/gotenberg/litho/test/integration/fixture/pdfgen"

   func init() { register("annots_1b", annots1b) }

   // annots1b builds annots_1b.pdf: what the fixture plants, the clauses it
   // reaches, and its controls. See ../FIXTURES.md for the authoritative
   // expected firing set.
   func annots1b() ([]byte, error) {
   	pdf := pdfgen.New()
   	...
   	return save(pdf, "1.4")
   }
   ```

   The doc comment carries the fixture's intent; keep it in step with
   `../FIXTURES.md`. Run `gofmt -w` after editing it: gofmt rewrites list
   bullets and indented blocks in doc comments.

2. Write the PDF and check it is stable:

   ```sh
   go test ./test/integration/corpus/litho/pdf/build/ -run 'TestBuildersMatchCommitted/annots_1b$'
   ```

   A new or changed fixture fails here first. `PDFGEN_DUMP=/some/dir` writes
   the built file there; inspect it, then copy it to `../pdfs/<fixture>.pdf`
   and record the expected firings in `../FIXTURES.md`. On an unintended
   mismatch the test prints the first differing offset with context.

3. `golangci-lint run ./test/integration/corpus/litho/pdf/... ./test/integration/fixture/...`
   (zero issues).

## pdfgen at a glance

The left column is the pikepdf idiom the builders were first written in.
pdfgen reproduces what pikepdf wrote, so a fixture doc that names a pikepdf
behaviour describes pdfgen's too.

| pikepdf                                                                           | Go                                                                                                              |
| --------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| `pikepdf.new()`                                                                   | `pdf := pdfgen.New()`                                                                                           |
| `Dictionary(Type=Name.Page, Count=1)`                                             | `d("Type", n("Page"), "Count", 1)`                                                                              |
| `Dictionary(**{key: v})`, `d[Name("/" + k)] = v`, `d.K = v`, `setattr(d, k, v)`   | `dd.Set(k, v)` (keys are plain strings, a leading `/` is dropped)                                               |
| `del d.K`, `Name.K in d`, `d.K`                                                   | `dd.Del("K")`, `dd.Has("K")`, `dd.Get("K")`                                                                     |
| `Array([0, 0, 612, 792])`, `.append(x)`, `.extend(xs)`                            | `a(0, 0, 612, 792)`, `arr.Append(x)`, `arr.Append(xs...)`                                                       |
| `Name.Font`, `Name("/Helvetica")`                                                 | `n("Font")`, `n("/Helvetica")`                                                                                  |
| `pikepdf.Object.parse(b"/text#2Fxml")` (a name token)                             | `nameToken("/text#2Fxml")` (decodes `#xx`; `n` keeps `#` literal like `Name()`)                                 |
| `String("text")` with a Python `str`                                              | `text("text")` (PDFDocEncoding, else UTF-16BE with BOM, as pikepdf)                                             |
| `String(b"\x00\x01")` with `bytes`                                                | `str("\x00\x01")` or `pdfgen.String(b)`                                                                         |
| Python `int`, `bool`, `None`                                                      | Go `int`, `bool`, `nil` (converted by `d`/`a`/`Set`)                                                            |
| Python `float` (`0.5`, `612.0`)                                                   | Go `float64` literal `0.5` / `pdfgen.Float(f)` (six decimals, trailing zeros trimmed: `612.0` → `612`)          |
| `Decimal("1.50")`                                                                 | `pdfgen.Real("1.50")` (the exact token; check what pikepdf wrote, see Gotchas)                                  |
| `pdf.make_indirect(dict)`                                                         | `pdf.Indirect(dd)` (in place; returns it)                                                                       |
| `pdf.make_indirect(stream_or_array_or_scalar)`                                    | `pdf.MakeIndirect(x)` (in place for arrays/streams; a scalar comes back as `*pdfgen.Ref`)                       |
| `Stream(pdf, data)` then `s.Type = ...`                                           | `pdf.Stream(data, "Type", n("XObject"), ...)` or `s.Set(...)` later                                             |
| `s.write(data, filter=Name.X, decode_parms=P)`                                    | `s.Data = data; s.Set("Filter", n("X")); s.Set("DecodeParms", P)` (no parms: leave the key out)                 |
| `s.read_bytes()` on a stream you built                                            | `s.Data` (only for unfiltered data)                                                                             |
| `pdf.pages.append(pikepdf.Page(page))`                                            | `pdf.AddPage(page)`                                                                                             |
| `pdf.pages[i]`, `page.obj`                                                        | `pdf.Page(i)` (the page dictionary)                                                                             |
| `pdf.add_blank_page(page_size=(w, h))`                                            | `pdf.AddBlankPage(w, h)` (pass floats if Python did: `612.0` and `612` write the same)                          |
| `pdf.Root.X = v`, `pdf.trailer.X = v`                                             | `pdf.Root.Set("X", v)`, `pdf.Trailer.Set("X", v)`                                                               |
| `pdf.docinfo` (getter, even just read)                                            | `docinfo(pdf)` (creates an indirect empty /Info when absent, like pikepdf)                                      |
| `pdf.docinfo = pdf.make_indirect(D)`                                              | `pdf.Trailer.Set("Info", pdf.Indirect(D))`                                                                      |
| `save(pdf, "x.pdf")` (`_common.save`, min_version 1.7)                            | `return save(pdf, "1.7")`                                                                                       |
| `save(pdf, "x.pdf", min_version="1.4")`                                           | `return save(pdf, "1.4")`                                                                                       |
| `preserve_pdfa=False`, `fix_metadata_version=False`                               | `save(pdf, "1.7", noPreservePDFA, noFixMetadataVersion)`                                                        |
| `object_stream_mode=ObjectStreamMode.generate`                                    | `generateObjectStreams` option                                                                                  |
| `pdf.save(path, ..., linearize=True)`                                             | `pdf.Save(pdfgen.SaveOptions{MinVersion: "1.7", Linearize: true})`                                              |
| `save(encryption=Encryption(owner=..., user=..., aes=True, R=4), static_id=True)` | `SaveOptions{CompressStreams: true, StaticID: true, Encryption: &pdfgen.Encryption{...}}` (see `trailer_2b.go`) |
| reopening the saved file to read object numbers / /Size                           | `out, err := saveOutput(...)`; `out.Num(obj)`, `out.Size`, `out.Bytes`                                          |
| `zlib.compress(x)`, `zlib.compress(x, 9)`                                         | `zlibCompress(x)`, `zdeflate.Compress(x, 9)`                                                                    |
| `re.search(rb"...", data, re.S)`                                                  | `regexp.MustCompile(`(?s)...`)` on `[]byte` (RE2: no backreferences or lookaround)                              |
| `data.replace(old, new)` checked once, same length                                | `patchOnce(data, old, new, why)`                                                                                |

Shared helpers in `common.go` (Python names on the left):

| Python                                                                               | Go                                                                       |
| ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------ |
| `lzw_encode(data, early_change=1)`                                                   | `lzwEncode(data, 1)`                                                     |
| `xmp_packet(part=2, conformance="B", rev=..., ua_part=..., ua_rev=..., extra="...")` | `xmpPacket(xmpOpts{Part: 2, Conformance: "B", ...})` (nil field = None)  |
| `attach_xmp(pdf, packet)`                                                            | `attachXMP(pdf, packet)`                                                 |
| `type3_font(pdf, [(g, adv, ops)...], widths, first=65)`                              | `type3Font(pdf, []type3Proc{{g, adv, ops}}, []any{...})` (codes from 65) |
| `tounicode_cmap(pdf, {code: cp})`                                                    | `toUnicodeCMap(pdf, map[int]int{...})`                                   |
| `unembedded_truetype(base="/Helvetica")`                                             | `unembeddedTrueType()` (always /Helvetica)                               |
| `rgb_image(pdf, interpolate=False)`                                                  | `rgbImage(pdf, false)`                                                   |
| `filespec(pdf, name, payload, relationship=None, subtype=None)`                      | `filespec(pdf, name, payload, "", "")` (empty string = None)             |

Font, ICC and JPEG 2000 programs come from `truetype.go`, `cff.go`,
`type1.go`, `icc.go` and `jp2.go`, held to their Python originals by
`TestHelpersMatchGolden` (`testdata/helpers_golden.txt`); the Type 0 encoding
CMap fixtures share `helpers.go`. Names take a module prefix:

| Python                                                                 | Go                                                                                                              |
| ---------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| `_truetype.build([(3, 1, {0x41: "A"})], glyph_names)`                  | `ttBuild([]ttSubtable{{3, 1, map[int]string{0x41: "A"}}}, nil)` (nil = default names)                           |
| `_truetype.tables_of`, `assemble`, `ADVANCE_WIDTH`                     | `ttTablesOf`, `ttAssemble`, `ttAdvanceWidth`                                                                    |
| `_cff.build()`, `build_cid(cids)`, `build_cff2()`                      | `cffBuild()`, `cffBuildCID(cids)`, `cffBuildCFF2()`                                                             |
| `_cff.build_charstrings(cs)` (default width)                           | `cffBuildCharstrings(cs, cffAdvanceWidth)` (defaults are explicit)                                              |
| `_cff.FONT_NAME` (bytes)                                               | `[]byte(cffFontName)`                                                                                           |
| `_type1.build(name=b"X", glyphs=..., encoding=..., charstrings=False)` | `t1Build(t1Options{Name: "X", Glyphs: ..., Encoding: ..., OmitCharStrings: true})` returns `(data, l1, l2, l3)` |
| `_type1.encoding_array(codes, fill_notdef=True)`                       | `t1EncodingArray(codes, true)`                                                                                  |
| `_icc.icc_profile(b"mntr", b"RGB ", b"XYZ ", tags)`                    | `iccProfile("mntr", "RGB ", "XYZ ", []iccTag{{"desc", ...}})`                                                   |
| `_icc.s15(*v)`, `xyz(t)`, `COMMON_TAGS`, `IDENTITY`                    | `iccS15(v...)`, `iccXYZ(t...)`, `iccCommonTags`, `iccIdentity`                                                  |
| `_jp2.container(cs, 3, 8, [colr...], depths=None)`                     | `jp2Container(jp2Three, 3, [][]byte{...}, nil)` (8-bit)                                                         |
| `_jp2.colr(4, 1)` (no enum)                                            | `jp2Colr(4, 1, 0)` (the enum is written only for method 1)                                                      |

## Gotchas

These are the qpdf and pikepdf behaviours pdfgen reproduces, and the Go
pitfalls that broke a byte during the port. Each one cost a mismatch once.

- **Never type U+FEFF literally in Go source.** Go rejects a BOM in the middle
  of a file. The xpacket header's BOM is `"\xef\xbb\xbf"`.
- **Object numbers come from a walk, not from creation order.** qpdf numbers
  objects breadth-first from the trailer (/Root first, then the other trailer
  keys in sorted order), each object's references in sorted-key order. So the
  order you `Set` keys never matters, but the **source** numbers do in two
  cases: object streams (members ordered by source number) and
  linearization. `pdf.Stream`, `pdf.Indirect` and `pdf.MakeIndirect`
  number objects in call order as qpdf does, and `MakeIndirect` on an
  already indirect object renumbers it: reorder those calls in such a
  fixture only on purpose.
- **The first page append pushes inherited attributes down.** On its first
  call, pikepdf's `pdf.pages.append` makes qpdf move /Resources, /MediaBox,
  /CropBox and /Rotate from /Pages down to the pages already in the tree
  (none yet), so attributes set on /Pages before the first append vanish;
  set after it, they stay. `pdf.AddPage` does the same.
- **A hand-built page tree skips `AddPage`.** A builder that writes /Kids,
  /Count and /Parent itself must not also call `AddPage`, which sets
  /Parent, rewrites /Count and pushes inherited attributes.
- **Creation order is argument order.** `d("N", appearance(), "D",
appearance())` creates N's stream first.
- **Never iterate a Go map to emit bytes** (content streams, arrays): use a
  slice in the intended order. Dictionaries written by pdfgen are sorted
  anyway.
- **Float arithmetic must round in doubles, step by step.** Go folds
  constant expressions exactly (`1.0 / 3 * 255` in a const context is not
  the double a runtime computation gives): divide typed `float64`
  variables. Go may also fuse
  `a*b+c` into one FMA rounding on arm64; wrap each product in `float64()`
  (`float64(a*b) + c`) to force the separate rounding. Python's `round` is
  `math.RoundToEven`, and `"%g" % x` is `pyG(x)` (`helpers.go`), not Go's
  `%g`.
- **Closures named `text`, `n`, `d` or `a` shadow the package helpers.** Name
  a local content builder `textOp` or similar.
- **The catalog's XMP is rewritten** on every save unless
  `noFixMetadataVersion` is passed (pikepdf's fix_metadata_version): pikepdf
  parses the packet with lxml and writes it
  back (`pdfgen.PikepdfXMP`): namespace declarations move before other
  attributes, `<a></a>` becomes `<a/>`, CDATA becomes escaped text, every
  processing instruction is stripped and a fresh xpacket wrapper added, and
  the /Metadata stream is replaced by a new one carrying only /Type and
  /Subtype (other keys set on it are lost). pdfgen errors out on a packet
  it cannot reproduce (DOCTYPE, malformed XML, `pdf:PDFVersion`): a fixture
  planting such a packet passes `noFixMetadataVersion`.
- **qpdf decodes the catalog's /Metadata stream** before writing it, even at
  decode level none, and drops its /Filter. Only page-level or other metadata
  keeps a filter. pdfgen decodes Flate and ASCIIHex here and errors on the
  other standard filters (port them in `pdfgen/decode.go` if needed).
- **A newline precedes every `endstream`** (pikepdf's default
  preserve_pdfa); `noPreservePDFA` removes it. Object streams follow the same rule; the
  cross-reference stream never gets it.
- **Null dictionary values are dropped**, null array elements kept. Empty
  containers are `[ ]` and `<< >>`.
- **Strings**: qpdf picks hex when a byte below 0x18 other than
  `\n \r \t \b \f` appears, or when more than a fifth of the bytes are 0x18
  to 0x1F or 0x7F to 0xFF; otherwise a literal with `\n \r \t \b \f ( ) \\`
  escaped and other non-printables as 3-digit octal. Write text strings with
  `text`, which picks PDFDocEncoding or UTF-16 as pikepdf does.
- **Names** escape `#`, delimiters and bytes outside 0x21 to 0x7E as
  lowercase `#xx`.
- **Floats** are qpdf's `double_to_string`: `%.6f`, trailing zeros and point
  trimmed, so `1e-7` writes `0` and `1e45` writes all 45 digits.
  `Decimal` goes through a double too when it has an exponent
  (`Decimal("1E45")`), but a plain `Decimal("1.50")` keeps its token: check the
  committed file and use `pdfgen.Real`.
- **/Size, /ID, /Prev, /Encrypt and cross-reference stream keys** in the
  trailer are replaced or dropped by the writer.
- **The deterministic /ID** is `MD5(hex(MD5(every byte written up to and
including " /ID [")) + " QPDF " + " " + each /Info string value in key
order)`, the seed cut at its first NUL byte. A post-save patch therefore
  never changes the /ID: patch the bytes `save` returns. A file saved with
  a /ID in `pdf.Trailer` (a reopened file) keeps its /ID[0].
- **/Extensions**: with extension level 0, qpdf removes a catalog
  /Extensions holding only /ADBE, and removes /ADBE beside other entries.
- **A signature dictionary's /Contents** (`/Type /Sig` with /ByteRange) is
  always written as a hex string.
- **zlib**: Go's `compress/zlib` does not produce C zlib's bytes. Use
  `zlibCompress` / `zdeflate.Compress`, which match C zlib 1.2.12 (and
  Python's `zlib.compress`) at every level.
- **Encryption** (R4, AESV2) reproduces qpdf's /O, /U, /P, file key and
  /Encrypt dictionary; only the AES IVs differ, derived instead of random
  (`pdfgen.Encryption`), so an encrypted fixture rebuilds byte for byte.
