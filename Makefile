.PHONY: build test fmt vet clean run

BINARY := paseo-relay

build:
	go build -o $(BINARY) .

test:
	go test ./... -v -timeout 30s

fmt:
	gofmt -w .

vet:
	go vet ./...

clean:
	rm -f $(BINARY)

run: build
	./$(BINARY)
