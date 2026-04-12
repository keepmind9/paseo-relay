# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o paseo-relay .

# Runtime stage
FROM alpine:3.19

RUN apk --no-cache add ca-certificates
COPY --from=builder /app/paseo-relay /usr/local/bin/paseo-relay

EXPOSE 8080
ENTRYPOINT ["paseo-relay"]
