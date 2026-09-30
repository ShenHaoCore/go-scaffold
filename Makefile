.PHONY: init deps tools gen build docker docker-build test test-coverage lint clean \
	up down run run-api run-rpc run-all dev \
	migrate-up migrate-down migrate-force setup-migrate \
	check-goctl check-go-zero check-templates check-protoc init-templates update-templates \
	newlogic print-config-key

APP_NAME ?= scaffold
GO ?= go
GOCTL_WANT := 1.7.3
GOZERO_WANT := v1.7.3
MIGRATE_WANT := v4.17.1
GOREMAN_WANT := v0.3.15
GOLANGCI_LINT_WANT := v1.55.2

init: deps setup-migrate

deps:
	$(GO) mod tidy
	$(GO) mod download

# 环境准备唯一入口（与 README / go.mod 三处版本一致）
# protoc 本体须系统安装；此处安装 Go 插件
tools:
	$(GO) install github.com/zeromicro/go-zero/tools/goctl@v$(GOCTL_WANT)
	$(GO) install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@$(MIGRATE_WANT)
	$(GO) install github.com/mattn/goreman@$(GOREMAN_WANT)
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_WANT)
	$(GO) install google.golang.org/protobuf/cmd/protoc-gen-go@v1.35.1
	$(GO) install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
	@command -v air >/dev/null 2>&1 || $(GO) install github.com/air-verse/air@latest
	@echo "tools installed: goctl@$(GOCTL_WANT) migrate@$(MIGRATE_WANT) goreman@$(GOREMAN_WANT) golangci-lint@$(GOLANGCI_LINT_WANT) protoc-gen-go/grpc"
	@command -v protoc >/dev/null 2>&1 || echo "WARN: protoc not in PATH (required by make gen); install from https://grpc.io/docs/protoc-installation/"

setup-migrate:
	@command -v migrate >/dev/null 2>&1 || $(GO) install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@$(MIGRATE_WANT)

check-goctl:
	@command -v goctl >/dev/null 2>&1 || { \
		echo "ERROR: goctl not found. Run: make tools"; \
		exit 1; \
	}
	@ver=$$(goctl -v 2>/dev/null | awk '{print $$NF}' | tr -d 'v'); \
	if [ -z "$$ver" ]; then ver=$$(goctl -v 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1); fi; \
	if [ -z "$$ver" ]; then \
		echo "ERROR: cannot parse goctl version; install goctl@v$(GOCTL_WANT)"; \
		exit 1; \
	fi; \
	if [ "$$ver" != "$(GOCTL_WANT)" ]; then \
		echo "ERROR: goctl $$ver != required $(GOCTL_WANT). Run: make tools"; \
		exit 1; \
	fi; \
	echo "goctl $$ver OK"

# 优先匹配缩进的 require 行，避免误匹配 replace / 注释
check-go-zero:
	@mod=$$(grep -E '^[[:blank:]]+github.com/zeromicro/go-zero ' go.mod | head -1 | awk '{print $$2}'); \
	if [ -z "$$mod" ]; then \
		mod=$$(grep 'github.com/zeromicro/go-zero ' go.mod | head -1 | awk '{print $$2}'); \
	fi; \
	if [ "$$mod" != "$(GOZERO_WANT)" ]; then \
		echo "ERROR: go.mod go-zero $$mod != required $(GOZERO_WANT). Run: go get github.com/zeromicro/go-zero@$(GOZERO_WANT)"; \
		exit 1; \
	fi; \
	echo "go-zero $$mod OK"

check-templates:
	@test -f templates/api/handler.tpl || { \
		echo "ERROR: templates/api/handler.tpl not found. Run: make init-templates"; \
		exit 1; \
	}

check-protoc:
	@protos=$$(find api/desc -name '*.proto' 2>/dev/null | head -1); \
	if [ -z "$$protos" ]; then \
		echo "check-protoc SKIP: no *.proto under api/desc"; \
		exit 0; \
	fi; \
	command -v protoc >/dev/null 2>&1 || { \
		echo "ERROR: protoc not found. Install protobuf compiler, then: make tools"; \
		exit 1; \
	}; \
	command -v protoc-gen-go >/dev/null 2>&1 || { \
		echo "ERROR: protoc-gen-go not found. Run: make tools"; \
		exit 1; \
	}; \
	command -v protoc-gen-go-grpc >/dev/null 2>&1 || { \
		echo "ERROR: protoc-gen-go-grpc not found. Run: make tools"; \
		exit 1; \
	}

