GO ?= go1.26.3
IMAGE_REPO ?= ghcr.io/thameem-abbas/composite-dra-driver
IMAGE_TAG ?= latest

.PHONY: build build-driver build-webhook test image image-driver image-webhook deploy clean

build: build-driver build-webhook

build-driver:
	$(GO) build -o bin/composite-dra-driver ./cmd/driver

build-webhook:
	$(GO) build -o bin/composite-dra-webhook ./cmd/webhook

test:
	$(GO) test ./... -v

image: image-driver image-webhook

image-driver:
	podman build -t $(IMAGE_REPO)/driver:$(IMAGE_TAG) --target driver -f Dockerfile .

image-webhook:
	podman build -t $(IMAGE_REPO)/webhook:$(IMAGE_TAG) --target webhook -f Dockerfile .

deploy:
	kubectl apply -f deploy/namespace.yaml
	kubectl apply -f deploy/rbac.yaml
	kubectl apply -f deploy/configmap.yaml
	kubectl apply -f deploy/deviceclass.yaml
	kubectl apply -f deploy/daemonset.yaml

clean:
	rm -rf bin/

lint:
	$(GO) vet ./...

mod-tidy:
	$(GO) mod tidy
