package html

// MaxElements caps the elements of one document: 2^18, 262,144. Anti-DoS
// guard, not spec-mandated: the cascade and the layout each hold a few
// hundred bytes an element, and the selector matching time grows with the
// count. A long HTML report or a book chapter holds tens of thousands.
const MaxElements = 1 << 18

// maxTreeDepth caps the element nesting the tree builder follows: 512, the
// depth the layout engine lays out. Anti-DoS guard, not spec-mandated: the
// builder recurses once a level. Deeper content is left out with a warning.
const maxTreeDepth = 512

// maxResourceBytes caps one resource read through [Options.Resources]: 32
// MiB. Anti-DoS guard, not spec-mandated: a resource is held in memory
// while it is parsed or decoded. A photograph straight from a camera
// weighs under 20 MiB.
const maxResourceBytes = 32 << 20

// maxTotalResourceBytes caps the bytes one conversion reads through
// [Options.Resources], every stylesheet and image together: 256 MiB.
// Anti-DoS guard, not spec-mandated: a document naming the same large file
// under many URLs would otherwise read it again for each. Past it every
// further resource is left out with a warning.
const maxTotalResourceBytes = 256 << 20

// maxSpan caps a colspan or a col span at 1000 and a rowspan at 65534, the
// clamps of the HTML Standard, 4.9.11 (Processing model). Spec-mandated.
const (
	maxColSpan = 1000
	maxRowSpan = 65534
)

// maxOutlineTitleRunes caps the characters of one outline entry's title:
// 1024. Anti-DoS guard, not spec-mandated: a heading of a whole page of
// text would otherwise copy it into the outline. A real heading is a line.
const maxOutlineTitleRunes = 1024

// maxCounters caps the counter instances alive at once: 16,384. Anti-DoS
// guard, not spec-mandated: a counter costs a map entry, and a document
// whose siblings each reset new names would otherwise grow them with its
// element count. A document nests a few counters a few levels deep.
const maxCounters = 1 << 14

// maxGeneratedBytes caps the text one pseudo-element generates: 64 KiB.
// Anti-DoS guard, not spec-mandated: counters() repeats its separator once
// a nesting level, and a label is a few words.
const maxGeneratedBytes = 64 << 10
