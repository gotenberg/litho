// Package limits holds the resource budgets one document is read under: the
// decoded size of a stream, the pixels of an image, the size of an embedded
// file compliance opens, the nesting of form XObjects, and the entries of the
// object cache. It imports no other litho package, so the filter pipeline, the
// codecs, the document and every walker above them read the same values.
package limits
