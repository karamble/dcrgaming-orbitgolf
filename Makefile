GO ?= go
GODOT ?= .tools/Godot_v4.6.3-stable_linux.x86_64

.PHONY: build check play preview
build:
	mkdir -p bin
	$(GO) build -o bin/orbit-backend ./cmd/orbit-backend
check: build
	$(GO) test -race ./...
	$(GO) vet ./...
	@check_log=$$(mktemp /tmp/orbit-check.XXXXXX); \
	trap 'rm -f -- "$$check_log"' EXIT; \
	$(GODOT) --headless --path client --editor --import --quit > "$$check_log" 2>&1; result=$$?; \
	cat "$$check_log"; \
	if [ "$$result" -ne 0 ]; then exit "$$result"; fi; \
	if grep -Eq 'SCRIPT ERROR:|^ERROR:' "$$check_log"; then exit 1; fi; \
	$(GODOT) --headless --path client --script res://scripts/smoke.gd > "$$check_log" 2>&1; result=$$?; \
	cat "$$check_log"; \
	if grep -Eq 'SCRIPT ERROR:|^ERROR:' "$$check_log"; then exit 1; fi; \
	exit $$result
play: build
	$(GODOT) --headless --path client --editor --import --quit
	$(GODOT) --path client
preview: build
	mkdir -p artifacts
	$(GODOT) --headless --path client --editor --import --quit
	$(GODOT) --path client -- --menu --capture=$(CURDIR)/artifacts/title.png
	$(GODOT) --path client -- --hole=0 --capture=$(CURDIR)/artifacts/shipyard.png
	$(GODOT) --path client -- --hole=3 --capture=$(CURDIR)/artifacts/reactor.png
	$(GODOT) --path client -- --hole=6 --capture=$(CURDIR)/artifacts/moon.png