init-templates:
	goctl template init --home templates
	@echo "Edit templates/api/handler.tpl to use pkg/response (Success/Error)"

# 备份后重置 templates；备份失败须报错；升级后人工 review diff
update-templates:
	@test -d templates || { echo "ERROR: templates/ missing"; exit 1; }
	@ts=$$(date +%Y%m%d%H%M%S); \
	bak="templates.bak.$$ts"; \
	cp -a templates "$$bak" || { echo "ERROR: backup templates failed"; exit 1; }; \
	echo "backed up to $$bak"; \
	goctl template init --home templates; \
	echo "Review diff: diff -ru $$bak templates"

# 入口 main.api（空 service 文档占位）。goctl 若拒绝空 service，则跳过 api gen，仅还原 handwritten routes。
gen: check-goctl check-go-zero check-templates
	@set -e; \
	restore_hw() { \
		rm -f scaffold.go; \
		cp scripts/handwritten/routes.go.in internal/handler/routes.go; \
	}; \
	trap restore_hw EXIT; \
	if goctl api go -api api/desc/main.api -dir . --style go_zero --home templates 2>/tmp/goctl-gen.err; then \
		echo "OK: goctl api gen from main.api"; \
	else \
		echo "WARN: goctl api gen skipped (empty service or other error); keeping handwritten routes"; \
		sed -n '1,20p' /tmp/goctl-gen.err 2>/dev/null || true; \
	fi; \
	restore_hw; \
	grep -q 'StatusServiceUnavailable' internal/logic/health/health_logic.go || { echo "ERROR: health degraded→503 missing"; exit 1; }; \
	grep -q 'RecoveryMiddleware' internal/handler/routes.go || { echo "ERROR: RecoveryMiddleware missing in routes.go"; exit 1; }; \
	grep -q 'TraceMiddleware' internal/handler/routes.go || { echo "ERROR: TraceMiddleware missing in routes.go"; exit 1; }; \
	grep -q 'LangMiddleware' internal/handler/routes.go || { echo "ERROR: LangMiddleware missing in routes.go"; exit 1; }; \
	grep -q 'AuthMiddleware' internal/handler/routes.go || { echo "ERROR: AuthMiddleware missing in routes.go"; exit 1; }; \
	grep -q 'AgentMiddleware' internal/handler/routes.go || { echo "ERROR: AgentMiddleware missing in routes.go"; exit 1; }; \
	grep -q '/health' internal/handler/routes.go || { echo "ERROR: /health routes missing"; exit 1; }; \
	trap - EXIT; \
	if find api/desc -name '*.proto' 2>/dev/null | grep -q .; then \
		$(MAKE) check-protoc; \
		echo "NOTE: *.proto present — run protoc manually for your packages"; \
	fi; \
	echo "OK: gen done; handwritten routes restored; health 503 intact"

# P3：优先自维护模板；goctl --only-logic 仅作可选尝试
newlogic: check-templates
	@if [ -z "$(API)" ]; then \
		echo "Usage: make newlogic API=<name>"; \
		exit 1; \
	fi
	@if [ -f scripts/logic.tpl ]; then \
		mkdir -p internal/logic/$(API); \
		if [ -f internal/logic/$(API)/$(API)_logic.go ]; then \
			echo "SKIP: internal/logic/$(API)/$(API)_logic.go already exists"; \
		else \
			sed "s/{{API}}/$(API)/g" scripts/logic.tpl > internal/logic/$(API)/$(API)_logic.go; \
			echo "created internal/logic/$(API)/$(API)_logic.go"; \
		fi; \
		if [ -f scripts/logic_test.tpl ] && [ ! -f internal/logic/$(API)/$(API)_logic_test.go ]; then \
			sed "s/{{API}}/$(API)/g" scripts/logic_test.tpl > internal/logic/$(API)/$(API)_logic_test.go; \
			echo "created internal/logic/$(API)/$(API)_logic_test.go"; \
		fi; \
	else \
		echo "HINT: add scripts/logic.tpl, or first-time: goctl api go -api api/desc/$(API).api -dir . --style go_zero --home templates"; \
		exit 1; \
	fi

