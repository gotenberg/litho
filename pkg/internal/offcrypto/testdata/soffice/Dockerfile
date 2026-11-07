# The pinned Gotenberg image's LibreOffice with the Python UNO bridge, which
# soffice --convert-to lacks to set a password. python3-uno is pinned to the
# image's LibreOffice build.
ARG GOTENBERG_IMAGE=gotenberg/gotenberg:9.0.0
FROM ${GOTENBERG_IMAGE}
USER root
RUN echo "deb http://deb.debian.org/debian trixie-backports main" > /etc/apt/sources.list.d/backports.list \
  && apt-get update -qq \
  && apt-get install -y -qq --no-install-recommends "python3-uno=$(dpkg-query -W -f='${Version}' libreoffice-core)" \
  && rm -rf /var/lib/apt/lists/*
