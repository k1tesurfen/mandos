BINARY := mandos
PREFIX := /usr/local

.PHONY: build install uninstall test vet clean

build:
	go build -o $(BINARY) ./cmd/mandos

install: build
	install -d $(PREFIX)/bin
	install -m 0755 $(BINARY) $(PREFIX)/bin/$(BINARY)

uninstall:
	rm -f $(PREFIX)/bin/$(BINARY)

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -f $(BINARY)
