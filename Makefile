GO ?= go

.PHONY: all build test vet fmt appimage clean run e2e

all: vet test build

build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w" -o build/swl .

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

appimage:
	./build-appimage.sh

# Mode interactif, pour coller une liste de jeux.
run: build
	./build/swl

# Vérification de bout en bout dans un faux HOME Steam, sans toucher au vrai.
e2e: build
	./scripts/e2e.sh

clean:
	rm -rf build
