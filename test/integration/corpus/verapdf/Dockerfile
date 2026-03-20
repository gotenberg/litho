FROM verapdf/cli:latest

USER root
RUN apk add --no-cache git bash

ENV PATH="/opt/verapdf:${PATH}"

WORKDIR /work
COPY setup.sh ./
RUN chmod +x setup.sh

ENTRYPOINT ["bash", "/work/setup.sh"]
