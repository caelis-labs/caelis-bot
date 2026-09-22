.PHONY: setup doctor dev check check-portability build run package smoke smoke-adapter smoke-workflow smoke-bot schema
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
