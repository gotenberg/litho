# litho/image corpus fixtures

Image files of every format `pdf.FromImage` reads, and hostile ones, which `corpus build` writes from code (`imageFiles` in `cmd/corpus/corpus_images.go`, through the encoders of `internal/imagetest`) into the testdata, `sources/<name>/<name>.<extension>`, never committed: the same code writes the same bytes on every machine. No file comes from outside the repository and none shows a person: the pictures are gradients, shapes and text drawn by the generator.

No reference engine converts an image file to PDF as a viewer shows it, so the corpus has no `diff` cell. It runs `targets` (each source at every output flavour, the hostile files left out) and `robustness` (every file, each ending as `robustness.json` says). Its manifest names a goldens directory for both: the sources themselves.

The pictures are smooth gradients, a palette ramp whose neighbouring blocks are next in it, light colours on a clear screen: no two neighbouring pixels differ by much, so the renderer check of `targets`, which draws each file with litho and the pinned poppler at 72 dpi, judges the conversion and not how two renderers resample a hard edge. The bilevel pages are drawn at 72.01 pixels an inch: the pinned `pdftoppm` smooths the edges of a 1-bit image it draws at one pixel a device pixel exactly, where litho draws them sharp.

A source is read as an Office source is (`fixture.LoadOffice`): its format is told from its bytes by the conversion, and its extension only marks it as an image for the rig. `png-paper-a4-landscape` carries an `options.json` of the route's paper fields.

## Sources

| Source                   | Exercises                                                                                                                                         |
| ------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| `png-rgb`                | 8-bit RGB, a pHYs chunk of 150 pixels an inch                                                                                                     |
| `png-alpha`              | RGBA, an alpha falling from opaque to clear: a soft mask, flattened under PDF/A-1                                                                 |
| `png-16bit`              | 16-bit RGB: 16 bits a component from PDF 1.5, 8 under PDF 1.4                                                                                     |
| `png-gray`               | 8-bit gray                                                                                                                                        |
| `png-indexed`            | A palette of four colours: written indexed, at 2 bits                                                                                             |
| `png-indexed-alpha`      | A palette with a clear and a half-clear entry (tRNS): indexed with a soft mask                                                                    |
| `png-1bit`               | Black and white, a pHYs of 2,835 pixels a metre (72.01 an inch): written at 1 bit                                                                 |
| `png-icc`                | An iCCP chunk of a version 2 RGB profile: an ICCBased colour space                                                                                |
| `png-described`          | Title and Description text chunks, an eXIf orientation 6: the document's title, the figure's description, the picture turned                      |
| `png-paper-a4-landscape` | `paperWidth`, `paperHeight` of A4 and `landscape`: the picture fitted to the turned paper                                                         |
| `jpeg-baseline`          | A baseline YCbCr JPEG with a JFIF density of 72                                                                                                   |
| `jpeg-gray`              | A one-component JPEG                                                                                                                              |
| `jpeg-cmyk`              | An Adobe CMYK JPEG, inverted, with a CMYK profile in APP2                                                                                         |
| `jpeg-progressive`       | A progressive JPEG with an EXIF resolution of 300 and an EXIF title                                                                               |
| `jpeg-progressive-cmyk`  | A progressive Adobe CMYK JPEG, EXIF orientation 6                                                                                                 |
| `jpeg-progressive-gray`  | A progressive gray JPEG, EXIF orientation 3                                                                                                       |
| `gif-static`             | A GIF of 16 colours                                                                                                                               |
| `gif-animated`           | Two frames, the first 120 by 80 at 60, 40 on a 240 by 160 screen: the first frame composed on a clear screen                                      |
| `webp-lossless`          | A VP8L bitstream                                                                                                                                  |
| `webp-alpha`             | A VP8L bitstream with an alpha                                                                                                                    |
| `webp-lossy`             | A VP8 key frame                                                                                                                                   |
| `webp-animated`          | Two ANMF frames: the first one                                                                                                                    |
| `webp-icc-exif`          | A VP8X file with a version 4 ICCP profile and an EXIF chunk of orientation 8 and a title; the profile is left out under PDF 1.4                   |
| `bmp-8bit`               | A palette of 16 colours, 96 pixels an inch                                                                                                        |
| `bmp-24bit`              | 24-bit, 200 pixels an inch                                                                                                                        |
| `bmp-32bit-alpha`        | 32-bit under a BITMAPV4HEADER, its alpha a soft mask                                                                                              |
| `tiff-multipage`         | Four pages and a reduced-resolution image: uncompressed RGB, LZW in strips of 16 rows, PackBits gray, Deflate palette with orientation 8; a title |
| `tiff-bilevel`           | A 1-bit page, PackBits, 72.01 pixels an inch                                                                                                      |
| `tiff-16bit-icc`         | 16-bit RGB, Deflate, with a version 4 RGB profile                                                                                                 |
| `tiff-alpha`             | RGB with an unassociated alpha, LZW                                                                                                               |
| `jp2-rgb`                | A lossless JP2 file of three components, sRGB: written as it is from PDF 1.5, decoded under PDF 1.4                                               |
| `jp2-gray`               | A lossless JP2 file of one component, greyscale                                                                                                   |
| `j2k-codestream`         | A bare JPEG 2000 codestream: decoded and written losslessly                                                                                       |
| `ico-sizes`              | Four icons, 16 (PNG), 32 (32-bit DIB), 48 (24-bit DIB and its AND mask) and 256 (32-bit DIB): the 256 one                                         |
| `svg-text`               | Shapes and text in Liberation Sans and Serif, a title and a description: vector, its text in the fonts given                                      |
| `svg-viewbox`            | A view box alone, an XML declaration: a page of 300 by 300 CSS pixels                                                                             |

The sources that state no description fail the PDF/UA targets on their figure (ISO 14289-1, 7.3), as any undescribed picture does: `png-described` and `svg-text` pass them.

## Hostile files

| Files                                                                 | How each ends                                                                                              |
| --------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `bomb-png`, `-jpeg`, `-gif`, `-webp`, `-bmp`, `-tiff`, `-jp2`, `-ico` | `ErrConvertInputTooLarge`: a header promising far more pixels than the cap, refused before decoding        |
| `bomb-svg`                                                            | A PDF: a drawing 100,000,000 pixels a side allocates nothing by its size; its page is cut to the PDF limit |
| `deflate-bomb-tiff-strip`                                             | A PDF: a strip claiming 2^31 - 1 rows that inflates to 64 MiB is read as the page's 100 rows               |
| `deflate-bomb-png`                                                    | `ErrConvertMalformed`: the PNG decoder refuses the data past the image's rows                              |
| `truncated-png`, `-gif`, `-webp`, `-bmp`, `-tiff`, `-ico`, `-jp2`     | `ErrConvertMalformed`: each format's file cut in half                                                      |
| `truncated-jpeg`                                                      | A PDF: a JPEG is written as it is, its frame header intact                                                 |
| `tiff-loop`                                                           | A PDF of two pages: the chain's last IFD links back to its first                                           |
| `ico-outside`                                                         | `ErrConvertMalformed`: the icon's one image lies past the file's end                                       |
| `unsupported-heif`, `-avif`, `-jxl`, `not-an-image`                   | `ErrConvertMalformed`, the message naming the format the signature tells                                   |
