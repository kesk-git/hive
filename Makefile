# The targets of spec/test-targets.md. Every target and every package
# directory is named in full; no target is derived from a pattern.

GO ?= go

.PHONY: test test-contract test-pool test-spawning-pool test-dispatch \
	test-shadow test-synapse test-word test-ui test-integration \
	test-distribution check-imports check build

# test-dir runs the tests under the directories in $(1) with the module's
# standard tooling. A target whose directories hold no test fails, so an empty
# package cannot pass by default.
define test-dir
@if [ -z "$$(find $(1) -name '*_test.go' -print -quit 2>/dev/null)" ]; then \
	echo "$@: nothing to run: $(1) holds no test" >&2; \
	exit 1; \
fi
$(GO) test $(foreach d,$(1),./$(d)/...)
endef

# build-exe builds the executable $(2) of the package $(1) into dist/ when its
# main package exists, and otherwise says that it is not there yet.
define build-exe
@if [ -n "$$(find packages/$(1)/cmd/$(2) -name '*.go' -print -quit 2>/dev/null)" ]; then \
	CGO_ENABLED=0 $(GO) build -trimpath -o dist/$(2) ./packages/$(1)/cmd/$(2); \
else \
	echo "build: $(2) is not there yet"; \
fi
endef

test:
	$(MAKE) check
	$(MAKE) test-contract
	$(MAKE) test-pool
	$(MAKE) test-spawning-pool
	$(MAKE) test-dispatch
	$(MAKE) test-shadow
	$(MAKE) test-synapse
	$(MAKE) test-word
	$(MAKE) test-ui
	$(MAKE) build
	$(MAKE) test-integration
	$(MAKE) test-distribution
	$(MAKE) check-imports

test-contract:
	$(call test-dir,contract spec)

test-pool:
	$(call test-dir,packages/pool)

test-spawning-pool:
	$(call test-dir,packages/spawning-pool)

test-dispatch:
	$(call test-dir,packages/dispatch)

test-shadow:
	$(call test-dir,packages/shadow)

test-synapse:
	$(call test-dir,packages/synapse)

test-word:
	$(call test-dir,packages/word)

test-ui:
	$(call test-dir,packages/ui)

test-integration:
	$(call test-dir,integration/calls)

test-distribution:
	$(call test-dir,integration/distribution)

check-imports:
	$(call test-dir,integration/imports)

check:
	@out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then \
		echo "check: these files are not formatted:" >&2; \
		echo "$$out" >&2; \
		exit 1; \
	fi
	$(GO) vet ./...
	GOOS=windows $(GO) vet ./...
	GOOS=linux $(GO) vet ./...

build:
	$(GO) build ./...
	@mkdir -p dist
	$(call build-exe,pool,hive-pool)
	$(call build-exe,spawning-pool,hive-spawning-pool)
	$(call build-exe,dispatch,hive-dispatch)
	$(call build-exe,shadow,hive-shadow)
	$(call build-exe,synapse,hive-synapse)
	$(call build-exe,word,hive-word)
	$(call build-exe,ui,hive-ui)
