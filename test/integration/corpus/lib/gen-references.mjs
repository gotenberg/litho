#!/usr/bin/env node
/**
 * gen-references.mjs
 *
 * Walks a flat directory of PDFs and, for each file, emits two JSON
 * references derived from PDF.js: one at --text-out (per-page text +
 * items, plus metadata) and one at --meta-out (metadata only).
 *
 * The shape matches corpus/pdf.js/gen-pdfjs-reference.mjs so the
 * corpus CLI's text/metadata features parse both corpora with one
 * loader.
 *
 * Usage:
 *   node gen-references.mjs \
 *     --pdfjs-dir /tmp/pdf.js \
 *     --pdfs-dir /output/pdfs \
 *     --text-out /output/goldens/text/ \
 *     --meta-out /output/goldens/metadata/
 */

import fs from "fs";
import path from "path";

const args = process.argv.slice(2);
let pdfjsDir = "";
let pdfsDir = "";
let textOut = "";
let metaOut = "";

for (let i = 0; i < args.length; i++) {
  switch (args[i]) {
    case "--pdfjs-dir":
      pdfjsDir = args[++i];
      break;
    case "--pdfs-dir":
      pdfsDir = args[++i];
      break;
    case "--text-out":
      textOut = args[++i];
      break;
    case "--meta-out":
      metaOut = args[++i];
      break;
  }
}

if (!pdfjsDir || !pdfsDir || !textOut || !metaOut) {
  console.error(
    "Usage: gen-references.mjs --pdfjs-dir <dir> --pdfs-dir <dir> --text-out <dir> --meta-out <dir>",
  );
  process.exit(1);
}

const buildPath = path.resolve(
  pdfjsDir,
  "build",
  "generic-legacy",
  "build",
  "pdf.mjs",
);
if (!fs.existsSync(buildPath)) {
  console.error(
    `pdf.js build not found at ${buildPath}.\n` +
      `Run 'npx gulp generic-legacy' in ${pdfjsDir} first.`,
  );
  process.exit(1);
}

const pdfjsLib = await import("file://" + buildPath);

// PDF.js reads a predefined CMap (ISO 32000-1, 9.7.5.2, Table 118) and the
// Adobe <Ordering>-UCS2 tables from cMapUrl. Without it every font naming one
// fails to load and its text comes out empty, whatever the file says.
const cMapUrl =
  path.resolve(pdfjsDir, "build", "generic-legacy", "web", "cmaps") + "/";
if (!fs.existsSync(cMapUrl)) {
  console.error(`pdf.js CMaps not found at ${cMapUrl}.`);
  process.exit(1);
}

fs.mkdirSync(textOut, { recursive: true });
fs.mkdirSync(metaOut, { recursive: true });

function round(val, decimals) {
  if (typeof val !== "number" || isNaN(val)) return 0;
  const factor = Math.pow(10, decimals);
  return Math.round(val * factor) / factor;
}

// lineBreakTolerance is how far, in user space units, an item may sit from
// the previous one across the line before it starts a new line.
const lineBreakTolerance = 2;

