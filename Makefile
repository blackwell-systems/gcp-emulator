BINARY     := gcp-emulator
BUILD_DIR  := bin
IMAGE      := ghcr.io/blackwell-systems/gcp-emulator

.PHONY: build run docker-build docker-run test fmt fmt-check clean

build:
	go build -o $(BUILD_DIR)/$(BINARY) ./cmd/$(BINARY)

run:
	go run ./cmd/$(BINARY)

run-strict:
	IAM_MODE=strict go run ./cmd/$(BINARY) --policy policy.yaml.example

test:
	go test ./...

fmt:
	find . -name '*.go' | xargs gofmt -w

fmt-check:
	@unformatted=$$(find . -name '*.go' | xargs gofmt -l); \
	if [ -n "$$unformatted" ]; then \
		echo "Unformatted files:"; echo "$$unformatted"; exit 1; \
	fi

docker-build:
	docker build -t $(IMAGE):latest .

docker-run:
	docker run -p 9090:9090 -p 8090:8090 $(IMAGE):latest

clean:
	rm -rf $(BUILD_DIR)
