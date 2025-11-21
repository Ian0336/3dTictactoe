# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o backend_go .

# Run stage
FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/backend_go .

EXPOSE 4000

CMD ["./backend_go"]
