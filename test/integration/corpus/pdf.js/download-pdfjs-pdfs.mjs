#!/usr/bin/env node
/**
 * download-pdfjs-pdfs.mjs
 *
 * Parallel downloader for pdf.js test PDFs. Bypasses test.mjs which hangs on
 * MD5 mismatches (pdf.js bug #19580).
 *
 * Must be run from the pdf.js test/ directory (where test_manifest.json and
 * the pdfs/ directory with .link sidecar files live).
 *
 * Features:
 *   - Concurrent downloads (default 8)
 *   - 30s per-file timeout to prevent hangs from dead links
 *   - MD5 verification: files with mismatched checksums are deleted
 *
 * Usage:
 *   node scripts/download-pdfjs-pdfs.mjs --test-dir tmp/pdf.js/test
 */

import crypto from "crypto";
import fs from "fs";
import http from "http";
import https from "https";
import path from "path";
// CLI args

const args = process.argv.slice(2);
let testDir = "";

for (let i = 0; i < args.length; i++) {
  if (args[i] === "--test-dir") {
    testDir = args[++i];
  }
}

if (!testDir) {
  console.error("Usage: node download-pdfjs-pdfs.mjs --test-dir <pdf.js/test>");
  process.exit(1);
}
// Constants

const CONCURRENCY = 8;
const TIMEOUT_MS = 30_000;
const MAX_REDIRECTS = 10;
// Read manifest

const manifestPath = path.join(testDir, "test_manifest.json");
const manifest = JSON.parse(fs.readFileSync(manifestPath, "utf-8"));

// Collect files that need downloading: link flag set and file missing.
// Also collect expected MD5 per file for post-download verification.
const seen = new Set();
const md5Map = new Map();
const toDownload = [];

for (const entry of manifest) {
  if (entry.md5) md5Map.set(entry.file, entry.md5);
  if (!entry.link || seen.has(entry.file)) continue;
  seen.add(entry.file);

  const filePath = path.join(testDir, entry.file);
  if (fs.existsSync(filePath)) continue;

  const linkFile = filePath + ".link";
  if (!fs.existsSync(linkFile)) continue;

  const url = fs.readFileSync(linkFile, "utf-8").trim();
  if (url) toDownload.push({ file: entry.file, filePath, url });
}

console.log(
  `  ${toDownload.length} files to download (${CONCURRENCY} concurrent)...`,
);
// Download helpers

function download(url, dest, redirects = 0) {
  return new Promise((resolve, reject) => {
    if (redirects > MAX_REDIRECTS) {
      return reject(new Error("too many redirects"));
    }
    const mod = url.startsWith("https") ? https : http;
    const req = mod.get(url, { timeout: TIMEOUT_MS }, (res) => {
      if (
        res.statusCode >= 300 &&
        res.statusCode < 400 &&
        res.headers.location
      ) {
        res.resume();
        return resolve(download(res.headers.location, dest, redirects + 1));
      }
      if (res.statusCode !== 200) {
        res.resume();
        return reject(new Error(`HTTP ${res.statusCode}`));
      }
      const dir = path.dirname(dest);
      if (!fs.existsSync(dir)) fs.mkdirSync(dir, { recursive: true });
      const ws = fs.createWriteStream(dest);
      res.pipe(ws);
      ws.on("finish", () => resolve());
      ws.on("error", reject);
    });
    req.on("timeout", () => {
      req.destroy();
      reject(new Error("timeout"));
    });
    req.on("error", reject);
  });
}

function md5File(filePath) {
  return new Promise((resolve, reject) => {
    const hash = crypto.createHash("md5");
    const rs = fs.createReadStream(filePath);
    rs.on("data", (chunk) => hash.update(chunk));
    rs.on("end", () => resolve(hash.digest("hex")));
    rs.on("error", reject);
  });
}
// Concurrency-limited pool

let ok = 0;
let fail = 0;
let md5Fail = 0;
let idx = 0;

async function worker() {
  while (idx < toDownload.length) {
    const { file, filePath, url } = toDownload[idx++];
    try {
      await download(url, filePath);
      // Verify MD5 if the manifest specifies one: delete on mismatch.
      const expected = md5Map.get(file);
      if (expected) {
        const actual = await md5File(filePath);
        if (actual !== expected) {
          fs.unlinkSync(filePath);
          md5Fail++;
          continue;
        }
      }
      ok++;
    } catch {
      fail++;
    }
    if ((ok + fail + md5Fail) % 50 === 0) {
      console.log(`  ... ${ok + fail + md5Fail}/${toDownload.length}`);
    }
  }
}

await Promise.all(Array.from({ length: CONCURRENCY }, () => worker()));
console.log(
  `  downloads complete: ${ok} ok, ${fail} failed, ${md5Fail} md5 mismatch (deleted)`,
);
