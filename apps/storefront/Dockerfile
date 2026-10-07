# syntax=docker/dockerfile:1.7

FROM node:22.12-alpine AS dependencies

WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci

FROM node:22.12-alpine AS builder

WORKDIR /app
COPY --from=dependencies /app/node_modules ./node_modules
COPY . .

# basePath is compiled into Next route and asset URLs. The browser API stays /api.
ARG STOREFRONT_BASE_PATH=/storefront-preview
ENV STOREFRONT_BASE_PATH=$STOREFRONT_BASE_PATH
RUN npm run build \
    && mkdir -p .next/standalone/.next \
    && cp -R .next/static .next/standalone/.next/static

FROM node:22.12-alpine AS runtime

ARG STOREFRONT_BASE_PATH=/storefront-preview
ENV NODE_ENV=production \
    HOSTNAME=0.0.0.0 \
    PORT=3000 \
    STOREFRONT_BASE_PATH=$STOREFRONT_BASE_PATH

RUN addgroup -S -g 10001 storefront \
    && adduser -S -D -H -u 10001 -G storefront storefront

WORKDIR /app
COPY --from=builder --chown=storefront:storefront /app/.next/standalone ./

USER storefront

EXPOSE 3000

HEALTHCHECK --interval=30s --timeout=3s --start-period=15s --retries=3 \
    CMD node -e "fetch('http://127.0.0.1:' + process.env.PORT + process.env.STOREFRONT_BASE_PATH + '/healthz').then((r) => process.exit(r.ok ? 0 : 1)).catch(() => process.exit(1))"

CMD ["node", "server.js"]
