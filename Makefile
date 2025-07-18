up:
	docker-compose up -d

down:
	docker-compose down

run:
	go run ./cmd/api

build:
	go build -o bin/rinha ./cmd/api

fmt:
	go fmt ./...

lint:
	golangci-lint run

docker-run:
	docker-compose up --build api