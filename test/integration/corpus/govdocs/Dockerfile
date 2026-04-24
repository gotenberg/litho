FROM verapdf/cli:latest AS verapdf-dist

FROM node:24

RUN apt-get update && apt-get install -y --no-install-recommends \
  git unzip curl ca-certificates default-jre-headless parallel \
  && rm -rf /var/lib/apt/lists/*

# Graft the VeraPDF distribution from its official image. Avoids
# re-running the interactive IzPack installer during our build.
COPY --from=verapdf-dist /opt/verapdf /opt/verapdf
ENV PATH="/opt/verapdf:${PATH}"

WORKDIR /work
COPY setup.sh download-threads.sh gen-references.mjs run-verapdf.sh ./
RUN chmod +x setup.sh download-threads.sh run-verapdf.sh

ENTRYPOINT ["./setup.sh"]
