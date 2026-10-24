// Package imagetiles encodes a raster of 8-bit samples as images whose
// photographic parts are baseline JPEG (ITU-T T.81) and whose other parts
// stay lossless: the raster is cut on a grid, the runs of cells that only
// photographic content reaches are encoded as JPEG tiles, and the tiles are
// drawn over one lossless image of the whole raster, so a renderer placing
// each image on its own draws no seam between them.
//
// Flattening's composites (package flatten), the raster fallback of redaction, and
// the blanking of photographs a redaction overwrites all encode their images
// through it, so a lossy image is cut the same way wherever litho writes one.
package imagetiles
