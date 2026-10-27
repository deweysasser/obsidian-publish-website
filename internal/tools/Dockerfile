# Pinned publish tooling (wrangler, pagefind), so they run in a container
# instead of on the host and aren't re-downloaded on every publish.
# Embedded in the binary; built on first use as obsidian-publish-tools:<hash>.
FROM node:24.22.1-bookworm-slim
RUN npm install -g --no-fund --no-audit --no-update-notifier wrangler@4.149.0 pagefind@1.5.2 \
 && npm cache clean --force
ENV WRANGLER_SEND_METRICS=false npm_config_update_notifier=false
USER node
WORKDIR /home/node
