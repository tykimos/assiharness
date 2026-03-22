BINARY := assiharness
MODULE := github.com/tykimos/assiharness
GO := go

.PHONY: build test clean vet build-all

build:
	$(GO) build -o $(BINARY) ./cmd/assiharness

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

clean:
	rm -f $(BINARY)
	rm -f $(BINARY)-*

build-all:
	GOOS=linux GOARCH=amd64 $(GO) build -o $(BINARY)-linux-amd64 ./cmd/assiharness
	GOOS=darwin GOARCH=arm64 $(GO) build -o $(BINARY)-darwin-arm64 ./cmd/assiharness
	GOOS=windows GOARCH=amd64 $(GO) build -o $(BINARY)-windows-amd64.exe ./cmd/assiharness
