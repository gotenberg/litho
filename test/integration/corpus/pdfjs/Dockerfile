FROM node:24

RUN apt-get update && apt-get install -y --no-install-recommends git && rm -rf /var/lib/apt/lists/*

WORKDIR /work

COPY setup.sh download-pdfjs-pdfs.mjs gen-pdfjs-reference.mjs ./

RUN chmod +x setup.sh

ENTRYPOINT ["./setup.sh"]
