# check=skip=InvalidDefaultArgInFrom,FromPlatformFlagConstDisallowed
# Shared corpus provisioning image.
#
# Installs Node (for PDF.js), a Java runtime, veraPDF, pdfcpu, Mustang,
# KoSIT, and baseline utilities. Bakes a built pdf.js checkout into the
# image so per-run provisioning only downloads fixtures and generates
# goldens.
#
# Every version comes from versions.env. `corpus init` passes each entry
# as --build-arg, so no ARG below carries a default that could drift from
# the file. The directive on the first line silences the two build checks
# that design trips: a FROM whose ARGs have no default, and the constant
# platform of the veraPDF stage. To build and run by hand:
#
#   docker build $(go run ./test/integration/cmd/corpus versions --build-args) \
#     -t litho-corpus-builder test/integration/corpus
#   docker run --rm -v $(pwd)/test/integration/testdata/<name>:/output \
#     litho-corpus-builder <name> <profiles_csv>

ARG VERAPDF_VERSION
ARG VERAPDF_IMAGE_DIGEST
ARG NODE_VERSION
ARG NODE_DEBIAN_RELEASE
ARG NODE_IMAGE_DIGEST

# verapdf/cli is published as linux/amd64 only. Pin the platform so the
# build does not emit the InvalidBaseImagePlatform warning on arm64
# hosts. The files copied out are platform-independent (pure Java jars
# plus shell launchers). The tag names the release; the digest, when
# set, pins the one build of it the goldens were made with.
FROM --platform=linux/amd64 verapdf/cli:v${VERAPDF_VERSION}${VERAPDF_IMAGE_DIGEST:+@}${VERAPDF_IMAGE_DIGEST} AS verapdf-dist

FROM node:${NODE_VERSION}-${NODE_DEBIAN_RELEASE}${NODE_IMAGE_DIGEST:+@}${NODE_IMAGE_DIGEST}

