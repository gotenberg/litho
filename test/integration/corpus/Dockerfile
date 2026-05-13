# Shared corpus provisioning image.
#
# Installs Node (for PDF.js), VeraPDF, and baseline utilities. Bakes a
# built pdf.js clone into the image so per-run provisioning only
# downloads fixtures and generates goldens.
#
# Usage:
#   docker build -t litho-corpus-builder test/integration/corpus
#   docker run --rm -v $(pwd)/test/integration/testdata/<name>:/output \
#     litho-corpus-builder <name> <profiles_csv>

# verapdf/cli is published as linux/amd64 only. Pin the platform so the
# build does not emit the InvalidBaseImagePlatform warning on arm64
# hosts. The files copied out are platform-independent (pure Java jars
# plus shell launchers).
FROM --platform=linux/amd64 verapdf/cli:latest AS verapdf-dist

FROM node:24

RUN apt-get update && apt-get install -y --no-install-recommends \
  git unzip curl ca-certificates default-jre-headless parallel \
  && rm -rf /var/lib/apt/lists/*

COPY --from=verapdf-dist /opt/verapdf /opt/verapdf
ENV PATH="/opt/verapdf:${PATH}"

# Pre-clone + build pdf.js so the shared golden generator can load it
# without paying the setup cost on every provisioning run.
RUN git clone --depth 1 https://github.com/mozilla/pdf.js.git /opt/pdfjs \
  && cd /opt/pdfjs && npm install && npx gulp generic-legacy

# Install pdfcpu CLI: third-party oracle for the bookmarks feature.
# Pinned to a tagged release so corpus runs stay reproducible when
# upstream master shifts the JSON export shape.
ARG PDFCPU_VERSION=0.12.1
RUN curl -fSL -o /tmp/pdfcpu.tar.xz \
  "https://github.com/pdfcpu/pdfcpu/releases/download/v${PDFCPU_VERSION}/pdfcpu_${PDFCPU_VERSION}_Linux_x86_64.tar.xz" \
  && tar -xJf /tmp/pdfcpu.tar.xz -C /tmp \
  && install -m 0755 "/tmp/pdfcpu_${PDFCPU_VERSION}_Linux_x86_64/pdfcpu" /usr/local/bin/pdfcpu \
  && rm -rf /tmp/pdfcpu.tar.xz "/tmp/pdfcpu_${PDFCPU_VERSION}_Linux_x86_64"

WORKDIR /work
COPY entrypoint.sh ./
COPY lib/ ./lib/
COPY pdfjs/ ./pdfjs/
COPY verapdf/ ./verapdf/
COPY govdocs/ ./govdocs/
COPY pdf20/ ./pdf20/
COPY pdfcpu/ ./pdfcpu/
COPY facturx/ ./facturx/
RUN chmod +x entrypoint.sh \
  lib/run-verapdf.sh \
  lib/gen-bookmarks.sh \
  lib/gen-merges.sh \
  lib/gen-merge-empty.sh \
  pdfjs/download.sh \
  verapdf/download.sh \
  govdocs/download.sh \
  pdf20/download.sh \
  pdfcpu/download.sh \
  facturx/download.sh

ENTRYPOINT ["/work/entrypoint.sh"]
