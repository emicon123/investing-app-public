# syntax=docker/dockerfile:1
# Build context is the project root.

# ── Stage 1: Build React SPA ────────────────────────────────────────────────────
FROM node:24-alpine AS builder
WORKDIR /app

# Build-time Google OAuth client ID baked into the Vite bundle
ARG VITE_GOOGLE_CLIENT_ID
ARG VITE_API_BASE_URL
ENV VITE_GOOGLE_CLIENT_ID=${VITE_GOOGLE_CLIENT_ID}
ENV VITE_API_BASE_URL=${VITE_API_BASE_URL}

COPY package.json package-lock.json* ./
COPY packages/common/ts/package.json ./packages/common/ts/
COPY packages/common/ui/package.json ./packages/common/ui/
COPY apps/portfolio-b/frontend-web/package.json ./apps/portfolio-b/frontend-web/

RUN npm install

COPY packages/common/ ./packages/common/
COPY apps/portfolio-b/frontend-web/ ./apps/portfolio-b/frontend-web/

WORKDIR /app/apps/portfolio-b/frontend-web
RUN npm run build

# ── Stage 2: Serve with Nginx ───────────────────────────────────────────────────
FROM nginx:1.31-alpine
COPY --from=builder /app/apps/portfolio-b/frontend-web/dist /usr/share/nginx/html
COPY apps/portfolio-b/infra/nginx/nginx.conf /etc/nginx/conf.d/default.conf
