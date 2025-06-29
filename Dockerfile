# Tahap 1: Builder - Kompilasi kode Go
FROM golang:1.24-alpine AS builder
ENV CGO_ENABLED=0
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -ldflags="-w -s" -o /app/server .

# Tahap 2: Final - Image runtime yang ramping dan aman
FROM alpine:latest
WORKDIR /app
# Membuat user non-root dan direktori penyimpanan
RUN addgroup -S appgroup && adduser -S appuser -G appgroup && \
    mkdir -p /storage && \
    chown appuser:appgroup /storage
# Salin binary dan lisensi
COPY LICENSE .
COPY --from=builder /app/server .
# Setel kepemilikan akhir dan ganti user
RUN chown appuser:appgroup /app/server
USER appuser
# Volume untuk penyimpanan file persisten
VOLUME /storage
# Expose port
EXPOSE 8080
CMD ["./server"]
