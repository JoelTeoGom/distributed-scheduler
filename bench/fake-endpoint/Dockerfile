# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.25.3-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/fake-endpoint .

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/fake-endpoint /fake-endpoint
USER nonroot:nonroot
ENTRYPOINT ["/fake-endpoint"]
