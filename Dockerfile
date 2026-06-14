# Build a static binary, then ship it on distroless.
# Builds on the native build platform and cross-compiles to the target arch.
FROM --platform=$BUILDPLATFORM golang:1.26 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS TARGETARCH VERSION=dev
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w -X main.version=${VERSION}" -o /gatus-mcp ./cmd/gatus-mcp

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /gatus-mcp /gatus-mcp

EXPOSE 3000

ENTRYPOINT ["/gatus-mcp"]
