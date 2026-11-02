# syntax=docker/dockerfile:1.7

FROM node:26.13.0-alpine3.24@sha256:c6ad9e9c1d80f6eb607c26b4b0a132ff7a2344fad1c78ea3baa2a7d4c8de6d94 AS web
WORKDIR /workspace
COPY package.json package-lock.json ./
RUN npm ci
COPY src/web ./src/web
COPY src/webui ./src/webui
RUN npm run build:web

FROM golang:1.27.3-alpine3.24@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS build
WORKDIR /workspace
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY src ./src
COPY --from=web /workspace/src/webui/dist ./src/webui/dist
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/sessionsift ./src/cmd/sessionsift

FROM gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329
WORKDIR /app
COPY --from=build --chown=nonroot:nonroot /out/sessionsift /app/sessionsift
COPY LICENSE NOTICE /app/
USER nonroot:nonroot
ENTRYPOINT ["/app/sessionsift"]
