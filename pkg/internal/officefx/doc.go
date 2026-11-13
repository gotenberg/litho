// Package officefx draws the effects of Office drawings and text as
// Microsoft Office prints them: bitmaps drawn by the effect engine
// (package effect) under, over or in place of a shape, a picture or a run
// of glyphs that otherwise stays vector and text.
//
// # Rules
//
// The chains are Microsoft's, one for Word, PowerPoint and Excel
// (ECMA-376 Part 1, 20.1.8), with the measures they part on in [Rules]:
//
//   - outer shadow (20.1.8.45 outerShdw): the silhouette skewed by kx and
//     ky, then scaled by sx and sy, about the point algn names, a Gaussian
//     blur of a third of blurRad, moved dist along dir, in its colour;
//   - glow (20.1.8.32): the silhouette made opaque, grown by a square then
//     a disc, a Gaussian blur of a sixth of rad, in its colour
//     ([Rules.GlowMask]);
//   - reflection (20.1.8.50): the content skewed and scaled about algn
//     (sy -100% mirrors it), moved dist along dir, a Gaussian blur of a
//     third of blurRad, its opacity running from stA at stPos to endA at
//     endPos of its extent along fadeDir;
//   - soft edge (20.1.8.53): the content inside the silhouette made
//     opaque, shrunk by a disc and blurred by a third of rad
//     ([Rules.SoftFade]);
//   - inner shadow (20.1.8.40 innerShdw): the silhouette's complement
//     moved against dist along dir, a Gaussian blur of a third of
//     blurRad, kept inside the silhouette ([InnerMask]);
//   - blur (20.1.8.15) and fill overlay (20.1.8.29): the content drawn as
//     one bitmap, blurred by a third of rad, the overlay's colour blended
//     over it where it paints ([Content]);
//   - preset shadow (20.1.8.49 prstShdw): no bitmap, but one or two vector
//     copies of the content in one colour, scaled and skewed about a point
//     of the bounds by the preset's rule ([PresetCopies], [PresetUnder]).
//
// A run of text takes the outer shadow, the glow and the reflection.
// PowerPoint draws them from its glyphs as they paint, by its rules for a
// shape's; Word from their outlines, by its rules for a shape's at
// lengths scaled by the run's font size, the shadow cast by the glyphs
// over their glow and the reflection mirroring all three ([Source.Text]);
// [WordRunReach] says how far they push the run's line.
// An effect list names its program (css.OfficeEffects.Rules).
//
// Each bitmap is drawn at the resolution [effect.Plan] chooses for its
// blur ([PlanGrid]), an RGB picture with a soft mask (ISO 32000-1,
// 11.6.5.3) in the page model, with no structure parent: the caller
// leaves it an artifact (ISO 14289-1, 7.1), as Word and PowerPoint mark a
// run's, or hangs it from the element of what it decorates or replaces,
// as Word tags a shape's.
package officefx
