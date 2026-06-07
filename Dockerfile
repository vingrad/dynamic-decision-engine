# syntax=docker/dockerfile:1

# --- builder ----------------------------------------------------------------
FROM golang:1.25-alpine AS builder
WORKDIR /src

# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown

# Build a static binary (CGO disabled — the pgx driver is pure Go).
ENV CGO_ENABLED=0
RUN go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
    -o /out/dde ./cmd/dde

# --- runtime ----------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder /out/dde /app/dde

EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/dde"]
CMD ["serve"]
