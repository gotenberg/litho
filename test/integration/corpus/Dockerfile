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
  git unzip curl ca-certificates default-jre-headless parallel jq \
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

# Install Mustang CLI: third-party oracle for the facturx-validity
# feature. Covers the Factur-X / ZUGFeRD PDF wrapper (filename,
# AFRelationship, XMP fx:* properties, extension schema) plus the
# embedded CII XML schema. Pinned to a tagged release for reproducibility.
ARG MUSTANG_VERSION=2.13.0
RUN curl -fSL -o /opt/mustang.jar \
  "https://github.com/ZUGFeRD/mustangproject/releases/download/V${MUSTANG_VERSION}/Mustang-CLI-${MUSTANG_VERSION}.jar"

# Install KoSIT validator + XRechnung scenarios bundle: third-party
# oracle for the facturx-schematron feature. The XRechnung config
# bundles both XRechnung-specific and generic EN 16931 CII Schematron;
# KoSIT auto-selects the rule set per document via CustomizationID.
# Apache-2.0 for both artefacts. Pinned for reproducibility; bump the
# version pins yearly to track Schematron drift.
ARG KOSIT_VERSION=1.5.0
ARG XRECHNUNG_CONFIG_VERSION=2.1.0
ARG XRECHNUNG_CONFIG_RELEASE=2024-06-20
RUN curl -fSL -o /opt/kosit.jar \
  "https://github.com/itplr-kosit/validator/releases/download/v${KOSIT_VERSION}/validationtool-${KOSIT_VERSION}-standalone.jar" \
  && curl -fSL -o /tmp/xrechnung-config.zip \
    "https://github.com/itplr-kosit/validator-configuration-xrechnung/releases/download/release_${XRECHNUNG_CONFIG_VERSION}/validator-configuration-xrechnung_${XRECHNUNG_CONFIG_VERSION}_${XRECHNUNG_CONFIG_RELEASE}.zip" \
  && mkdir -p /opt/kosit-config \
  && unzip -q /tmp/xrechnung-config.zip -d /opt/kosit-config \
  && rm /tmp/xrechnung-config.zip

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
  lib/run-mustang.sh \
  lib/run-kosit.sh \
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
