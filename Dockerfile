# syntax=docker/dockerfile:1.7

FROM node:22-alpine AS web
WORKDIR /workspace
COPY package.json package-lock.json ./
RUN npm ci
COPY src/web ./src/web
COPY src/webui ./src/webui
RUN npm run build:web

FROM golang:1.27-alpine AS build
WORKDIR /workspace
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY src ./src
COPY --from=web /workspace/src/webui/dist ./src/webui/dist
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/sessionsift ./src/cmd/sessionsift

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build --chown=nonroot:nonroot /out/sessionsift /app/sessionsift
USER nonroot:nonroot
ENTRYPOINT ["/app/sessionsift"]
