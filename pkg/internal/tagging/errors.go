package tagging

import "errors"

// The reasons [AutoTag] leaves a document as it is. It reports one in
// [Result.Skipped] and returns no error: leaving a document alone is a
// decision it documents, not a failure.
var (
	// ErrAlreadyTagged reports a document that already declares a logical
	// structure: a catalog /StructTreeRoot (ISO 32000-1, 14.7.2) or /MarkInfo
	// with /Marked true (ISO 32000-1, 14.7.1). A second tree would replace the
	// producer's.
	ErrAlreadyTagged = errors.New("the document is already tagged")

	// ErrMarkedContent reports a page whose content already holds a
	// marked-content sequence, a BDC or BMC operator (ISO 32000-1, 14.6).
	// Marking it again would nest or cross the existing sequences.
	ErrMarkedContent = errors.New("the page content is already marked")

	// ErrUnreadableContent reports a page whose content cannot be assembled,
	// so AutoTag cannot prove it unmarked.
	ErrUnreadableContent = errors.New("the page content cannot be read")

	// ErrNothingToTag reports a document none of whose pages draws text or an
	// image the classifier places in a structure element.
	ErrNothingToTag = errors.New("no page draws content to tag")
)
