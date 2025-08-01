FROM golang:1.24-alpine AS builder

ENV CGO_ENABLED=0

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -ldflags="-w -s" -o /bin/app ./cmd/api

FROM scratch

COPY --from=builder /bin/app /app

EXPOSE 8080

ENTRYPOINT ["/app"]