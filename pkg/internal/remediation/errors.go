package remediation

import "errors"

// errXMPPropertyNotFound is returned by the XMP splicer helpers when a
// property lookup fails. It is internal; callers switch on it via
// [errors.Is].
var errXMPPropertyNotFound = errors.New("remediation: xmp property not found")

// errInlineImageSplice reports that the repairs of a content stream holding an
// inline image cannot be spliced in place of the operations they change
// ([spliceOperations]). Writing the stream again from its parsed operations
// would drop the images' data (ISO 32000-1, 8.9.7), so it is left as written.
var errInlineImageSplice = errors.New("remediation: the repairs of a content stream holding an inline image cannot be spliced")

// errStructTreeUnread reports that a walk of the structure tree, its
// /ParentTree or its /IDTree could not read the whole of it, so an edit built
// on the walk could leave a reference behind.
var errStructTreeUnread = errors.New("remediation: the structure tree cannot be read whole")
