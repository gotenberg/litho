package models

import (
	"context"
	"image"
	"io"
)

// ImageMeta represents metadata for an extracted Image XObject.
//
// Open returns an [io.ReadCloser] over the image stream's data, decoded lazily
// through its filters on read. It yields the samples, packed at
// BitsPerComponent in ColorSpace, rather than the stored bytes or pixels. A
// filter litho cannot decode is skipped, its input handed on still encoded. The
// caller must close the returned reader.
//
// Decode turns this image's samples into pixels on demand. It decodes as
// Reader.DecodeImage in package pdf does, an opacity the JPEG 2000 data carries
// included.
//
// Filename can repeat on one page, since two form XObjects may each name an
// image Im0. Decode is the way to decode this entry rather than another image
// of the same name.
//
// Concurrent reads of a value are safe; a write needs synchronization.
// Open and Decode may be called from several goroutines at once, and alongside
// other reads of the Reader that returned the value, while that Reader stays
// open.
type ImageMeta struct {
	// Filename is the name the image has in its resource dictionary, such as
	// Im0. It is not a file name and can repeat on one page.
	Filename string `json:"filename"`
	// ObjectNum is the object number of the image XObject, shared by every
	// placement of the same image, or 0 when the image is a direct object.
	ObjectNum int `json:"objectNum,omitempty"`
	// Width is the image width in samples.
	Width int `json:"width"`
	// Height is the image height in samples.
	Height int `json:"height"`
	// ColorSpace names the image's colour space.
	ColorSpace string `json:"colorSpace"`
	// BitsPerComponent is the number of bits of each colour component. It is
	// 8 when the image states none or is JPXDecode data.
	BitsPerComponent int `json:"bitsPerComponent"`
	// Open reads the image's decoded samples. Reader.ExtractImages sets it on
	// every entry, and each call returns a new reader.
	Open func(ctx context.Context) (io.ReadCloser, error) `json:"-"`
	// Decode returns the image's pixels. Reader.ExtractImages sets it on every
	// entry.
	Decode func(ctx context.Context) (image.Image, error) `json:"-"`
}
