// Package limits holds the resource budgets one document is read under: the
// decoded size of a stream, the pixels of an image, the size of an embedded
// file compliance opens, the nesting of form XObjects, the entries of the
// object cache, the pixels of a rendered page, the source, pages and memory
// of a conversion. It imports no other litho package, so the filter
// pipeline, the codecs, the document, the converters and every walker above
// them read the same values.
//
// A conversion's memory is one [Budget], carried on its context: every site
// that takes memory a document asks for charges it there, at a cost per
// unit the site documents, and fails past the limit.
package limits
