export GOWORK := off

default: build

build:
	go build -o terraform-provider-devin

install: build
	mkdir -p ~/.terraform.d/plugins/registry.terraform.io/cognitionai/devin/0.1.0/$$(go env GOOS)_$$(go env GOARCH)
	cp terraform-provider-devin ~/.terraform.d/plugins/registry.terraform.io/cognitionai/devin/0.1.0/$$(go env GOOS)_$$(go env GOARCH)/

test:
	go test ./... -v

lint:
	golangci-lint run ./...

docs:
	cd tools && go generate ./...

docs-check: docs
	@if [ -n "$$(git status --porcelain docs)" ]; then \
		echo "docs/ is out of date; run 'make docs' and commit the result:"; \
		git status --porcelain docs; \
		git diff docs; \
		exit 1; \
	fi

.PHONY: build install test lint docs docs-check
