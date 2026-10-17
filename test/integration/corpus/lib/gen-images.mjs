#!/usr/bin/env node
/**
 * gen-images.mjs
 *
 * Walks a flat directory of PDFs and, for each file, emits one JSON golden
 * describing the image XObjects PDF.js paints on each page: their size and a
 * coarse signature of their pixels. The corpus CLI's imagedecode feature
 * matches litho's decoded images against it.
 *
 * Inline images are left out: PDF.js paints the larger ones through the same
 * operator as image XObjects, but they are not XObjects.
 *
 * The signature splits an image into a grid of at most 4x4 cells and records
 * the mean red, green and blue of each cell, rounded to one decimal. Means
 * absorb the rounding differences two decoders legitimately disagree on,
 * while a wrong filter, colour space or orientation moves them far.
 *
 * PDF.js runs colour-managed, as a browser runs it: ICCBased images go
 * through their profiles and DeviceCMYK through the CMYK profile PDF.js
 * ships, both by its qcms WebAssembly module.
 *
 * Usage:
 *   node gen-images.mjs \
 *     --pdfjs-dir /opt/pdfjs \
 *     --pdfs-dir /output/pdfs \
 *     --out /output/goldens/images/
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
    "Usage: gen-images.mjs --pdfjs-dir <dir> --pdfs-dir <dir> --out <dir>",
  );
  process.exit(1);
}

const buildDir = path.resolve(pdfjsDir, "build", "generic-legacy");
const buildPath = path.join(buildDir, "build", "pdf.mjs");
if (!fs.existsSync(buildPath)) {
  console.error(`pdf.js build not found at ${buildPath}.`);
  process.exit(1);
}
const pdfjsLib = await import("file://" + buildPath);

// PDF.js decodes JPEG 2000 and JBIG2 through WebAssembly modules it loads
// from wasmUrl; without one it cannot decode those images at all.
const wasmDir = path.join(buildDir, "web", "wasm");
const wasmUrl = fs.existsSync(wasmDir) ? wasmDir + "/" : undefined;

// iccUrl holds the CMYK profile PDF.js reads DeviceCMYK through.
const iccDir = path.join(buildDir, "web", "iccs");
const iccUrl = fs.existsSync(iccDir) ? iccDir + "/" : undefined;

// PDF.js loads its colour module and CMYK profile with a synchronous
// XMLHttpRequest, which Node lacks. This one reads the local files those
// URLs name. The JPEG 2000 and JBIG2 modules load through fetch instead,
// which fails on a local path, so PDF.js decodes them with the JavaScript
// builds of the same decoders, as it did before colour management.
globalThis.XMLHttpRequest = class {
  open(_method, url) {
    this.url = url;
  }
  send() {
    this.response = new Uint8Array(fs.readFileSync(this.url)).buffer;
  }
};

fs.mkdirSync(out, { recursive: true });

// imageTimeout bounds the wait for one decoded image. PDF.js hands image data
// over asynchronously and never answers for one it failed to decode.
const imageTimeout = 30000;

// maxCells is the grid size along each axis.
const maxCells = 4;

// signature returns the grid of cell means for decoded PDF.js image data.
//
// PDF.js can hand over fewer samples than width x height: a stream that ends
// early leaves the rows past its end out of the buffer rather than padding
// them. Only the rows the buffer holds in full count. When it holds fewer
// than height rows, the signature records that number as rows, and a cell
// reaching a row PDF.js did not supply has no mean: it is stored as null,
// which the comparator skips rather than reading as a level. A cell is not
// averaged over its supplied rows alone, since a decoder that pads the
// missing rows would then disagree with PDF.js by what it padded with.
function signature(img) {
  const { width, height, kind, data } = img;
  const gw = Math.min(maxCells, width);
  const gh = Math.min(maxCells, height);
  const sums = new Float64Array(gw * gh * 3);
  const counts = new Float64Array(gw * gh);
  const rowBytes = (width + 7) >> 3;
  const stride = kind === pdfjsLib.ImageKind.RGBA_32BPP ? 4 : 3;
  const bytesPerRow =
    kind === pdfjsLib.ImageKind.GRAYSCALE_1BPP ? rowBytes : width * stride;
  const rows = Math.min(height, Math.floor(data.length / bytesPerRow));
  // A cell row is complete when its last image row lies below rows.
  const completeCellRows = [];
  for (let cy = 0; cy < gh; cy++) {
    const lastRow = Math.ceil(((cy + 1) * height) / gh) - 1;
    completeCellRows.push(lastRow < rows);
  }
  for (let y = 0; y < rows; y++) {
    const cy = Math.floor((y * gh) / height);
    for (let x = 0; x < width; x++) {
      const cx = Math.floor((x * gw) / width);
      let r, g, b;
      if (kind === pdfjsLib.ImageKind.GRAYSCALE_1BPP) {
        const v = ((data[y * rowBytes + (x >> 3)] >> (7 - (x & 7))) & 1) * 255;
        r = g = b = v;
      } else {
        const i = (y * width + x) * stride;
        r = data[i];
        g = data[i + 1];
        b = data[i + 2];
      }
      const c = cy * gw + cx;
      sums[c * 3] += r;
      sums[c * 3 + 1] += g;
      sums[c * 3 + 2] += b;
      counts[c]++;
    }
  }
  const sig = [];
  for (let c = 0; c < gw * gh; c++) {
    const complete = completeCellRows[Math.floor(c / gw)];
    for (let k = 0; k < 3; k++) {
      sig.push(
        complete ? Math.round((sums[c * 3 + k] / counts[c]) * 10) / 10 : null,
      );
    }
  }
  if (rows < height) {
    return { grid: [gw, gh], sig, rows };
  }
  return { grid: [gw, gh], sig };
}

// decodedImage waits for the image data PDF.js decoded under objId.
function decodedImage(page, objId) {
  const store = objId.startsWith("g_") ? page.commonObjs : page.objs;
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error("timeout")), imageTimeout);
    store.get(objId, (img) => {
      clearTimeout(timer);
      resolve(img);
    });
  });
}

const pdfs = fs
  .readdirSync(pdfsDir)
  .filter((name) => name.toLowerCase().endsWith(".pdf"))
  .sort();

let generated = 0;
let failed = 0;

for (const name of pdfs) {
  const id = path.basename(name, path.extname(name));
  let task;
  let doc;
  try {
    const data = new Uint8Array(fs.readFileSync(path.join(pdfsDir, name)));
    task = pdfjsLib.getDocument({
      data,
      useSystemFonts: true,
      useWasm: true,
      useWorkerFetch: true,
      wasmUrl,
      iccUrl,
      isEvalSupported: false,
    });
    doc = await task.promise;
  } catch {
    failed++;
    continue;
  }

  const result = { id, file: name, pages: [] };
  for (let p = 1; p <= doc.numPages; p++) {
    const images = [];
    try {
      const page = await doc.getPage(p);
      const ops = await page.getOperatorList();
      const seen = new Set();
      for (let i = 0; i < ops.fnArray.length; i++) {
        if (ops.fnArray[i] !== pdfjsLib.OPS.paintImageXObject) continue;
        const [objId, width, height] = ops.argsArray[i];
        if (seen.has(objId)) continue;
        seen.add(objId);
        try {
          const img = await decodedImage(page, objId);
          if (!img || !img.data) continue;
          // An inline image has no object reference, and litho lists image
          // XObjects only.
          if (!img.ref) continue;
          // PDF.js draws an image at the size of its /SMask or /Mask when that
          // is larger, so the samples no longer line up with the image's own.
          if (img.width !== width || img.height !== height) continue;
          const sig = signature(img);
          // A buffer short of one complete cell row leaves nothing to
          // compare with.
          if (sig.sig.every((v) => v === null)) continue;
          images.push({
            width: img.width,
            height: img.height,
            ...sig,
          });
        } catch {
          // An image PDF.js could not decode has no golden to compare with.
        }
      }
    } catch {
      // A page PDF.js cannot interpret contributes no images.
    }
    if (images.length > 0) {
      result.pages.push({ page: p, images });
    }
  }
  try {
    await task.destroy();
  } catch {
    // Releasing the document is best effort.
  }

  fs.writeFileSync(path.join(out, `${id}.json`), JSON.stringify(result) + "\n");
  generated++;
  if (generated % 50 === 0) {
    console.log(`  ... ${generated} generated so far`);
  }
}

console.log(`Done. generated=${generated} failed=${failed}`);
