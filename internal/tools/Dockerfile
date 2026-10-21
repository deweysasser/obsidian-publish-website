# Pinned publish tooling (wrangler, pagefind), so they run in a container
# instead of on the host and aren't re-downloaded on every publish.
# Embedded in the binary; built on first use as obsidian-publish-tools:<hash>.
FROM node:22.11.0-bookworm-slim
RUN npm install -g --no-fund --no-audit --no-update-notifier wrangler@3.90.0 pagefind@1.3.0 \
 && npm cache clean --force
ENV WRANGLER_SEND_METRICS=false npm_config_update_notifier=false
USER node
WORKDIR /home/node
