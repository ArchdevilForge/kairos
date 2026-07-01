# Multi-stage build: builder → distroless
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /bin/kairosd ./cmd/kairosd

# ---------------------------------------------------------------------------
FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /bin/kairosd /kairosd

ENTRYPOINT ["/kairosd"]
CMD ["--config", "/etc/kairos/config.yaml"]
