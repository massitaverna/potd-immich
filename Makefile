BINARY  := bin/potd-immich
IMAGE   := ghcr.io/massitaverna/potd-immich
# Release tags are vX.Y.Z; the binary and image report X.Y.Z, like the CI build.
VERSION ?= $(patsubst v%,%,$(shell git describe --tags --always --dirty 2>/dev/null || echo dev))

.PHONY: test build image clean

test:
	go vet ./...
	go test ./...

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o $(BINARY) .

image:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE):$(VERSION) .

clean:
	rm -rf bin
