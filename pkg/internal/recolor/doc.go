// Package recolor converts the colour of a document to one target colour
// space: the colour operators of page, form, pattern, annotation appearance
// and Type 3 glyph content, the colour spaces resources define, the shadings
// they paint, and the samples of images (ISO 32000-1, 8.6, 8.7 and 8.9).
//
// Process colour (the device, CIE-based and ICC-based families) converts to
// the [Target]. Spot colour (Separation and DeviceN) keeps its colorants by
// default, and only its alternate space and tint transform convert, so a
// named ink still prints as itself; [Options.ConvertSpots] converts it to
// process colour through its tint transform instead. Indexed spaces keep
// their indices and convert their palette, and Pattern spaces convert the
// pattern or the base space of an uncoloured one.
//
// Colour moves between spaces through an [Engine], device values to device
// values through the profile connection space: by default the ICC engine of
// package icc (ICC.1:2010), with CalGray, CalRGB and Lab spaces carried to the
// connection space by the formulas of ISO 32000-1, 8.6.5. A [Converter]
// converts one source space's colours to the target's components.
//
// [Content] walks a document's pages, writing every object it changes to a
// [writer.ObjAllocator] in place of the original, so an object several pages
// share is converted once. The images the walk reaches convert in the same
// [Session], through the same plans: [Images] converts their samples, or the
// colour table of an Indexed image, and a spot colour image keeps the space
// the colour operators keep.
package recolor
