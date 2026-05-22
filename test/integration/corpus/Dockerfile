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
ARG MUSTANG_VERSION=2.23.0
RUN curl -fSL -o /opt/mustang.jar \
  "https://github.com/ZUGFeRD/mustangproject/releases/download/core-${MUSTANG_VERSION}/Mustang-CLI-${MUSTANG_VERSION}.jar"

# Install KoSIT validator + XRechnung scenarios bundle: third-party
# oracle for the facturx-schematron feature. The XRechnung config
# bundles both XRechnung-specific and generic EN 16931 CII Schematron;
# KoSIT auto-selects the rule set per document via CustomizationID.
# Apache-2.0 for both artefacts. Pinned for reproducibility; bump the
# version pins yearly to track Schematron drift.
ARG KOSIT_VERSION=1.6.2
ARG XRECHNUNG_CONFIG_TAG=v2026-01-31
ARG XRECHNUNG_CONFIG_ASSET=xrechnung-3.0.2-validator-configuration-2026-01-31.zip
ARG EN16931_VERSION=1.3.16
RUN curl -fSL -o /opt/kosit.jar \
  "https://github.com/itplr-kosit/validator/releases/download/v${KOSIT_VERSION}/validator-${KOSIT_VERSION}-standalone.jar" \
  && curl -fSL -o /tmp/xrechnung-config.zip \
    "https://github.com/itplr-kosit/validator-configuration-xrechnung/releases/download/${XRECHNUNG_CONFIG_TAG}/${XRECHNUNG_CONFIG_ASSET}" \
  && mkdir -p /opt/kosit-config \
  && unzip -q /tmp/xrechnung-config.zip -d /opt/kosit-config \
  && rm /tmp/xrechnung-config.zip \
  # Swap KoSIT's bundled EN 16931 CII XSLT (v1.3.15 in the
  # 2026-01-31 release) for the v1.3.16 release litho's
  # bundled schematron also tracks. Keeps the matrix comparing
  # the same rule semantics on both sides; without the swap,
  # KoSIT applies the older rule list and disagreement reads
  # as a litho coverage gap instead of as version drift.
  && curl -fSL -o /tmp/en16931-cii.zip \
    "https://github.com/ConnectingEurope/eInvoicing-EN16931/releases/download/validation-${EN16931_VERSION}/en16931-cii-${EN16931_VERSION}.zip" \
  && unzip -p /tmp/en16931-cii.zip "xslt/EN16931-CII-validation.xslt" \
    > /opt/kosit-config/resources/cii/16b/xsl/EN16931-CII-validation.xsl \
  && rm /tmp/en16931-cii.zip

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
