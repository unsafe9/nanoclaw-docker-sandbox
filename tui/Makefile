.PHONY: build mock clean run run-mock

BINARY = nanoclaw-tui
MOCK = mock-server

build:
	go build -o $(BINARY) .

mock:
	go build -o $(MOCK) ./cmd/mock-server/

run: build
	./$(BINARY)

run-mock: mock
	./$(MOCK)

clean:
	rm -f $(BINARY) $(MOCK)
