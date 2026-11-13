BINARY := claude-router
PLATFORMS := darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64
UI ?= 1
UI_DEP := $(if $(filter 0,$(UI)),ui-clean,ui)
VERSION ?= dev
LDFLAGS := -s -w -X main.version=$(VERSION)

test:
	go test ./... -race

ui:
	cd web && npm ci && npm run build

ui-clean:
	find web/dist -mindepth 1 ! -name .gitkeep -delete

ui-test:
	cd web && npm test

build: $(UI_DEP)
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/router

release: $(UI_DEP)
	@for p in $(PLATFORMS); do \
	  os=$${p%/*}; arch=$${p#*/}; ext=""; \
	  [ "$$os" = "windows" ] && ext=".exe"; \
	  echo "=> $$os/$$arch"; \
	  CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
	    go build -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-$$os-$$arch$$ext ./cmd/router || exit 1; \
	done

prices:
	curl -fsSL https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json \
	  -o internal/pricing/prices.json

.PHONY: test build release prices ui ui-clean ui-test