build:
	mkdir -p output
	$(GO) build -o output/$(APP_NAME)-api ./cmd/api
	$(GO) build -o output/$(APP_NAME)-rpc ./cmd/rpc

# IMAGE 可由 CI 覆盖，如 IMAGE=registry/scaffold:sha
IMAGE ?= $(APP_NAME):latest

docker:
	docker build -t $(IMAGE) .

# 与 docker 等价的显式目标名（供 CI 语义化调用）
docker-build: docker

test:
	$(GO) test ./... -count=1

test-coverage:
	$(GO) test ./... -coverprofile=coverage.out || true
	@if [ -f coverage.out ]; then $(GO) tool cover -func=coverage.out; else echo "WARN: coverage.out not generated"; fi

lint:
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run ./...; else $(GO) vet ./...; fi

clean:
	rm -rf output coverage coverage.out coverage.html

up:
	docker compose up -d

down:
	docker compose down

# run ≡ run-all（禁止串行 run-rpc && run-api，第二端永远起不来）
run: run-all

run-api:
	APP_ENV=$${APP_ENV:-dev} $(GO) run ./cmd/api -f config

run-rpc:
	APP_ENV=$${APP_ENV:-dev} $(GO) run ./cmd/rpc -f config

# 强制 goreman + Procfile（禁止 pkill / Makefile trap）
run-all:
	@command -v goreman >/dev/null 2>&1 || { echo "ERROR: goreman not found. Run: make tools"; exit 1; }
	APP_ENV=$${APP_ENV:-dev} goreman -f Procfile start

# 一键：compose +（有 *.up.sql 才）migrate + goreman
dev: up
	@set -a; \
	if [ -f .env ]; then . ./.env; fi; \
	set +a; \
	command -v goreman >/dev/null 2>&1 || { echo "ERROR: goreman not found. Run: make tools"; exit 1; }; \
	if ls migrations/*.up.sql >/dev/null 2>&1; then \
		if [ -z "$$DB_DSN" ]; then \
			echo "ERROR: set DB_DSN (export or put in .env). See .env.example"; \
			exit 1; \
		fi; \
		command -v migrate >/dev/null 2>&1 || $(GO) install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@$(MIGRATE_WANT); \
		echo "[migrate] up ..."; \
		migrate -path migrations -database "$$DB_DSN" up; \
	else \
		echo "[migrate] skip (no migrations/*.up.sql)"; \
	fi; \
	echo "[app] goreman APP_ENV=$${APP_ENV:-dev}"; \
	APP_ENV=$${APP_ENV:-dev} goreman -f Procfile start

print-config-key:
	@echo "Etcd config key: see config/*.yaml Etcd.Key (default: scaffold/config)"

migrate-up:
	@if [ -z "$(DB_DSN)" ]; then \
		echo "ERROR: set DB_DSN first. Example:"; \
		echo "  export DB_DSN='postgres://scaffold:scaffold123@127.0.0.1:5432/scaffold?sslmode=disable'"; \
		echo "See .env.example"; \
		exit 1; \
	fi
	migrate -path migrations -database "$(DB_DSN)" up

migrate-down:
	@if [ -z "$(DB_DSN)" ]; then \
		echo "ERROR: set DB_DSN first. See .env.example"; \
		exit 1; \
	fi
	migrate -path migrations -database "$(DB_DSN)" down 1

# 把 dirty 版本设为 VERSION（修脏状态），不是回退到 VERSION；生产慎用
migrate-force:
	@if [ -z "$(DB_DSN)" ]; then \
		echo "ERROR: set DB_DSN first. See .env.example"; \
		exit 1; \
	fi
	@if [ -z "$(VERSION)" ]; then \
		echo "ERROR: set VERSION=<n>. Example: make migrate-force VERSION=1"; \
		echo "NOTE: force sets version to N (fix dirty), it does NOT roll back to N"; \
		exit 1; \
	fi
	migrate -path migrations -database "$(DB_DSN)" force $(VERSION)
