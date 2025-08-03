FROM golang:1.24-alpine AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /main ./cmd/api

FROM alpine:3.19

WORKDIR /

COPY --from=build /main /main

EXPOSE 8080

USER nobody:nogroup

ENTRYPOINT ["/main"] 