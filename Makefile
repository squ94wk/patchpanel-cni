IMG ?= patchpanel-cni:dev

.PHONY: build generate lint image e2e

build:
	go build ./...
	GOOS=linux go build ./...

generate:
	go tool controller-gen object paths=./api/... 
	go tool controller-gen crd paths=./api/... output:crd:artifacts:config=charts/patchpanel-cni/templates/crds

lint:
	golangci-lint run ./...
	GOOS=linux golangci-lint run ./...

image:
	docker build -t $(IMG) .

# Creates the kind cluster patchpanel-e2e, runs the e2e tests and deletes the cluster again.
e2e: image
	bash test/e2e/run.sh
