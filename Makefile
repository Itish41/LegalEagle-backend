.DEFAULT_GOAL:=run
.PHONY=fmt vet tidy build run
fmt:
	go fmt ./...
vet: fmt
	go vet ./...
tidy: vet
	go mod tidy
build: tidy
	go build
run: build
	go run main.go
