# syntax=docker/dockerfile:1

# Build stage: the Go toolchain compiles a static binary and stays behind.
# It runs on the build machine's platform and cross-compiles, so building for another one needs no emulation.
FROM --platform=$BUILDPLATFORM golang:1.25.3-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src

# Dependencies first: this layer stays cached until go.mod or go.sum change.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/job-service ./cmd/api

# Runtime stage: no shell or package manager, only CA certificates, time zone data and a nonroot user.
FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/job-service /job-service
EXPOSE 8081
USER nonroot:nonroot
ENTRYPOINT ["/job-service"]
