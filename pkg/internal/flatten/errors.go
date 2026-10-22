package flatten

import "errors"

var (
	// ErrTooComplex reports a page flattening cannot handle within its
	// budgets: regions that do not settle within the passes [Analyze]
	// allows them, or transparency the render pixel budget cannot
	// composite.
	ErrTooComplex = errors.New("flatten: page too complex to flatten")
	// ErrInvalidOptions reports [Options] flattening cannot run with.
	ErrInvalidOptions = errors.New("flatten: invalid options")
	// ErrPageStaged reports a page [Redaction] cannot composite because the
	// allocator holds an edit of it: the renderer reads the document, not
	// the edits queued on it, so its composite would show the page as it
	// was before them.
	ErrPageStaged = errors.New("flatten: page has edits the renderer does not read")
)

// errNotForm reports a Do replaced by its text that names no form XObject.
var errNotForm = errors.New("flatten: not a form XObject")
