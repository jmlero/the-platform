# Build context must contain a Git archive under baseline/ plus these pilot files.
ARG WORKER_IMAGE=localhost/the-platform/marina-worker@sha256:debcd4ed9692b5a960b9b48fa7fb9c11672c75e9ec8f57460c99eb441070d2bd
FROM ${WORKER_IMAGE}
USER 0:0
WORKDIR /opt/context-lens/dependencies
COPY baseline/package.json baseline/package-lock.json ./
# Downloads are confined to image setup. Runtime checks never run npm install.
RUN npm ci --no-audit --no-fund && npm cache clean --force
COPY baseline /opt/context-lens/baseline
COPY trusted /opt/context-lens/trusted
COPY check.sh /opt/context-lens/check
RUN chmod 0555 /opt/context-lens/check && chmod -R a+rX /opt/context-lens
USER 65532:65532
WORKDIR /work
