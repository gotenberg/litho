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

fs.mkdirSync(textOut, { recursive: true });
fs.mkdirSync(metaOut, { recursive: true });

function round(val, decimals) {
  if (typeof val !== "number" || isNaN(val)) return 0;
  const factor = Math.pow(10, decimals);
  return Math.round(val * factor) / factor;
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
    const doc = await pdfjsLib.getDocument({ data, useSystemFonts: true })
      .promise;

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
        let fullText = "";
        let lastY = null;
        for (const item of textContent.items) {
          if (item.str === undefined) continue;
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
