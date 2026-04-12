.PHONY: build test fmt clean run

BINARY := paseo-relay

build:
	go build -o $(BINARY) .

test:
	GOPROXY=https://goproxy.cn,direct go test ./... -v -timeout 30s

fmt:
	gofmt -w .

clean:
	rm -f $(BINARY)

run: build
	./$(BINARY)
