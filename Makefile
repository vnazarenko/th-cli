# Makefile for the `th-cli` CLI (trendHERO public API client).
#
# Common targets:
#   make build          build ./th-cli for the host platform
#   make test           run the full test suite
#   make lint           go vet (+ golangci-lint if installed)
#   make generate       regenerate the oapi-codegen client from the spec
#   make install-skill  symlink skills/th-cli into ~/.claude/skills/
#   make release        cross-compile release binaries into dist/
#   make clean          remove build/test artifacts

BINARY      := th-cli
PKG         := github.com/vnazarenko/th-cli
DIST        := dist
SKILL_NAME  := th-cli
SKILLS_DIR  := $(HOME)/.claude/skills

# Version metadata, injected into the binary via -ldflags so `th-cli version` (and
# the User-Agent header) report real build info. Both vars live in internal/api
# — a single source of truth — so we stamp them there.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
VERSION_PKG := $(PKG)/internal/api
LDFLAGS ?= -X $(VERSION_PKG).Version=$(VERSION) -X $(VERSION_PKG).Commit=$(COMMIT)

# Release matrix: mac/linux x amd64/arm64.
PLATFORMS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64

.PHONY: build generate test lint install-skill release clean

build:
	go build -ldflags '$(LDFLAGS)' -o $(BINARY) .

generate:
	go generate ./...

test:
	go test ./...

lint:
	go vet ./...
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || \
		echo "golangci-lint not installed; ran go vet only"

install-skill:
	@mkdir -p $(SKILLS_DIR)
	@target="$(SKILLS_DIR)/$(SKILL_NAME)"; \
	src="$(CURDIR)/skills/$(SKILL_NAME)"; \
	if [ -L "$$target" ]; then \
		echo "skill symlink already present: $$target"; \
	elif [ -e "$$target" ]; then \
		echo "WARNING: $$target exists and is not a symlink; leaving it untouched"; \
	else \
		ln -s "$$src" "$$target" && echo "linked $$src -> $$target"; \
	fi

release: clean
	@mkdir -p $(DIST)
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		out="$(DIST)/$(BINARY)-$$os-$$arch"; \
		echo "building $$out"; \
		GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 \
			go build -ldflags '$(LDFLAGS)' -o "$$out" . || exit 1; \
	done

clean:
	rm -rf $(DIST)
	rm -f $(BINARY)
	go clean -testcache
