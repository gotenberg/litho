// Tests for join-lines.mjs. Run with: node --test test/integration/corpus/lib/join-lines.test.mjs

import assert from "node:assert/strict";
import { test } from "node:test";

import { joinLines } from "./join-lines.mjs";

// item builds a text item shaped as PDF.js's getTextContent returns it.
function item(
  str,
  transform,
  { width = 0, height = 0, dir = "ltr", eol = false } = {},
) {
  return { str, dir, transform, width, height, hasEOL: eol };
}

test("an upright line breaks when its Y moves by more than the tolerance", () => {
  const items = [
    item("one", [10, 0, 0, 10, 50, 700], { width: 15 }),
    item("two", [10, 0, 0, 10, 70, 699], { width: 15 }),
    item("three", [10, 0, 0, 10, 50, 688], { width: 25 }),
  ];
  assert.equal(joinLines(items), "onetwo\nthree");
});

test("a flagged line end separates lines closer than the tolerance", () => {
  // A signature block in a 0.2-unit font, its lines 0.22 units apart.
  const items = [
    item("Faris", [0.21, 0, 0, 0.21, 371.94, 176.02], { width: 1.53 }),
    item("", [0.18, 0, 0, 0.18, 371.95, 175.8], { eol: true }),
    item("United", [0.18, 0, 0, 0.18, 371.95, 175.8], { width: 2.49 }),
  ];
  assert.equal(joinLines(items), "Faris\nUnited");
});

test("a line break is written once however many reasons end the line", () => {
  const items = [
    item("first", [10, 0, 0, 10, 50, 700], { width: 25, eol: true }),
    item("", [10, 0, 0, 10, 50, 680], { eol: true }),
    item("second", [10, 0, 0, 10, 50, 680], { width: 30 }),
  ];
  assert.equal(joinLines(items), "first\nsecond");
});

test("rotated lines break across their own baseline", () => {
  // Text turned 90 degrees: a line runs up the page, the next sits to its
  // left at the same height.
  const items = [
    item("fo", [0, 8, -8, 0, 100, 100], { width: 9 }),
    item("o", [0, 8, -8, 0, 100, 109], { width: 4.5 }),
    item("bar", [0, 8, -8, 0, 88, 100], { width: 13.5 }),
  ];
  assert.equal(joinLines(items), "foo\nbar");
});

test("a flag before the same baseline in an oblique font is no line end", () => {
  // PDF.js flags the glyph after a switch to a skewed text matrix.
  const items = [
    item("Technical Note", [9, 0, 0, 9, 144.41, 38.1], { width: 63.97 }),
    item("", [9, 0, 3.08, 9, 208.38, 38.1], { eol: true }),
    item("-Forestry", [9, 0, 3.08, 9, 208.38, 38.1], { width: 40 }),
  ];
  assert.equal(joinLines(items), "Technical Note-Forestry");
});

test("a flag before a gap on the same baseline reads as a space", () => {
  const items = [
    item("Bureau,", [6, 0, 0, 6, 36, 22], { width: 56.36, eol: true }),
    item("the", [6, 0, 1.27, 6, 93.97, 22], { width: 10 }),
  ];
  assert.equal(joinLines(items), "Bureau, the");
});

test("a vertical column is a line", () => {
  const items = [
    item("上", [12, 0, 0, 12, 300, 700], { height: 12, dir: "ttb" }),
    item("下", [12, 0, 0, 12, 300, 688], { height: 12, dir: "ttb" }),
    item("左", [12, 0, 0, 12, 280, 700], { height: 12, dir: "ttb" }),
  ];
  assert.equal(joinLines(items), "上下\n左");
});
