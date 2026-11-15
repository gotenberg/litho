// Package imagetest writes the image files the tests of the image
// conversion and the litho/image corpus read: JPEG files of one, three
// or four components, baseline or progressive, with JFIF, EXIF, ICC and
// Adobe segments; WebP files, lossless, lossy and animated; JPEG 2000
// files; icon files; BMP files of several depths; multi-page TIFF files;
// and PNG files with ancillary chunks. Go's standard library and
// golang.org/x/image encode none of these, or not with every variant a
// test needs.
//
// Every encoder is integer arithmetic, so a file is the same bytes on
// every machine. The files are made to be read, not to be small: each
// format's simplest valid coding is used.
package imagetest
