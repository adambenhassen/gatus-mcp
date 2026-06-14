# Build a static binary, then ship it on distroless.
FROM golang:1.26 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /gatus-mcp ./cmd/gatus-mcp

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /gatus-mcp /gatus-mcp

EXPOSE 3000

ENTRYPOINT ["/gatus-mcp"]
