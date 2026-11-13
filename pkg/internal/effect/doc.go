// Package effect draws the effects a vector page cannot express into
// 8-bit images and filters them: blurred shadows and glows, soft edges,
// reflections, filtered pictures, the filter graphs of SVG and CSS. Its
// results are the same bytes on every machine.
//
// # Surfaces
//
// A [Surface] holds W by H pixels in one of two [Format]s: [Coverage], a
// byte of alpha a pixel, for an effect drawn in one colour, and [RGBA],
// four bytes premultiplied by alpha. Its [Grid] places it on the page in
// points; the primitives work in pixels. [New] refuses a surface past
// [MaxPixels] and charges its bytes to the conversion's memory budget,
// [Surface.Release] gives them back; a primitive that needs work memory
// charges it for the length of its call. Pixels outside a surface read
// as transparent black unless a primitive says otherwise.
//
// # Primitives
//
//   - [Fill]: paths into coverage, filled by [NonZero] or [EvenOdd], or
//     stroked with round joins and caps.
//   - [Blur]: a separable convolution by integer [Kernel]s:
//     [GaussianKernel], [TentKernel] (a box twice), [BoxKernel] (one to
//     three boxes); [Edge] says what a blur reads past the edge.
//   - [Dilate], [Erode]: grow and shrink by a rectangle; [DilateDisc],
//     [ErodeDisc] by a disc.
//   - [Offset]: a move by a fraction of a pixel, resampled bilinearly.
//   - [Transform]: an affine map, resampled bilinearly.
//   - [ColorMatrix] with [SaturateMatrix], [HueRotateMatrix],
//     [LuminanceToAlphaMatrix], [GrayscaleMatrix], [SepiaMatrix].
//   - [Transfer]: a lookup table a channel, from [TransferFunc].
//   - [Flood], [Tint], [AlphaOf], [Threshold].
//   - [Composite] by a Porter-Duff [Operator], [Arithmetic], [Merge];
//     [Mask], a surface times a coverage.
//   - [Blend] by the sixteen [BlendMode]s.
//   - [Tile], [AlphaRamp].
//   - [Plan] and [Page]: the resolution an effect is drawn at.
//   - [Paint]: page content, text, pictures, shadings and groups, drawn
//     into RGBA surfaces by litho's PDF renderer, for the effects that
//     read the content itself rather than its silhouette.
//
// # What realises what
//
// SVG filter primitives (Filter Effects 1, 9):
//
//   - feGaussianBlur: [Blur] with [GaussianKernel] of stdDeviation in
//     pixels, edgeMode as [Edge].
//   - feOffset: [Offset]. feFlood: [Flood]. feTile: [Tile].
//   - feColorMatrix: [ColorMatrix], its four types by the constructors.
//   - feComponentTransfer: [Transfer] of [TransferFunc] tables.
//   - feComposite: [Composite], or [Arithmetic] for operator arithmetic.
//   - feBlend: [Blend]. feMerge: [Merge].
//   - feMorphology: [Dilate] or [Erode].
//   - feDropShadow: [AlphaOf], [Blur], [Offset], [Flood] and [Composite]
//     In for the shadow's colour, then [Merge] under the source.
//   - feImage: the content it names drawn by [Paint], then [Transform]
//     into place.
//   - SourceAlpha: [AlphaOf].
//
// CSS filter functions (Filter Effects 1, 6.1 and 13.1):
//
//   - blur(s): [Blur] with [GaussianKernel] of s; Chromium draws it as
//     [BoxKernel] of three passes, which the same call reproduces.
//   - drop-shadow(x y s c): as feDropShadow, the deviation s itself, as
//     feDropShadow's stdDeviation takes it and Chromium draws it.
//   - grayscale, sepia, saturate, hue-rotate: [ColorMatrix] with
//     [GrayscaleMatrix], [SepiaMatrix], [SaturateMatrix],
//     [HueRotateMatrix].
//   - brightness, contrast, invert, opacity: [Transfer] with
//     [BrightnessTransfer], [ContrastTransfer], [InvertTransfer],
//     [OpacityTransfer].
//   - box-shadow and text-shadow blurs (CSS Backgrounds 3, 7.1): [Fill],
//     [Blur] with [GaussianKernel] of half the blur radius, [Tint].
//
// Office effects (ECMA-376 Part 1, 20.1.8), with the falloffs Microsoft
// Office and LibreOffice print:
//
//   - outerShdw (20.1.8.45): [Fill] of the silhouette, [Transform] for sx,
//     sy, kx, ky about algn, [Blur] with [GaussianKernel] of a third of
//     blurRad, [Offset] by dist along dir, [Tint].
//   - innerShdw (20.1.8.40): the silhouette inverted ([Flood] full,
//     [Composite] Out the silhouette), [Offset], [Blur] at a third of
//     blurRad, [Composite] In the silhouette, [Tint].
//   - glow (20.1.8.32): [Fill], [Threshold] at 1, [Dilate] by half rad,
//     [Blur] at a sixth of rad, [Tint].
//   - softEdge (20.1.8.53): [Fill], [Threshold] at 1, [Erode] by rad,
//     [Blur] at a third of rad, [Composite] In over the shape's content.
//   - blur (20.1.8.15): the content, [Blur] at a third of rad.
//   - reflection (20.1.8.50): the content, [Transform] for the flip, sy and
//     dist, [Blur] at a third of blurRad, [AlphaRamp] from stA at stPos
//     to endA at endPos along fadeDir.
//   - fillOverlay (20.1.8.29): the content, [Flood], [Blend].
//   - effectDag (20.1.8.25): its alpha operators by [Transfer] on the
//     alpha, its colour operators by [ColorMatrix] or [Transfer], blend,
//     cont and tree by [Composite], [Blend] and [Merge].
//   - LibreOffice's shadow blur, glow and soft edge: the same chains with
//     [TentKernel] of the radius, a box run twice.
//
// Left out: feDiffuseLighting, feSpecularLighting and their lights need
// surface normals and a lighting model; feTurbulence needs the reference
// Perlin noise bit for bit; feConvolveMatrix and feDisplacementMap read
// arbitrary neighbours by document-chosen kernels and maps. No Office or
// CSS effect maps onto them; each can join as a primitive without
// changing the surfaces. DrawingML artistic effects, thirteen proprietary
// filters, are left out too; so are 3-D bevels, extrusions and cameras,
// which are not filters.
//
// # Resolution
//
// [Plan] draws an effect at the resolution where its blur's standard
// deviation spans 5 pixels, within [MinScale] (72 pixels an inch) and
// [MaxScale] (300 pixels an inch); content that is not blurred takes
// [MaxScale]. A surface past [MaxPixels] is drawn coarser and reported;
// a page's surfaces together stop at [MaxPagePixels].
//
// The rule comes from a measurement: rectangles of 30 by 20 points and
// squares of twice the deviation, blurred by deviations of 0.5 to 16
// points at a range of resolutions and four sub-pixel offsets, enlarged
// bilinearly and compared with their exact blur, the product of two
// differences of the normal distribution. The error depends on the
// pixels a deviation spans and nothing else; the largest difference in
// levels of 255 over shapes and offsets, then the largest root mean
// square:
//
//	pixels a deviation   1     1.5   2     2.5   3     3.5   4     5     6
//	largest error        24.1  14.3  7.5   5.3   3.5   2.9   2.1   1.1   1.2
//	root mean square     5.2   2.3   1.4   0.9   0.5   0.4   0.3   0.3   0.3
//
// From 5 pixels the error stops falling: what remains is the byte's
// rounding, the kernel cut at three deviations and the enlargement, 1.5
// levels at worst over further offsets. A deviation under 1.2 points
// meets [MaxScale] first and is drawn at fewer pixels.
//
// # Determinism
//
// No result depends on the machine. [Fill] maps each point to a grid of
// 1/65,536 pixel once, cuts curves by halving in integers and
// accumulates exact signed areas in integers, products of coordinates
// in 128 bits. Kernels, matrices, transfer tables, resampling and
// compositing work in integers or in fixed point. The few values that
// start in floating point (a kernel's weights, a transform's inverse, a
// stroke's normals, a matrix's coefficients) are computed with every
// product wrapped in an explicit conversion, which The Go Programming
// Language Specification says rounds it and so forbids fusing it with
// an addition, and without the math package's Exp, Log, Pow or Sin,
// which amd64 and arm64 implement differently; then they are rounded to
// fixed point before use. The tests pin the SHA-256 of every primitive's
// output on fixed inputs.
//
// [Paint] draws with litho's PDF renderer, which works in floating point
// and rounds every product on its own before it is summed (see package
// render), and rounds its result to 8 bits the same way. The renderer
// also calls the math package's Hypot, Sincos, Atan2 and Acos on stroke
// and curve geometry, and reads fonts and colour through packages not
// held to that rule: a surface [Paint] draws is the same on every machine
// as far as those agree. The tests pin the SHA-256 of its surfaces for
// text, pictures, shadings, shapes and transparency.
package effect
