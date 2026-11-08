// Package css parses CSS and computes the style of every element of an
// HTML tree: stylesheets and an x/net/html DOM in, one [Style] per element
// out, with the @page styles and the @font-face rules beside them.
//
// # Pipeline
//
//   - [Parse] tokenizes and parses a stylesheet as CSS Syntax 3 asks, with
//     its error recovery, fetches its imports through
//     [ParseOptions.Fetch], and keeps the style rules, @page rules and
//     @font-face rules with their media conditions. Values are parsed and
//     shorthands expanded into longhands here; a value holding var() waits
//     for computed-value time.
//   - [NewResolver] takes stylesheets in cascade order with their origins,
//     drops the rules whose @media conditions do not match a [Media], and
//     indexes the selectors by their rightmost compound.
//   - [Resolver.Compute] matches selectors (a Selectors 4 subset), sorts
//     the declarations of each element by origin and importance,
//     specificity and order (CSS Cascade 4), adds the style attribute and
//     the presentational hints, substitutes custom properties (CSS
//     Variables 1) and computes every longhand into points, as [Style]
//     promises.
//
// [HTMLUserAgent] is the user agent stylesheet for HTML and [HTMLHints]
// the presentational hints of HTML attributes.
//
// # Not supported
//
// Namespaced selectors match nothing; :has(), :dir() and the form state
// pseudo-classes other than :checked, :disabled and :enabled make a
// selector invalid. min(), max() and clamp() comparing a length with a
// percentage make a value invalid, and so does a calc() mixing lengths and
// percentages outside the box sizes, margins, paddings, insets, gaps and
// flex-basis, which hold it as a [Calc]. Cascade layers are flattened into source order; @container
// and @scope are ignored with their rules. Logical properties map onto the
// physical ones of a horizontal left-to-right box. The input is UTF-8.
// The flexible box layout properties and the box alignment properties that
// apply to it compute in full (CSS Flexbox 1; CSS Box Alignment 3), and
// the children of a flex container are blockified; so do the grid layout
// properties (CSS Grid 1, without subgrid), whose items are blockified
// too, and the sizing keywords min-content, max-content, fit-content and
// stretch of the size properties (CSS Sizing 3 and 4). Generated content,
// 3D transforms and columns parse and compute to the nearest supported
// value, with a warning.
//
// # Concurrency
//
// A [Stylesheet], a [Resolver] and a [Computed] are immutable and safe for
// concurrent use. [Resolver.Compute] keeps its state per call.
//
// # Determinism
//
// For the same stylesheets, tree and options, [Resolver.Compute] returns
// the same styles and the same warnings in the same order. No map
// iteration decides an order.
package css