// joinLines concatenates a page's text items into the golden text, one line
// per line of text as it runs on the page. A horizontal line is the run of
// items sharing a baseline, so a horizontal item starts a new line when its Y
// moves. A vertical line is a column: PDF.js gives the items of a vertical
// font (writing mode 1, ISO 32000-1, 9.7.4.3) dir "ttb", their origins
// advance down the column, and the next column sits at another X. A vertical
// item therefore starts a new line when its X moves. A change of writing mode
// starts a new line when the item moved at all: PDF.js puts the empty
// end-of-line marker that precedes a change of mode at the origin of the item
// that follows, and that marker already broke the line. PDF.js emits the
// spaces and end-of-line markers it synthesises with dir "ltr" whatever the
// writing mode, so a blank item keeps the writing mode of the line it sits
// in. A page with no vertical item breaks exactly where it did when only Y was
// compared.
function joinLines(items) {
  let fullText = "";
  let mode = null;
  let lastX = 0;
  let lastY = 0;
  for (const item of items) {
    if (item.str === undefined) continue;
    const x = item.transform[4];
    const y = item.transform[5];
    const blank = item.str.trim() === "";
    let itemMode = item.dir === "ttb" ? "vertical" : "horizontal";
    if (blank && mode !== null) {
      itemMode = mode;
    }
    if (mode !== null) {
      const movedX = Math.abs(x - lastX) > lineBreakTolerance;
      const movedY = Math.abs(y - lastY) > lineBreakTolerance;
      const across = itemMode === "vertical" ? movedX : movedY;
      if (itemMode === mode ? across : movedX || movedY) {
        fullText += "\n";
      }
    }
    fullText += item.str;
    mode = itemMode;
    lastX = x;
    lastY = y;
  }
  return fullText;
}

const pdfs = fs
  .readdirSync(pdfsDir)
  .filter((name) => name.toLowerCase().endsWith(".pdf"))
  .sort();

let generated = 0;
let failed = 0;

for (const name of pdfs) {
  const id = path.basename(name, path.extname(name));
  const pdfPath = path.join(pdfsDir, name);

  try {
    const data = new Uint8Array(fs.readFileSync(pdfPath));
    const doc = await pdfjsLib.getDocument({
      data,
      useSystemFonts: true,
      cMapUrl,
      cMapPacked: true,
    }).promise;

    const result = {
      id,
      file: name,
      numPages: doc.numPages,
      metadata: null,
      outline: null,
      pages: [],
    };

    try {
      const meta = await doc.getMetadata();
      result.metadata = {
        info: meta.info || null,
        contentDispositionFilename: meta.contentDispositionFilename || null,
      };
    } catch {
      // Some PDFs have broken metadata: skip silently.
    }

    try {
      const outline = await doc.getOutline();
      if (outline) {
        result.outline = outline.map((item) => ({
          title: item.title,
          bold: item.bold || false,
          italic: item.italic || false,
          dest: item.dest,
        }));
      }
    } catch {
      // Outline extraction can fail on malformed PDFs.
    }

    for (let i = 1; i <= doc.numPages; i++) {
      let page;
      try {
        page = await doc.getPage(i);
      } catch {
        result.pages.push({ pageNumber: i, error: "failed to get page" });
        continue;
      }

      try {
        const textContent = await page.getTextContent();
        const fullText = joinLines(textContent.items);
        result.pages.push({
          pageNumber: i,
          text: fullText,
          itemCount: textContent.items.length,
          items: textContent.items
            .filter((item) => item.str !== undefined)
            .map((item) => ({
              str: item.str,
              x: round(item.transform[4], 2),
              y: round(item.transform[5], 2),
              width: round(item.width, 2),
              height: round(item.height, 2),
              fontName: item.fontName || "",
            })),
        });
      } catch {
        result.pages.push({
          pageNumber: i,
          error: "failed to extract text",
        });
      }
    }

    // Text golden carries everything; metadata golden carries only
    // Info + outline. Splitting keeps the per-feature loader simple.
    const textPath = path.join(textOut, `${id}.json`);
    fs.writeFileSync(textPath, JSON.stringify(result, null, 2) + "\n");

    const metaOnly = {
      id: result.id,
      file: result.file,
      numPages: result.numPages,
      metadata: result.metadata,
      outline: result.outline,
      pages: [],
    };
    const metaPath = path.join(metaOut, `${id}.json`);
    fs.writeFileSync(metaPath, JSON.stringify(metaOnly, null, 2) + "\n");

    generated++;
    if (generated % 50 === 0) {
      console.log(`  ... ${generated} generated so far`);
    }
  } catch {
    failed++;
  }
}

console.log(`Done. generated=${generated} failed=${failed}`);
