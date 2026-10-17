/**
 * join-lines.mjs
 *
 * Joins the text items PDF.js's getTextContent returns for a page into the
 * text golden of that page. gen-references.mjs writes it; join-lines.test.mjs
 * covers it (node --test
 * test/integration/corpus/lib/join-lines.test.mjs).
 */

// lineBreakTolerance is how far, in user space units, an item may sit from
// the previous one across the line before it starts a new line.
const lineBreakTolerance = 2;

// sameDirectionTolerance is how far apart, in radians, the baselines of two
// items may point and still count as one direction. It absorbs the rounding
// of a rotation matrix, not a real change of direction.
const sameDirectionTolerance = 1e-3;

// sameBaselineRatio is how far across its baseline, as a fraction of the
// font size, the text after an end-of-line flag may sit and still continue
// the line the flag ended. PDF.js flags an upright line end only when the
// text moves across the line by more than the font size, so a flag over a
// smaller move is not a line end.
const sameBaselineRatio = 0.1;

// spaceInFlowRatio is the gap along a line, as a fraction of the font size,
// from which PDF.js reads a space between two glyphs: its
// SPACE_IN_FLOW_MIN_FACTOR (src/core/evaluator.js, addFakeSpaces).
const spaceInFlowRatio = 0.102;

// baselineAngle returns the angle of an item's baseline in user space. An
// item's transform is its text rendering matrix (ISO 32000-1, 9.4.4): its
// first row is the image of the text space x axis, the direction a
// horizontal line advances in.
function baselineAngle(transform) {
  return Math.atan2(transform[1], transform[0]);
}

// angleDistance returns how far apart two angles point, between 0 and pi.
function angleDistance(a, b) {
  const d = Math.abs(a - b) % (2 * Math.PI);
  return d > Math.PI ? 2 * Math.PI - d : d;
}

// acrossLine returns how far an item at (dx, dy) from another moves across
// the line it runs in: along X for a vertical column, along the normal of
// the baseline otherwise. The normal of a baseline at angle a is
// (-sin a, cos a), so for upright text the distance is |dy|.
function acrossLine(mode, angle, dx, dy) {
  if (mode === "vertical") {
    return Math.abs(dx);
  }
  return Math.abs(-dx * Math.sin(angle) + dy * Math.cos(angle));
}

// alongLine returns how far an item at (dx, dy) from another moves along the
// line it runs in: down a vertical column, along the baseline otherwise.
function alongLine(mode, angle, dx, dy) {
  if (mode === "vertical") {
    return -dy;
  }
  return dx * Math.cos(angle) + dy * Math.sin(angle);
}

// advance returns how far an item's text runs along its line, in user space.
function advance(mode, item) {
  return mode === "vertical" ? item.height : item.width;
}

// fontSize returns the size of an item's font in user space: the length of
// the image of the text space axis that runs across its line.
function fontSize(mode, transform) {
  if (mode === "vertical") {
    return Math.hypot(transform[0], transform[1]);
  }
  return Math.hypot(transform[2], transform[3]);
}

// joinLines concatenates a page's text items into the golden text, one line
// per line of text as PDF.js reads it.
//
// PDF.js flags the end of a line with hasEOL, on the item the line ends with
// or on an empty marker item, and its own text extraction (the find
// controller, web/pdf_find_controller.js) writes a line break after each
// flagged item, so a golden line ends there too. PDF.js sets the flag when the
// next glyph moves across the line by more than the font size
// (compareWithLastPosition, src/core/evaluator.js), comparing positions in the
// frame of the text's own rotation, so the flag also ends rotated lines and
// lines of a tiny font. A flag before text that sits on the baseline it ended
// (within [sameBaselineRatio] of the font size) is not a line end: PDF.js
// reads a skewed text matrix, an oblique font, as a rotation it cannot undo
// exactly, and flags the next glyph of the same line. Such a flag reads as
// what PDF.js writes between two glyphs of a line: a space when the gap along
// the line reaches its space threshold ([spaceInFlowRatio]), nothing
// otherwise.
//
// Positions break the lines PDF.js leaves unflagged, as they always did: PDF.js
// starts the text of a form XObject with no previous glyph to compare with,
// and it does not end a line at a superscript or subscript. A horizontal item
// starts a new line when it moves across its baseline by more than
// [lineBreakTolerance]: along the Y axis for upright text, and along the
// normal of the baseline for rotated text, whose lines a Y test glues together
// and splits at every item. A vertical line is a column: PDF.js gives the
// items of a vertical font (writing mode 1, ISO 32000-1, 9.7.4.3) dir "ttb",
// their origins advance down the column, and the next column sits at another
// X. A vertical item therefore starts a new line when its X moves. A change of
// writing mode or of baseline direction starts a new line when the item moved
// at all. PDF.js emits the spaces and end-of-line markers it synthesises with
// dir "ltr" whatever the writing mode, so a blank item keeps the writing mode
// and direction of the line it sits in. A line break is written once, however
// many reasons end the line.
export function joinLines(items) {
  let fullText = "";
  let mode = null;
  let angle = 0;
  let lastX = 0;
  let lastY = 0;
  // anchor is the last item that carried text, which a pending end-of-line
  // flag is measured from.
  let anchor = null;
  let endOfLine = false;
  let lineOpen = false;
  for (const item of items) {
    if (item.str === undefined) continue;
    const x = item.transform[4];
    const y = item.transform[5];
    const blank = item.str.trim() === "";
    let itemMode = item.dir === "ttb" ? "vertical" : "horizontal";
    let itemAngle = baselineAngle(item.transform);
    if (blank && mode !== null) {
      itemMode = mode;
      itemAngle = angle;
    }
    let lineBreak = false;
    if (mode !== null) {
      const dx = x - lastX;
      const dy = y - lastY;
      const moved =
        Math.abs(dx) > lineBreakTolerance || Math.abs(dy) > lineBreakTolerance;
      const turned =
        itemMode !== mode ||
        angleDistance(itemAngle, angle) > sameDirectionTolerance;
      lineBreak = turned
        ? moved
        : acrossLine(itemMode, itemAngle, dx, dy) > lineBreakTolerance;
    }
    let wordBreak = false;
    if (endOfLine && !blank) {
      const dx = x - anchor.transform[4];
      const dy = y - anchor.transform[5];
      const size = fontSize(itemMode, anchor.transform);
      if (acrossLine(itemMode, itemAngle, dx, dy) > sameBaselineRatio * size) {
        lineBreak = true;
      } else {
        const gap =
          alongLine(itemMode, itemAngle, dx, dy) - advance(itemMode, anchor);
        wordBreak = gap >= spaceInFlowRatio * size;
      }
      endOfLine = false;
    }
    if (lineBreak && lineOpen) {
      fullText += "\n";
      lineOpen = false;
      endOfLine = false;
    } else if (
      wordBreak &&
      lineOpen &&
      !/\s$/.test(fullText) &&
      !/^\s/.test(item.str)
    ) {
      fullText += " ";
    }
    fullText += item.str;
    if (item.str !== "") {
      lineOpen = true;
    }
    if (!blank) {
      anchor = item;
    }
    if (item.hasEOL === true && anchor !== null) {
      endOfLine = true;
    }
    mode = itemMode;
    angle = itemAngle;
    lastX = x;
    lastY = y;
  }
  return fullText;
}
