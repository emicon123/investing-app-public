# syntax=docker/dockerfile:1
# Build context is the project root (set in infra/compose.yml).

# ── Stage 1: Build React SPA ────────────────────────────────────────────────────
FROM node:24-alpine AS builder
WORKDIR /app

# Build-time Google OAuth client ID baked into the Vite bundle
ARG VITE_GOOGLE_CLIENT_ID
ARG VITE_API_BASE_URL
ENV VITE_GOOGLE_CLIENT_ID=${VITE_GOOGLE_CLIENT_ID}
ENV VITE_API_BASE_URL=${VITE_API_BASE_URL}

# Copy workspace manifests first so npm install is cached until they change
COPY package.json package-lock.json* ./
COPY packages/common/ts/package.json ./packages/common/ts/
COPY packages/common/ui/package.json ./packages/common/ui/
COPY apps/main/frontend-web/package.json ./apps/main/frontend-web/

RUN npm install

# Copy source after deps are installed
COPY packages/common/ ./packages/common/
COPY apps/main/frontend-web/ ./apps/main/frontend-web/

WORKDIR /app/apps/main/frontend-web
RUN npm run build

# ── Stage 2: Serve with Nginx ───────────────────────────────────────────────────
FROM nginx:1.31-alpine
COPY --from=builder /app/apps/main/frontend-web/dist /usr/share/nginx/html
COPY infra/nginx/limits.conf /etc/nginx/conf.d/00-limits.conf
COPY infra/nginx/nginx.conf /etc/nginx/conf.d/default.conf
