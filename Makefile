.PHONY: setup doctor dev check check-portability build run package smoke smoke-adapter smoke-workflow smoke-bot smoke-caelis smoke-caelis-live schema
setup:
	./script/npm.sh ci
	GOWORK=off go mod download
doctor:
	./script/doctor.sh
dev:
	./script/npm.sh run dev
check:
	./script/check.sh
check-portability:
	./script/npm.sh run check:portability
build:
	./script/build.sh
run:
	./script/build_and_run.sh
package:
	./script/package.sh
smoke:
	./script/npm.sh run smoke:codex
	./script/npm.sh run smoke:assets
smoke-adapter:
	./script/smoke-adapter.sh
smoke-workflow:
	./script/smoke-workflow.sh
schema:
	./script/npm.sh run schema:codex

smoke-bot:
	bash -c 'source script/env.sh; go run ./cmd/bot-workflow-smoke'

# Uses a separately built/installed Caelis binary, never a sibling Go import.
smoke-caelis:
	bash -c 'set -e; source script/env.sh; : "$${CAELIS_BOT_TEST_BINARY:?Set an external Caelis binary}"; go test -race -v -timeout 120s -run "^TestNativeHostIntegration$$" ./internal/backend/caelis'

# Opt-in, billable model calls through an already configured isolated Host.
smoke-caelis-live:
	bash -c 'set -e; source script/env.sh; : "$${CAELIS_BOT_LIVE_STORE:?Set an isolated configured Caelis store}" "$${CAELIS_BOT_LIVE_MODEL:?Set the model name}"; go test -race -v -count=1 -timeout 10m -run "^TestConfiguredModelIntegration$$" ./internal/backend/caelis'