RUN apt-get update && apt-get install -y --no-install-recommends \
  git unzip curl ca-certificates parallel jq \
  && rm -rf /var/lib/apt/lists/*

COPY lib/fetch-revision.sh /usr/local/bin/fetch-revision
RUN chmod +x /usr/local/bin/fetch-revision

# Pre-fetch + build pdf.js at the pinned commit so the shared golden
# generator can load it without paying the setup cost on every
# provisioning run. npm ci installs the dependency tree the commit's
# lockfile names.
ARG PDFJS_REVISION
RUN fetch-revision https://github.com/mozilla/pdf.js.git "${PDFJS_REVISION}" /opt/pdfjs \
  && cd /opt/pdfjs && npm ci && npx gulp generic-legacy

# Java runtime for veraPDF, Mustang and KoSIT: a pinned Eclipse Temurin
# JRE, checked against the SHA-256 its release publishes.
ARG TARGETARCH
ARG JRE_VERSION
RUN : "${JRE_VERSION:?build through corpus init, which passes versions.env}" \
  && major="${JRE_VERSION%%.*}" \
  && case "${TARGETARCH}" in \
    amd64) arch=x64 ;; \
    arm64) arch=aarch64 ;; \
    *) echo "no Temurin JRE for architecture ${TARGETARCH}" >&2 && exit 1 ;; \
  esac \
  && file="OpenJDK${major}U-jre_${arch}_linux_hotspot_$(echo "${JRE_VERSION}" | tr + _).tar.gz" \
  && base="https://github.com/adoptium/temurin${major}-binaries/releases/download/jdk-$(echo "${JRE_VERSION}" | sed 's/+/%2B/')" \
  && cd /tmp \
  && curl -fsSLO "${base}/${file}" \
  && curl -fsSLO "${base}/${file}.sha256.txt" \
  && sha256sum -c "${file}.sha256.txt" \
  && mkdir -p /opt/java \
  && tar -xzf "${file}" -C /opt/java --strip-components=1 \
  && rm -f "${file}" "${file}.sha256.txt"
ENV JAVA_HOME=/opt/java
ENV PATH="/opt/java/bin:${PATH}"

# Install pdfcpu CLI: third-party oracle for the bookmarks feature.
# Pinned to a tagged release so corpus runs stay reproducible when
# upstream master shifts the JSON export shape.
ARG PDFCPU_VERSION
RUN : "${PDFCPU_VERSION:?build through corpus init, which passes versions.env}" \
  && curl -fSL -o /tmp/pdfcpu.tar.xz \
  "https://github.com/pdfcpu/pdfcpu/releases/download/v${PDFCPU_VERSION}/pdfcpu_${PDFCPU_VERSION}_Linux_x86_64.tar.xz" \
  && tar -xJf /tmp/pdfcpu.tar.xz -C /tmp \
  && install -m 0755 "/tmp/pdfcpu_${PDFCPU_VERSION}_Linux_x86_64/pdfcpu" /usr/local/bin/pdfcpu \
  && rm -rf /tmp/pdfcpu.tar.xz "/tmp/pdfcpu_${PDFCPU_VERSION}_Linux_x86_64"

# Install Mustang CLI: third-party oracle for the facturx-validity
# feature. Covers the Factur-X / ZUGFeRD PDF wrapper (filename,
# AFRelationship, XMP fx:* properties, extension schema) plus the
# embedded CII XML schema. Pinned to a tagged release for reproducibility.
ARG MUSTANG_VERSION
RUN : "${MUSTANG_VERSION:?build through corpus init, which passes versions.env}" \
  && curl -fSL -o /opt/mustang.jar \
  "https://github.com/ZUGFeRD/mustangproject/releases/download/core-${MUSTANG_VERSION}/Mustang-CLI-${MUSTANG_VERSION}.jar"

# Install KoSIT validator + XRechnung scenarios bundle: third-party
# oracle for the facturx-schematron feature. The XRechnung config
# bundles both XRechnung-specific and generic EN 16931 CII Schematron;
# KoSIT auto-selects the rule set per document via CustomizationID.
# Apache-2.0 for both artefacts. Pinned for reproducibility; bump the
# version pins yearly to track Schematron drift.
ARG KOSIT_VERSION
ARG XRECHNUNG_CONFIG_TAG
ARG XRECHNUNG_CONFIG_ASSET
ARG EN16931_VERSION
RUN : "${KOSIT_VERSION:?build through corpus init, which passes versions.env}" \
  && : "${XRECHNUNG_CONFIG_TAG:?build through corpus init, which passes versions.env}" \
  && : "${XRECHNUNG_CONFIG_ASSET:?build through corpus init, which passes versions.env}" \
  && : "${EN16931_VERSION:?build through corpus init, which passes versions.env}" \
  && curl -fSL -o /opt/kosit.jar \
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

# veraPDF comes last: its launcher runs on the JRE above. The check
# fails the build when the digest pins a build of another release than
# VERAPDF_VERSION names, since a digest overrides the tag it sits next to.
COPY --from=verapdf-dist /opt/verapdf /opt/verapdf
ENV PATH="/opt/verapdf:${PATH}"
ARG VERAPDF_VERSION
RUN found="$(verapdf --version | head -n 1)" \
  && if [ "${found}" != "veraPDF ${VERAPDF_VERSION}" ]; then \
    echo "the veraPDF image carries ${found}, versions.env pins veraPDF ${VERAPDF_VERSION}: fix VERAPDF_IMAGE_DIGEST" >&2 && exit 1; \
  fi

WORKDIR /work
COPY entrypoint.sh ./
COPY lib/ ./lib/
COPY pdfjs/ ./pdfjs/
COPY verapdf/ ./verapdf/
COPY govdocs/ ./govdocs/
COPY pdf20/ ./pdf20/
COPY pdfcpu/ ./pdfcpu/
COPY facturx/ ./facturx/
# The litho corpus ships its fixtures in git rather than downloading them,
# so this COPY carries the PDFs themselves into the image.
COPY litho/ ./litho/
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
  facturx/download.sh \
  litho/download.sh

# Record the versions this image was built with, overrides included, so
# the entrypoint can hand the corpus revisions to the download scripts and
# write them into each corpus's versions.json. versions.env supplies the
# key names only: every value comes from the build arguments, and a key
# with no value (a dropped digest, a local-tool pin) is left out.
ARG VERAPDF_IMAGE_DIGEST
ARG NODE_DEBIAN_RELEASE
ARG NODE_IMAGE_DIGEST
ARG VERAPDF_CORPUS_REVISION
ARG PDF20_REVISION
ARG PDFCPU_CORPUS_REVISION
ARG FACTURX_CORPUS_REVISION
ARG GOVDOCS_THREADS
COPY versions.env /opt/litho/versions.pinned.env
RUN sed -n 's/^\([A-Z][A-Z0-9_]*\)=.*/\1/p' /opt/litho/versions.pinned.env \
  | while read -r key; do \
    value="$(printenv "${key}" || true)"; \
    if [ -n "${value}" ]; then printf '%s=%s\n' "${key}" "${value}"; fi; \
  done > /opt/litho/versions.env \
  && cat /opt/litho/versions.env

ENTRYPOINT ["/work/entrypoint.sh"]
