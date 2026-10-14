.PHONY: build frontend run dev clean

# Build the React frontend, then the Go binary that embeds it.
build: frontend
	go build -o short-report .

frontend:
	cd web && npm install && npm run build

run: build
	./short-report

# Two-terminal live-reload workflow:
#   1) go run .                (backend on :8787)
#   2) cd web && npm run dev   (frontend on :5173, hot reload, proxies /api)
dev:
	@echo "Run these in two separate terminals:"
	@echo "  1) go run ."
	@echo "  2) cd web && npm run dev"

clean:
	rm -rf web/dist web/node_modules short-report
