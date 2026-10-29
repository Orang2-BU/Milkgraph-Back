FROM golang:1.24.4-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/milkgraph ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/milkgraph /milkgraph
USER 65532:65532
ENV BIND_ADDRESS=:8080
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=3s --retries=3 CMD ["/milkgraph", "--healthcheck", "/readyz"]
ENTRYPOINT ["/milkgraph"]
