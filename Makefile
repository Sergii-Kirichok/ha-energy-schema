# ha-energy-schema — BobRIXOS Energy Schema (Home Assistant add-on, Go renderer)
#
# Run from Git Bash (needs sh, go, and for deploy: plink/pscp from PuTTY).
# Quick start:  make check   |   make bump && make release MSG="..."
# Run recipes under Git-for-Windows sh even when make is launched from
# PowerShell/cmd (where sh is not on PATH). The 8.3 short path (PROGRA~1) avoids
# spaces that would otherwise break SHELL; falls back to plain sh (Git Bash /
# Linux / macOS). Without this, POSIX recipes (VAR=1 cmd, pipes, printf) fail.
ifeq ($(OS),Windows_NT)
SHELL := $(firstword $(wildcard C:/PROGRA~1/Git/usr/bin/sh.exe) sh)
# Launched from PowerShell/cmd, sh inherits their PATH and can't find the Git
# coreutils (tail, rm, sed, grep, expr). Prepend Git's usr/bin so recipes work.
export PATH := C:/PROGRA~1/Git/usr/bin;$(PATH)
else
SHELL := sh
endif
.SHELLFLAGS := -c

# Disable Git-Bash/MSYS automatic POSIX->Windows path conversion. Without this,
# plink/pscp args like /bin/sh and /home/star/.ssh/ha_addon get rewritten to
# C:\Program Files\Git\... before the remote shell ever sees them.
export MSYS_NO_PATHCONV := 1
export MSYS2_ARG_CONV_EXCL := *

GO       ?= go
ADDON_DIR := energy-schema
PKGS      := ./...
LOCALBIN  := energy-schema

# Deploy config lives in deploy.local.mk (gitignored). See deploy.local.mk.example.
-include deploy.local.mk

.DEFAULT_GOAL := help

## help: list targets
help:
	@echo "ha-energy-schema targets:"
	@grep -E '^## ' Makefile | sed 's/^## /  /'

# ---------- dev ----------

## fmt: gofmt-write all sources
fmt:
	cd $(ADDON_DIR) && $(GO) fmt $(PKGS)

## fmt-check: fail if any source is not gofmt-clean
fmt-check:
	@cd $(ADDON_DIR) && out=`gofmt -l .`; if [ -n "$$out" ]; then echo "not gofmt-clean:"; echo "$$out"; exit 1; fi

## vet: go vet
vet:
	cd $(ADDON_DIR) && $(GO) vet $(PKGS)

## test: run unit tests
test:
	cd $(ADDON_DIR) && $(GO) test $(PKGS)

## cover: tests + total coverage
cover:
	cd $(ADDON_DIR) && $(GO) test -coverprofile=../coverage.out $(PKGS) && $(GO) tool cover -func=../coverage.out | tail -1

## tidy: go mod tidy
tidy:
	cd $(ADDON_DIR) && $(GO) mod tidy

## build: compile the binary locally (sanity check; HA builds via Docker)
build:
	cd $(ADDON_DIR) && CGO_ENABLED=0 $(GO) build -trimpath -o $(LOCALBIN) ./cmd/energy-schema

## golden: regenerate the render SVG snapshot after an intentional visual change
golden: export UPDATE_GOLDEN := 1
golden:
	cd $(ADDON_DIR) && $(GO) test ./internal/scada/ -run TestRenderGolden

## check: fmt-check + vet + test (CI gate)
check: fmt-check vet test

## bump: increment config.yaml patch version (x.y.Z -> x.y.Z+1)
bump:
	@cd $(ADDON_DIR) && cur=`grep -E '^version:' config.yaml | sed 's/.*"\(.*\)".*/\1/'`; \
	  maj=$${cur%.*}; pat=$${cur##*.}; new="$$maj.`expr $$pat + 1`"; \
	  LC_ALL=C sed -i "s/^version: \".*\"/version: \"$$new\"/" config.yaml; \
	  echo "version $$cur -> $$new"

## clean: remove local build artifacts
clean:
	rm -f $(ADDON_DIR)/$(LOCALBIN) coverage.out

# ---------- git / deploy ----------

## push: commit all and push (usage: make push MSG="...")
push: require-msg
	git add -A
	git commit -m "$(MSG)"
	git push

## remote-update: HAOS store-reload + add-on update + restart + logs
remote-update: require-deploy
	pscp -batch -pw "$(DEPLOY_PASS)" scripts/ha_update.sh $(DEPLOY_USER)@$(DEPLOY_HOST):/tmp/ha_update.sh
	printf 'ssh -i %s -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -p 22 %s@%s "SLUG=%s sh -s" < /tmp/ha_update.sh\n' \
	  "$(HA_KEY)" "$(HA_USER)" "$(HA_HOST)" "$(ADDON_SLUG)" \
	  | plink -batch -pw "$(DEPLOY_PASS)" $(DEPLOY_USER)@$(DEPLOY_HOST) /bin/sh

## logs: tail add-on logs on HAOS
logs: require-deploy
	pscp -batch -pw "$(DEPLOY_PASS)" scripts/ha_logs.sh $(DEPLOY_USER)@$(DEPLOY_HOST):/tmp/ha_logs.sh
	printf 'ssh -i %s -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -p 22 %s@%s "SLUG=%s sh -s" < /tmp/ha_logs.sh\n' \
	  "$(HA_KEY)" "$(HA_USER)" "$(HA_HOST)" "$(ADDON_SLUG)" \
	  | plink -batch -pw "$(DEPLOY_PASS)" $(DEPLOY_USER)@$(DEPLOY_HOST) /bin/sh

## deploy: check + push current state + remote update (usage: make deploy MSG="...")
deploy: check push remote-update

## release: bump version + check + push + remote update (usage: make release MSG="...")
release: require-msg bump check
	git add -A
	git commit -m "$(MSG)"
	git push
	$(MAKE) remote-update

# ---------- guards ----------

require-msg:
	@test -n "$(MSG)" || { echo 'MSG is required, e.g. make $(MAKECMDGOALS) MSG="0.5.0: ..."'; exit 1; }

require-deploy:
	@test -n "$(DEPLOY_HOST)" || { echo "deploy config missing: cp deploy.local.mk.example deploy.local.mk and fill it in"; exit 1; }

.PHONY: help fmt fmt-check vet test cover tidy build golden check bump clean \
        push remote-update logs deploy release require-msg require-deploy
