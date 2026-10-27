ARG GO_VERSION=1.27.3
FROM golang:${GO_VERSION} AS builder
ARG VERSION=development

WORKDIR /src
COPY . .
RUN mkdir /output && GOBIN=/output make install VERSION=$VERSION


# Everything the program runs (Node for Dataview, pagefind, wrangler) is in
# the image, so it needs no Docker of its own. Keep the Node, wrangler and
# pagefind versions in step with internal/tools/Dockerfile (a test checks).
FROM node:24.22.1-bookworm-slim
ARG PROGRAM=obsidian-publish-website
RUN npm install -g --no-fund --no-audit --no-update-notifier wrangler@4.149.0 pagefind@1.5.2 \
 && npm cache clean --force
COPY --from=builder /output/${PROGRAM} /usr/local/bin/obsidian-publish-website
ENV OBSIDIAN_PUBLISH_TOOLS_RUNNER=host \
    WRANGLER_SEND_METRICS=false \
    npm_config_update_notifier=false \
    HOME=/tmp
USER node
WORKDIR /work
ENTRYPOINT ["obsidian-publish-website"]
