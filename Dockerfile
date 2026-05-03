FROM golang:1.22-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o qrservice ./cmd/main.go

# ---
FROM alpine:3.19

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata libwebp-tools

COPY --from=builder /app/qrservice .
COPY --from=builder /app/migrations ./migrations

RUN mkdir -p /app/storage/qrcodes

EXPOSE 8080

CMD ["./qrservice"]
