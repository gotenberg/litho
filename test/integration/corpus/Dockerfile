# check=skip=InvalidDefaultArgInFrom,FromPlatformFlagConstDisallowed
# Shared corpus provisioning image.
#
# Installs Node (for PDF.js), a Java runtime, veraPDF, pdfcpu, Mustang,
# KoSIT, poppler's pdftoppm, and baseline utilities. Bakes a built pdf.js checkout into the
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
ARG POPPLER_DEBIAN_RELEASE
ARG POPPLER_DEBIAN_IMAGE_DIGEST

# verapdf/cli is published as linux/amd64 only. Pin the platform so the
# build does not emit the InvalidBaseImagePlatform warning on arm64
# hosts. The files copied out are platform-independent (pure Java jars
# plus shell launchers). The tag names the release; the digest, when
# set, pins the one build of it the goldens were made with.
FROM --platform=linux/amd64 verapdf/cli:v${VERAPDF_VERSION}${VERAPDF_IMAGE_DIGEST:+@}${VERAPDF_IMAGE_DIGEST} AS verapdf-dist

# poppler: pdftoppm renders the render goldens. The pinned release needs
# CMake 3.28, a C++23 compiler and FreeType 2.13, newer than the Node
# image's Debian release ships, so it builds on its own Debian release from
# the release tarball, checked against the SHA-256 versions.env pins.
# bundle-poppler packs the binaries with their libraries, loader and fonts,
# so the final stage runs them unchanged. Only the libraries pdftoppm reads
# PDFs with are enabled: no Qt, GLib, C++ wrapper, network, TIFF or
# signature backends. fonts-urw-base35 provides the metric-compatible
# substitutes for the standard 14 fonts (ISO 32000-1, 9.6.2.2), and
# fonts-dejavu-core the fallback for any other unembedded font. poppler-data
# and Noto Sans CJK, installed after the build so a bump of either keeps the
# build's cache, serve the CJK character collections.
FROM debian:${POPPLER_DEBIAN_RELEASE}-slim${POPPLER_DEBIAN_IMAGE_DIGEST:+@}${POPPLER_DEBIAN_IMAGE_DIGEST} AS poppler-build
RUN apt-get update && apt-get install -y --no-install-recommends \
  ca-certificates curl xz-utils cmake g++ make pkg-config \
  libfreetype-dev libfontconfig-dev libjpeg62-turbo-dev libpng-dev \
  libopenjp2-7-dev liblcms2-dev zlib1g-dev \
  fontconfig fonts-urw-base35 fonts-dejavu-core \
  && rm -rf /var/lib/apt/lists/*
ARG POPPLER_VERSION
ARG POPPLER_SHA256
RUN : "${POPPLER_VERSION:?build through corpus init, which passes versions.env}" \
  && : "${POPPLER_SHA256:?build through corpus init, which passes versions.env}" \
  && cd /tmp \
  && curl -fSLO "https://poppler.freedesktop.org/poppler-${POPPLER_VERSION}.tar.xz" \
  && if ! echo "${POPPLER_SHA256}  poppler-${POPPLER_VERSION}.tar.xz" | sha256sum -c -; then \
    echo "poppler-${POPPLER_VERSION}.tar.xz does not match POPPLER_SHA256=${POPPLER_SHA256}" >&2 && exit 1; \
  fi \
  && tar -xJf "poppler-${POPPLER_VERSION}.tar.xz" \
  && cmake -S "poppler-${POPPLER_VERSION}" -B build \
    -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF -DENABLE_UTILS=ON \
    -DENABLE_CPP=OFF -DENABLE_GLIB=OFF -DENABLE_GOBJECT_INTROSPECTION=OFF \
    -DENABLE_QT5=OFF -DENABLE_QT6=OFF -DENABLE_BOOST=OFF -DENABLE_LIBCURL=OFF \
    -DENABLE_LIBTIFF=OFF -DENABLE_NSS3=OFF -DENABLE_GPGME=OFF -DENABLE_HARFBUZZ=OFF \
    -DBUILD_GTK_TESTS=OFF -DBUILD_QT5_TESTS=OFF -DBUILD_QT6_TESTS=OFF \
    -DBUILD_CPP_TESTS=OFF -DBUILD_MANUAL_TESTS=OFF \
    -DPOPPLER_DATADIR=/opt/poppler/share/poppler \
  && cmake --build build -j "$(nproc)" --target pdftoppm pdfinfo
# poppler-data: the CMaps and CID-to-Unicode tables of the Adobe CJK
# character collections, at the POPPLER_DATADIR the build above names, inside
# the bundle. Without them poppler draws no glyph of a font of Adobe-GB1,
# -CNS1, -Japan1 or -Korea1 that has no /ToUnicode, and none of a font whose
# /Encoding is one of their CMaps (ISO 32000-1, 9.7.5.2, Table 118).
ARG POPPLER_DATA_VERSION
ARG POPPLER_DATA_SHA256
RUN : "${POPPLER_DATA_VERSION:?build through corpus init, which passes versions.env}" \
  && : "${POPPLER_DATA_SHA256:?build through corpus init, which passes versions.env}" \
  && cd /tmp \
  && curl -fSLO "https://poppler.freedesktop.org/poppler-data-${POPPLER_DATA_VERSION}.tar.gz" \
  && if ! echo "${POPPLER_DATA_SHA256}  poppler-data-${POPPLER_DATA_VERSION}.tar.gz" | sha256sum -c -; then \
    echo "poppler-data-${POPPLER_DATA_VERSION}.tar.gz does not match POPPLER_DATA_SHA256=${POPPLER_DATA_SHA256}" >&2 && exit 1; \
  fi \
  && tar -xzf "poppler-data-${POPPLER_DATA_VERSION}.tar.gz" \
  && make -C "poppler-data-${POPPLER_DATA_VERSION}" install prefix=/opt/poppler \
  && rm -rf "/tmp/poppler-data-${POPPLER_DATA_VERSION}" "/tmp/poppler-data-${POPPLER_DATA_VERSION}.tar.gz"
# Noto Sans CJK: poppler draws an unembedded font of a CJK collection with
# the first font fontconfig offers that supports the collection's language
# (ja for Adobe-Japan1, ko, zh-cn, zh-tw), and with any font when none does.
# One Regular face per region, the region's subset OpenType font, each
# checked against the SHA-256 versions.env pins. bundle-poppler copies them
# with the other fonts.
ARG NOTO_CJK_SANS_VERSION
ARG NOTO_SANS_JP_SHA256
ARG NOTO_SANS_KR_SHA256
ARG NOTO_SANS_SC_SHA256
ARG NOTO_SANS_TC_SHA256
RUN : "${NOTO_CJK_SANS_VERSION:?build through corpus init, which passes versions.env}" \
  && dir=/usr/share/fonts/opentype/noto-cjk \
  && base="https://github.com/notofonts/noto-cjk/raw/Sans${NOTO_CJK_SANS_VERSION}" \
  && mkdir -p "${dir}" \
  && for spec in "JP ${NOTO_SANS_JP_SHA256}" "KR ${NOTO_SANS_KR_SHA256}" \
    "SC ${NOTO_SANS_SC_SHA256}" "TC ${NOTO_SANS_TC_SHA256}"; do \
    set -- ${spec}; \
    : "${2:?build through corpus init, which passes versions.env}"; \
    file="NotoSans$1-Regular.otf"; \
    curl -fSL -o "${dir}/${file}" "${base}/Sans/SubsetOTF/$1/${file}" || exit 1; \
    if ! echo "$2  ${dir}/${file}" | sha256sum -c -; then \
      echo "${file} does not match NOTO_SANS_$1_SHA256=$2" >&2 && exit 1; \
    fi; \
  done \
  && curl -fSL -o "${dir}/LICENSE" "${base}/LICENSE"
COPY lib/70-noto-cjk.conf /etc/fonts/conf.d/70-noto-cjk.conf
COPY lib/bundle-poppler.sh /usr/local/bin/bundle-poppler
RUN bash /usr/local/bin/bundle-poppler /tmp/build/utils /opt/poppler pdftoppm pdfinfo \
  && rm -rf /tmp/build "/tmp/poppler-${POPPLER_VERSION}" "/tmp/poppler-${POPPLER_VERSION}.tar.xz"

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
# upstream master shifts the JSON export shape. The release publishes one
# Linux archive per architecture: the image installs its own, so an arm64
# host runs pdfcpu natively, and checks it against the SHA-256 versions.env
# pins for that architecture.
ARG PDFCPU_VERSION
ARG PDFCPU_SHA256_AMD64
ARG PDFCPU_SHA256_ARM64
RUN : "${PDFCPU_VERSION:?build through corpus init, which passes versions.env}" \
  && case "${TARGETARCH}" in \
    amd64) arch=x86_64 pin=PDFCPU_SHA256_AMD64 sha256="${PDFCPU_SHA256_AMD64}" ;; \
    arm64) arch=arm64 pin=PDFCPU_SHA256_ARM64 sha256="${PDFCPU_SHA256_ARM64}" ;; \
    *) echo "no pdfcpu archive for architecture ${TARGETARCH}" >&2 && exit 1 ;; \
  esac \
  && : "${sha256:?build through corpus init, which passes versions.env}" \
  && name="pdfcpu_${PDFCPU_VERSION}_Linux_${arch}" \
  && curl -fSL -o /tmp/pdfcpu.tar.xz \
    "https://github.com/pdfcpu/pdfcpu/releases/download/v${PDFCPU_VERSION}/${name}.tar.xz" \
  && if ! echo "${sha256}  /tmp/pdfcpu.tar.xz" | sha256sum -c -; then \
    echo "${name}.tar.xz does not match ${pin}=${sha256}: pin the hash its release lists in checksums.txt" >&2 && exit 1; \
  fi \
  && tar -xJf /tmp/pdfcpu.tar.xz -C /tmp \
  && install -m 0755 "/tmp/${name}/pdfcpu" /usr/local/bin/pdfcpu \
  && rm -rf /tmp/pdfcpu.tar.xz "/tmp/${name}"

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

# poppler's pdftoppm and pdfinfo, from the poppler-build stage.
COPY --from=poppler-build /opt/poppler /opt/poppler
ENV PATH="/opt/poppler/bin:${PATH}"
ARG POPPLER_VERSION
RUN found="$(pdftoppm -v 2>&1 | head -n 1)" \
  && if [ "${found}" != "pdftoppm version ${POPPLER_VERSION}" ]; then \
    echo "the image carries ${found}, versions.env pins poppler ${POPPLER_VERSION}" >&2 && exit 1; \
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
  lib/gen-render.sh \
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
ARG POPPLER_SHA256
ARG POPPLER_DEBIAN_RELEASE
ARG POPPLER_DEBIAN_IMAGE_DIGEST
COPY versions.env /opt/litho/versions.pinned.env
RUN sed -n 's/^\([A-Z][A-Z0-9_]*\)=.*/\1/p' /opt/litho/versions.pinned.env \
  | while read -r key; do \
    value="$(printenv "${key}" || true)"; \
    if [ -n "${value}" ]; then printf '%s=%s\n' "${key}" "${value}"; fi; \
  done > /opt/litho/versions.env \
  && cat /opt/litho/versions.env

ENTRYPOINT ["/work/entrypoint.sh"]
