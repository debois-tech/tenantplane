.PHONY: build test fmt verify generate manager-image kind-load install-crd deploy site-serve site-build

IMG ?= tenantplane/manager:dev
KIND_CLUSTER ?= tenantplane-dev

build:
	go build ./cmd/tenantplane

test:
	go test ./...

fmt:
	go fmt ./...

verify: fmt test build

generate:
	go run sigs.k8s.io/controller-tools/cmd/controller-gen object:headerFile="" paths="./internal/api/..."

manager-image:
	docker build -t $(IMG) .

kind-load: manager-image
	kind load docker-image $(IMG) --name $(KIND_CLUSTER)

install-crd:
	kubectl apply -f config/crd

deploy:
	kubectl apply -f deploy/tenantplane.yaml

# Documentation site (requires Hugo extended; see website/README.md).
site-serve:
	cd website && hugo server

site-build:
	cd website && hugo --gc --minify

