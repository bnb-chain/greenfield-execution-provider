BUILD_TAGS = netgo
PACKAGES=$(shell go list ./...)

build_executor:
ifeq ($(OS),Windows_NT)
	go build $(BUILD_FLAGS) -o build/executor.exe cmd/executor/main.go
else
	go build $(BUILD_FLAGS) -o build/executor cmd/executor/main.go
endif

build_observer:
ifeq ($(OS),Windows_NT)
	go build $(BUILD_FLAGS) -o build/observer.exe cmd/observer/main.go
else
	go build $(BUILD_FLAGS) -o build/observer cmd/observer/main.go
endif

build_sender:
ifeq ($(OS),Windows_NT)
	go build $(BUILD_FLAGS) -o build/sender.exe cmd/sender/main.go
else
	go build $(BUILD_FLAGS) -o build/sender cmd/sender/main.go
endif

local_up:
	bash +x ./deployment/local_up.sh

.PHONY: build_executor build_observer build_sender
