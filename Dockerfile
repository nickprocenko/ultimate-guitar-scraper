FROM golang:1.21-bookworm AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o ug-server .

FROM debian:bookworm-slim
RUN apt-get update && \
    apt-get install -y --no-install-recommends ffmpeg libchromaprint-tools ca-certificates && \
    rm -rf /var/lib/apt/lists/*
COPY --from=builder /app/ug-server /usr/local/bin/ug-server
EXPOSE 8080
CMD ["/usr/local/bin/ug-server", "serve"]
