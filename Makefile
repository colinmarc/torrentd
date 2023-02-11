	SOURCES = $(shell find . -name '*.go')

all: build/torrentd

pb/api.pb.go: pb/api.proto
	go build -o build/protoc-gen-go google.golang.org/protobuf/cmd/protoc-gen-go
	protoc --plugin=build/protoc-gen-go --go_out=. --go_opt=paths=source_relative $<

build/torrentd: pb/api.pb.go $(SOURCES)
	go build -o build/torrentd ./cmd/torrentd

build/dump: pb/api.pb.go $(SOURCES)
	go build -o build/dump ./cmd/dump