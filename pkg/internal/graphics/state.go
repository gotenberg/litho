package graphics

import (
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TextState represents the text-specific graphics state.
type TextState struct {
	Tc    float64    // Character spacing
	Tw    float64    // Word spacing
	Tz    float64    // Horizontal scaling (default 100)
	Tl    float64    // Leading
	Ts    float64    // Text rise (ISO 32000-1, 9.3.7)
	Tf    string     // Font name
	Tfs   float64    // Font size
	Tmode int        // Text rendering mode (0=Fill, 1=Stroke, 3=Invisible, etc)
	Tm    [6]float64 // Text matrix
	Tlm   [6]float64 // Text line matrix
	// Unadvanced is how far Tm advanced past where PDF.js reads the
	// position, since a text-positioning operator last set it: text
	// extraction adds the advance of each glyph PDF.js does not advance
	// past, and the operators that set the position (BT, ET, Td, TD, Tm,
	// T*, ' and ") reset it. Tf leaves it alone, as it leaves Tm (ISO
	// 32000-1, 9.3.1 and 9.4.2).
	//
	// It rides the state, so q saves it and Q restores it with Tm. ISO
	// 32000-1, 8.2, Figure 9 allows no q or Q inside a text object, and
	// neither the graphics state (8.4.1, Tables 52 and 53) nor the text
	// state (9.3.1, Table 104) lists the text matrix, so no clause says
	// what Q does to it there. The machine restores Tm with the rest of the
	// state, as PDF.js restores its whole text state, text matrix included
	// (src/core/evaluator.js, StateManager.save and TextState.clone), and
	// the shift has to follow the matrix it measures.
	Unadvanced float64
}

// Point represents a 2D coordinate.
type Point struct {
	X, Y float64
}

// Path represents a sequence of drawing operations.
type Path []Point

// BoundingBox represents a rectangular region.
type BoundingBox struct {
	MinX, MinY, MaxX, MaxY float64
}

// State represents the current graphics state.
type State struct {
	CTM              [6]float64 // Current Transformation Matrix
	LineWidth        float64
	StrokeColor      []float64
	StrokeColorSpace string // "DeviceGray", "DeviceRGB", "DeviceCMYK", or a named space
	FillColor        []float64
	FillColorSpace   string // "DeviceGray", "DeviceRGB", "DeviceCMYK", or a named space
	FillPatternName  string // Pattern resource name when FillColorSpace is "Pattern"
	FillPatternType  string // "Tiling" or "Shading" (resolved from /PatternType)
	Text             TextState
	SkipExtraction   bool        // Used for Optional Content Groups (Hidden Layers)
	ClippingPath     BoundingBox // Simplified clipping to a bounding box for performance
	// ClipApplied reports whether a W or W* operator has taken part in the
	// current clipping path. [NewMachine] seeds ClippingPath with the page box,
	// so without this flag "outside the clipping path" cannot be told from
	// "outside the CropBox", which is already reported separately. It rides the
	// graphics state, so q/Q restore it with everything else.
	ClipApplied bool
}

// MarkedContentState tracks the nested state of marked content blocks.
type MarkedContentState struct {
	ActualText    string
	HasText       bool
	Tag           string
	MCID          int
	IsHiddenOC    bool
	IsArtifact    bool   // True when the block is tagged /Artifact
	Language      string // /Lang tag for TTS language override (BCP 47)
	ExpansionText string // /E tag for abbreviation/acronym expansion
}

// Machine is the graphics state machine that processes content stream operations.
type Machine struct {
	Stack              []State
	MarkedContent      []MarkedContentState
	IgnoreTextOps      bool
	StripArtifacts     bool // When true, text inside /Artifact marked content is discarded
	IgnoreOCVisibility bool // When true, optional-content layer visibility does not drive SkipExtraction; the machine still records IsHiddenOC for callers that care.
	PendingActualText  string
	OCConfigOn         map[int]bool
	OCConfigOff        map[int]bool
	ActiveOCs          map[string]bool
	CurrentPath        BoundingBox // Tracks the current active path bounds
	Doc                *document.Document
	Resources          ast.PdfDict // Current page resources (for resolving Pattern names)
	// Staged holds objects an edit has queued for writing, keyed by object
	// number, and the machine prefers them to the document's when it resolves
	// an XObject. A caller reading content as earlier edits staged it sets it:
	// a stamp queues the XObject it draws at a number the document does not
	// define, and resolving that against the document alone counted the Do as
	// unresolved. Nil reads the document alone.
	Staged map[int]ast.PdfObject
	// Chunk is a monotonically increasing counter bumped by text
	// operators that start or reposition a new show-text chain
	// (BT, ET, Td, TD, Tm, Tf, T*). Glyphs produced while it is
	// unchanged came from the same operator chain, so downstream
	// reconstruction can treat them as one logical text run and
	// suppress gap-based space insertion inside that run.
	Chunk int
	// RecordUnknownOperators makes the machine remember every operator it
	// has no handler for. Off by default, because ignoring one is right for
	// extraction: a page still yields its text when the machine skips a
	// construct it does not model. It is wrong for a caller that must refuse
	// rather than silently pass over content it cannot account for, which
	// cannot currently tell the two apart. See [Machine.UnknownOperators].
	RecordUnknownOperators bool
	unknownOperators       map[string]int
	// RecordImageBoxes makes the machine remember where each image XObject
	// paints. Off by default, because the common caller extracts text and would
	// pay for geometry it never reads. A caller that removes content needs it:
	// an image inside the region it is clearing is content it must account for.
	// See [Machine.ImageBoxes] and [Machine.UnresolvedXObjects].
	RecordImageBoxes   bool
	imageBoxes         []ImageBox
	unresolvedXObjects int
	// RecordPathBoxes makes the machine remember where each painted path
	// covers. Off by default for the same reason as [Machine.RecordImageBoxes]:
	// the common caller extracts text and would pay for geometry it never
	// reads. See [Machine.PathBoxes].
	RecordPathBoxes bool
	pathBoxes       []PathBox
	// RecordShadingBoxes makes the machine remember the bounds of the clipping
	// region in force at each sh operator. Off by default for the same reason
	// as [Machine.RecordImageBoxes]. See [Machine.ShadingBoxes].
	RecordShadingBoxes bool
	shadingBoxes       []BoundingBox
	// RecordShowAdvances makes the text extractor remember how far each
	// show-text operator of the page's own content stream moved the text
	// matrix. Off by default for the same reason as [Machine.RecordImageBoxes].
	// A caller that removes a show operator needs it: the operator's advance
	// is what positions every later glyph on the same line, so removing it
	// without putting the advance back moves text the caller never selected.
	// See [Machine.ShowAdvances].
	RecordShowAdvances bool
	showAdvances       map[int]ShowAdvance
	// RecordGlyphAdvances makes the text extractor remember, code by code,
	// how each show-text operator of the page's own content stream split its
	// strings and moved the text matrix. Off by default for the same reason as
	// [Machine.RecordImageBoxes]. A caller that removes some of an operator's
	// glyphs and keeps the rest needs it: the kept codes stay where they were
	// only if each removed code's advance is put back. See
	// [Machine.GlyphAdvances].
	RecordGlyphAdvances bool
	glyphAdvances       map[int][]GlyphAdvance
	// MarkUnvaluedText makes the text extractor read as U+FFFD every code of
	// a Type 0 font that no mapping gives a character, which ISO 32000-1,
	// 9.10.2 gives no Unicode value. The default extraction reads such a code
	// as its own value, the stand-in PDF.js reads, while the code's CID is
	// unknown, and as nothing once litho holds the font's whole encoding CMap
	// and the code selects .notdef (9.7.6.3). Off by default. A caller
	// comparing how much text two versions of a document show needs it: a
	// repair that moves such a code from one reading to the other shows the
	// same glyph and loses no character a reader is given.
	MarkUnvaluedText bool
	// FormDepth is the nesting level of the content stream being executed: 0 for
	// the page's own, one deeper per form XObject a caller recurses into. The
	// machine cannot know this by itself, because a form's operators arrive
	// through the same Execute calls as the page's; the caller driving the walk
	// sets it. Recorded geometry carries it so a caller can tell a mark the page
	// made from one a form made, whose bytes live in another stream.
	FormDepth int
	// OpIndex is the position in the caller's operation slice of the operator
	// now executing. The machine cannot know it for the same reason as
	// [Machine.FormDepth]: operators arrive one at a time with no index
	// attached, so the caller driving the walk sets it. Recorded geometry
	// carries it, which is what lets a caller map a mark back to the operator
	// that made it instead of re-deriving the correspondence by counting.
	OpIndex int
	// pathStartOp is the OpIndex of the first construction operator of the path
	// being built. Bounds alone do not say which bytes drew them, and removing a
	// paint operator without the construction operators that fed it would leave
	// the path behind to merge into the next one.
	pathStartOp int
	// pathClipped records whether a W or W* took part in the path being built.
	// Such a path both paints and narrows the clip for everything after it, so a
	// caller removing it would change how later content renders.
	pathClipped bool
}

// NewMachine initializes a new graphics state machine with the default state.
// CropBox can be passed in to set the initial clipping region.
func NewMachine(cropBox [4]float64) *Machine {
	// If cropBox is 0,0,0,0, set it to a very large number
	if cropBox[2] == 0 && cropBox[3] == 0 {
		cropBox = [4]float64{-10000, -10000, 10000, 10000}
	} else {
		// Ensure min/max are correct
		if cropBox[0] > cropBox[2] {
			cropBox[0], cropBox[2] = cropBox[2], cropBox[0]
		}
		if cropBox[1] > cropBox[3] {
			cropBox[1], cropBox[3] = cropBox[3], cropBox[1]
		}
	}

	defaultState := State{
		CTM: [6]float64{1, 0, 0, 1, 0, 0},
		Text: TextState{
			Tz:    100.0,
			Tm:    [6]float64{1, 0, 0, 1, 0, 0},
			Tlm:   [6]float64{1, 0, 0, 1, 0, 0},
			Tmode: 0, // Fill text by default
		},
		LineWidth:        1.0,
		FillColor:        []float64{0}, // DeviceGray 0 = black (PDF spec default)
		FillColorSpace:   "DeviceGray", // PDF spec default color space
		StrokeColor:      []float64{0}, // DeviceGray 0 = black
		StrokeColorSpace: "DeviceGray",
		ClippingPath:     BoundingBox{MinX: cropBox[0], MinY: cropBox[1], MaxX: cropBox[2], MaxY: cropBox[3]},
	}
	return &Machine{
		Stack:         []State{defaultState},
		MarkedContent: make([]MarkedContentState, 0),
		OCConfigOn:    make(map[int]bool),
		OCConfigOff:   make(map[int]bool),
		ActiveOCs:     make(map[string]bool),
		CurrentPath:   emptyBoundingBox,
	}
}

// Current returns a pointer to the current graphics state.
func (m *Machine) Current() *State {
	return &m.Stack[len(m.Stack)-1]
}
