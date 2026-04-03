#!/usr/bin/env node
/**
 * gen-pdfjs-reference.mjs
 *
 * Generates reference JSON fixtures from the PDF.js corpus using a locally
 * built pdf.js library. Each fixture contains metadata, outline, and per-page
 * text content — used as ground truth for litho comparison tests.
 *
 * Requires a local pdf.js clone with `npx gulp generic` already run.
 *
 * Usage:
 *   node scripts/gen-pdfjs-reference.mjs \
 *     --pdfjs-dir tmp/pdf.js \
 *     --manifest test/integration/testdata/pdf.js/test_manifest.json \
 *     --out test/integration/testdata/pdf.js/references/
 */

import fs from "fs";
import path from "path";

// ---------------------------------------------------------------------------
// CLI args
// ---------------------------------------------------------------------------
const args = process.argv.slice(2);
let pdfjsDir = "";
let manifestPath = "";
let outDir = "";

for (let i = 0; i < args.length; i++) {
  switch (args[i]) {
    case "--pdfjs-dir":
      pdfjsDir = args[++i];
      break;
    case "--manifest":
      manifestPath = args[++i];
      break;
    case "--out":
      outDir = args[++i];
      break;
  }
}

if (!pdfjsDir || !manifestPath || !outDir) {
  console.error(
    "Usage: node gen-pdfjs-reference.mjs --pdfjs-dir <dir> --manifest <path> --out <dir>",
  );
  process.exit(1);
}

// ---------------------------------------------------------------------------
// Load pdf.js from the local build
// ---------------------------------------------------------------------------
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

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------
function round(val, decimals) {
  if (typeof val !== "number" || isNaN(val)) return 0;
  const factor = Math.pow(10, decimals);
  return Math.round(val * factor) / factor;
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------
const manifest = JSON.parse(fs.readFileSync(manifestPath, "utf-8"));
const corpusDir = path.dirname(manifestPath);

fs.mkdirSync(outDir, { recursive: true });

// Deduplicate by file path.
const seen = new Set();
const entries = [];
for (const entry of manifest) {
  if (seen.has(entry.file)) continue;
  seen.add(entry.file);
  entries.push(entry);
}

let generated = 0;
let skipped = 0;
let failed = 0;

for (const entry of entries) {
  const pdfPath = path.join(corpusDir, entry.file);

  if (!fs.existsSync(pdfPath)) {
    skipped++;
    continue;
  }

  try {
    const data = new Uint8Array(fs.readFileSync(pdfPath));
    const doc = await pdfjsLib.getDocument({
      data,
      useSystemFonts: true,
    }).promise;

    const result = {
      id: entry.id,
      file: entry.file,
      numPages: doc.numPages,
      metadata: null,
      outline: null,
      pages: [],
    };

    // Metadata.
    try {
      const meta = await doc.getMetadata();
      result.metadata = {
        info: meta.info || null,
        contentDispositionFilename: meta.contentDispositionFilename || null,
      };
    } catch {
      // Some PDFs have broken metadata — skip silently.
    }

    // Outline (bookmarks).
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

    // Per-page text content.
    const lastPage =
      entry.lastPage && entry.lastPage < doc.numPages
        ? entry.lastPage
        : doc.numPages;

    for (let i = 1; i <= lastPage; i++) {
      let page;
      try {
        page = await doc.getPage(i);
      } catch {
        result.pages.push({ pageNumber: i, error: "failed to get page" });
        continue;
      }

      try {
        const textContent = await page.getTextContent();

        // Build concatenated text the same way a consumer would.
        let fullText = "";
        let lastY = null;
        for (const item of textContent.items) {
          if (item.str === undefined) continue;
          // Insert newline when Y coordinate changes significantly.
          if (lastY !== null && Math.abs(item.transform[5] - lastY) > 2) {
            fullText += "\n";
          }
          fullText += item.str;
          lastY = item.transform[5];
        }

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

    const outPath = path.join(outDir, `${entry.id}.json`);
    fs.writeFileSync(outPath, JSON.stringify(result, null, 2) + "\n");
    generated++;

    if (generated % 50 === 0) {
      console.log(`  ... ${generated} generated so far`);
    }
  } catch {
    failed++;
  }
}

console.log(`Done. generated=${generated} skipped=${skipped} failed=${failed}`);
