# =============================================================================
# MAFAFD dashboard — Next.js 15 standalone build (non-root)
# =============================================================================

FROM node:24-alpine AS builder

WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund

COPY . .
ENV NEXT_TELEMETRY_DISABLED=1
RUN npm run build

# --- runtime stage ---
FROM node:24-alpine AS runtime

RUN addgroup -S -g 1001 mafafd && \
    adduser -S -D -H -u 1001 -G mafafd mafafd

WORKDIR /app
ENV NODE_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    PORT=3000 \
    HOSTNAME=0.0.0.0

COPY --from=builder --chown=mafafd:mafafd /app/public ./public
COPY --from=builder --chown=mafafd:mafafd /app/.next/standalone ./
COPY --from=builder --chown=mafafd:mafafd /app/.next/static ./.next/static

USER mafafd
EXPOSE 3000

CMD ["node", "server.js"]
