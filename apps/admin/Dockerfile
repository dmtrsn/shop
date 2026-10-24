# syntax=docker/dockerfile:1.7

FROM node:22.12-alpine AS dependencies

WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci

FROM node:22.12-alpine AS builder

WORKDIR /app
COPY --from=dependencies /app/node_modules ./node_modules
COPY . .

# The admin app has no base path: it owns / on its own admin origin, and the
# browser calls /api/admin there. Nothing about the staff session is known at
# build time.
RUN npm run build \
    && mkdir -p .next/standalone/.next \
    && cp -R .next/static .next/standalone/.next/static

FROM node:22.12-alpine AS runtime

ENV NODE_ENV=production \
    HOSTNAME=0.0.0.0 \
    PORT=3001

RUN addgroup -S -g 10001 admin \
    && adduser -S -D -H -u 10001 -G admin admin

WORKDIR /app
COPY --from=builder --chown=admin:admin /app/.next/standalone ./

USER admin

EXPOSE 3001

HEALTHCHECK --interval=30s --timeout=3s --start-period=15s --retries=3 \
    CMD node -e "fetch('http://127.0.0.1:' + process.env.PORT + '/healthz').then((r) => process.exit(r.ok ? 0 : 1)).catch(() => process.exit(1))"

CMD ["node", "server.js"]
