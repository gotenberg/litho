// Package officefx draws the effects of Office drawings and text, the
// outer shadow, the glow, the reflection and the soft edge, as Microsoft
// Office prints them: bitmaps drawn by the effect engine (package effect)
// under a shape, a picture or a run of glyphs that stays vector and text.
//
// # Rules
//
// The falloffs are Microsoft's, one rule for Word, PowerPoint and Excel
// (ECMA-376 Part 1, 20.1.8):
//
//   - outer shadow (20.1.8.45 outerShdw): the silhouette scaled by sx and
//     sy and skewed by kx and ky about the point algn names, a Gaussian
//     blur of a third of blurRad, moved dist along dir, in its colour;
//   - glow (20.1.8.32): the silhouette made opaque, grown by half rad, a
//     Gaussian blur of a sixth of rad, in its colour;
//   - reflection (20.1.8.50): the content scaled and skewed about algn
//     (sy -100% mirrors it), moved dist along dir, a Gaussian blur of a
//     third of blurRad, its opacity running from stA at stPos to endA at
//     endPos of its extent along fadeDir;
//   - soft edge (20.1.8.53): the content inside the silhouette made
//     opaque, shrunk by rad and blurred by a third of rad.
//
// Each bitmap is drawn at the resolution [effect.Plan] chooses for its
// blur, an RGB picture with a soft mask (ISO 32000-1, 11.6.5.3) in the
// page model, and an artifact (ISO 14289-1, 7.1): it decorates content
// that stays in the page as itself.
package officefx
