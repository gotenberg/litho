// Package surface holds the raster targets the renderer draws into: a
// premultiplied RGBA [Canvas] and an alpha [Mask] for coverage and clipping.
//
// Both are bounded at construction by a pixel budget the caller passes, so a
// page whose size or resolution comes from the file cannot allocate without
// limit. Neither type is safe for concurrent writes; distinct values may be
// used from distinct goroutines.
package surface
