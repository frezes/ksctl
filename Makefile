.PHONY: build test clean

build:
	@mkdir -p bin
	go build -o bin/ksctl ./cmd/ksctl

test:
	go test ./...

clean:
	rm -f bin/ksctl
