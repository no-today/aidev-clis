.PHONY: check build install uninstall crossbuild db-up db-down test-integration hooks
# Install the git pre-commit hook (fast subset of `check`, no crossbuild).
hooks:
	git config core.hooksPath scripts/githooks
	@echo "pre-commit hook enabled (bypass once with: git commit --no-verify)"
check: crossbuild
	gofmt -l . | (! grep .) || (echo "gofmt needed"; exit 1)
	go vet ./...
	go test ./...
	bash scripts/check-isolation.sh

# Integration DBs (mysql + pg) for dbcli real-driver tests.
db-up:
	docker compose -f test/integration/docker-compose.yml up -d --wait
db-down:
	docker compose -f test/integration/docker-compose.yml down -v
# Run the build-tagged integration tests against the running DBs.
test-integration:
	go test -tags=integration ./...
crossbuild:
	GOOS=windows GOARCH=amd64 go build ./...
	GOOS=linux   GOARCH=amd64 go build ./...
	GOOS=darwin  GOARCH=arm64 go build ./...
build:
	go build ./...
install:
	bash scripts/install.sh
uninstall:
	bash scripts/uninstall.sh
