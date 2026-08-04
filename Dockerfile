# syntax=docker/dockerfile:1

FROM node:24-alpine AS frontend-build

WORKDIR /src

COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,id=npm,target=/root/.npm \
    npm ci

COPY web/ .
ARG BUILD_TIMESTAMP
ENV VITE_BUILD_TIMESTAMP=${BUILD_TIMESTAMP}
RUN --mount=type=cache,id=npm,target=/root/.npm \
    npm run build

FROM golang:1.26-alpine AS backend-build

WORKDIR /src

COPY server/go.mod server/go.sum ./
RUN --mount=type=cache,id=gomod,target=/go/pkg/mod \
    go mod download

COPY server/ .
ARG TARGETOS=linux
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=
RUN --mount=type=cache,id=gomod,target=/go/pkg/mod \
    --mount=type=cache,id=gobuild,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath \
    -ldflags="-s -w \
      -X 'xlyra/server/internal/version.Version=${VERSION}' \
      -X 'xlyra/server/internal/version.Commit=${COMMIT}' \
      -X 'xlyra/server/internal/version.BuildTime=${BUILD_TIME}'" \
    -o /out/xlyra-server ./cmd/server

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

ENV APP_ENV=production \
    WORKDIR=/data \
    HTTP_HOST=0.0.0.0 \
    HTTP_PORT=5801 \
    STATIC_DIR=/app/dist

EXPOSE 5801
VOLUME ["/data"]

COPY --from=backend-build /out/xlyra-server /app/xlyra-server
COPY --from=frontend-build /src/dist /app/dist

ENTRYPOINT ["/app/xlyra-server"]
