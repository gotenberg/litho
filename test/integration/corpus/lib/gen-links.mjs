#!/usr/bin/env node
/**
 * gen-links.mjs
 *
 * Walks a flat directory of PDFs and, for each file, writes one JSON golden
 * listing the Link annotations PDF.js reads on every page: the page, the
 * rectangle in default user space, and the target, a URI or the 1-based page
 * an internal destination lands on. The diff feature of the corpus CLI
 * scores the links of litho's conversion of the same source against it.
 *
 * Usage:
 *   node gen-links.mjs \
 *     --pdfjs-dir /opt/pdfjs \
 *     --pdfs-dir /output/pdfs \
 *     --out /output/goldens/diff/links/
 */

import fs from "fs";
import path from "path";

const args = process.argv.slice(2);
let pdfjsDir = "";
let pdfsDir = "";
let out = "";

for (let i = 0; i < args.length; i++) {
  switch (args[i]) {
    case "--pdfjs-dir":
      pdfjsDir = args[++i];
      break;
    case "--pdfs-dir":
      pdfsDir = args[++i];
      break;
    case "--out":
      out = args[++i];
      break;
  }
}

if (!pdfjsDir || !pdfsDir || !out) {
  console.error(
    "Usage: gen-links.mjs --pdfjs-dir <dir> --pdfs-dir <dir> --out <dir>",
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
  console.error(`pdf.js build not found at ${buildPath}.`);
  process.exit(1);
}
const pdfjsLib = await import("file://" + buildPath);

fs.mkdirSync(out, { recursive: true });

function round(val) {
  return typeof val === "number" && !isNaN(val)
    ? Math.round(val * 100) / 100
    : 0;
}

// destPage resolves an internal destination, named or explicit (ISO
// 32000-1, 12.3.2), to the 1-based page it lands on, or 0.
async function destPage(doc, dest) {
  try {
    let explicit = dest;
    if (typeof dest === "string") {
      explicit = await doc.getDestination(dest);
    }
    if (!Array.isArray(explicit) || explicit.length === 0) return 0;
    const target = explicit[0];
    if (typeof target === "number") return target + 1;
    return (await doc.getPageIndex(target)) + 1;
  } catch {
    return 0;
  }
}

const pdfs = fs
  .readdirSync(pdfsDir)
  .filter((name) => name.toLowerCase().endsWith(".pdf"))
  .sort();

let generated = 0;
for (const name of pdfs) {
  const id = path.basename(name, path.extname(name));
  const data = new Uint8Array(fs.readFileSync(path.join(pdfsDir, name)));
  const result = { id, file: name, numPages: 0, links: [] };
  try {
    const doc = await pdfjsLib.getDocument({ data }).promise;
    result.numPages = doc.numPages;
    for (let i = 1; i <= doc.numPages; i++) {
      const page = await doc.getPage(i);
      const annots = await page.getAnnotations({ intent: "display" });
      for (const a of annots) {
        if (a.subtype !== "Link") continue;
        const link = { page: i, rect: (a.rect || []).map(round) };
        if (a.url) link.uri = a.url;
        else if (a.unsafeUrl) link.uri = a.unsafeUrl;
        if (a.dest) link.destPage = await destPage(doc, a.dest);
        result.links.push(link);
      }
    }
  } catch (e) {
    result.error = String(e && e.message ? e.message : e);
  }
  fs.writeFileSync(
    path.join(out, `${id}.json`),
    JSON.stringify(result, null, 2) + "\n",
  );
  generated++;
}
console.log(`  links: ${generated} goldens written`);
