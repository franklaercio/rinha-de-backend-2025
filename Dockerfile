FROM golang:1.21-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o /bin/app ./cmd/api

FROM alpine:latest

COPY --from=builder /bin/app /app

EXPOSE 8080

ENTRYPOINT ["/app"]