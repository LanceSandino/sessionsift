# syntax=docker/dockerfile:1.7

FROM node:26.12.1-alpine3.24@sha256:40cd011b55415bc894e45b02fdd5a5dfde056921b7d23a64c74f80497af48797 AS web
WORKDIR /workspace
COPY package.json package-lock.json ./
RUN npm ci
COPY src/web ./src/web
COPY src/webui ./src/webui
RUN npm run build:web

FROM golang:1.27.3-alpine3.24@sha256:b8c9b82affbbb82a020bbcf998dad2ba9516944ac7e883524642a481a9505e9c AS build
WORKDIR /workspace
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY src ./src
COPY --from=web /workspace/src/webui/dist ./src/webui/dist
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/sessionsift ./src/cmd/sessionsift

FROM gcr.io/distroless/static-debian13:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99
WORKDIR /app
COPY --from=build --chown=nonroot:nonroot /out/sessionsift /app/sessionsift
COPY LICENSE NOTICE /app/
USER nonroot:nonroot
ENTRYPOINT ["/app/sessionsift"]
