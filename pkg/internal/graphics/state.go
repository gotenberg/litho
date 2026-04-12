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
	Tf    string     // Font name
	Tfs   float64    // Font size
	Tmode int        // Text rendering mode (0=Fill, 1=Stroke, 3=Invisible, etc)
	Tm    [6]float64 // Text matrix
	Tlm   [6]float64 // Text line matrix
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
	Stack             []State
	MarkedContent     []MarkedContentState
	IgnoreTextOps     bool
	StripArtifacts    bool // When true, text inside /Artifact marked content is discarded
	PendingActualText string
	OCConfigOn        map[int]bool
	OCConfigOff       map[int]bool
	ActiveOCs         map[string]bool
	CurrentPath       BoundingBox // Tracks the current active path bounds
	Doc               *document.Document
	Resources         ast.PdfDict // Current page resources (for resolving Pattern names)
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
