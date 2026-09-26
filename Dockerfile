FROM golang:1.27.1-alpine AS builder

ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build \
    -ldflags="-s -w \
      -X git.golder.lan/rossgolderltd/debian-repo/internal/version.Version=${VERSION} \
      -X git.golder.lan/rossgolderltd/debian-repo/internal/version.Commit=${COMMIT} \
      -X git.golder.lan/rossgolderltd/debian-repo/internal/version.Date=${DATE}" \
    -o /debian-repo ./cmd/debian-repo

FROM alpine:3.24

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /debian-repo /usr/local/bin/debian-repo

USER 65534:65534
EXPOSE 5080 9508

HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
    CMD ["/usr/local/bin/debian-repo", "-healthcheck"]

ENTRYPOINT ["/usr/local/bin/debian-repo"]
CMD ["--config", "/etc/debian-repo/config.yaml"]
