.PHONY: build test vet fmt run-service run-tool docker-build docker-build-arm

build:
	go build ./...

test:
	go test -timeout=300s -count=1 ./...

race:
	go test -race -timeout=420s -count=1 ./...

vet:
	go vet ./...

fmt:
	go fmt ./...

run-service:
	go run ./cmd/wuxiangai

run-tool:
	go run ./cmd/hubctl

docker-build:
	./build_eval_docker.sh lightgovernance-eval linux/amd64

docker-build-arm:
	./build_eval_docker.sh lightgovernance-eval linux/arm64

tidy:
	go mod tidy

measure:
	go run .factory/measure_project.go -root . -enforce \
		-min-prod-lines 2000 -min-prod-files 20 \
		-min-packages 8 -min-test-lines 800 \
		-max-file-lines 600 -forbid-frontend
