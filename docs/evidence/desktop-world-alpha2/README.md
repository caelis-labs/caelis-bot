# Published alpha.2 integration acceptance

These logs contain only disposable fixture data. Official SDK: v0.1.0-alpha.2,
Go sum h1:a4fazp2HC/G/buCsaXZ798YXxn2ozfHccqR4mokxuXQ= (no Replace).
Published helper revision cc50357f9f922712ef4e2bf4db4822381d10d71a; archive SHA256
7e6932562a1b5da10fa67dc8458442203134da42def05212faf11884e59a42f3.

Normal preparation: `make setup`, `make check`, `make smoke`, `make build`.
The build downloaded the real published archive through desktop-world-runtime.sh.
With the absolute normally built development bundle path:

```sh
bash script/verify-desktop-world.sh "$PWD/dist/Caelis Bot Dev.app" development
codesign --verify --deep --strict "$PWD/dist/Caelis Bot Dev.app"
GOWORK=off CAELIS_BOT_TEST_DESKTOP_WORLD="$PWD/dist/Caelis Bot Dev.app/Contents/Resources/DesktopWorld/bin/desktop-world" \
  go test -race ./internal/desktopcontrol -count=1 -v
```

Published acceptance source files `value_test.go` and `observe_timeout_test.go`
were copied into ignored `.cache/pin-native/`, then compiled with
`GOWORK=off go test -c -o .cache/pin-native.test ./.cache/pin-native` using Bot's
normal go.mod (no modfile/replace). Real owned AppKit fixture launch went through
upstream `script/build_and_run.sh --build-only` followed by explicit `open -n`
with new unique title/log and slow-count 0 or 200. No broad process cleanup ran.

```sh
DW_NATIVE_FIXTURE_TITLE='Bot alpha2 native values 20260930-1050' \
DW_NATIVE_FIXTURE_LOG='/absolute/owned/value-fixture.jsonl' \
  .cache/pin-native.test -test.v -test.run '^TestNativeValueFixture$'
DW_NATIVE_FIXTURE_TITLE='Bot alpha2 slow AX 20260930-1051' \
DW_NATIVE_HELPER_PATH="$PWD/dist/Caelis Bot Dev.app/Contents/Resources/DesktopWorld/bin/desktop-world" \
  .cache/pin-native.test -test.v -test.run '^TestNativeObserveTimeoutFixture$'
```

Value tests exercise the actual pinned SDK native backend. Timeout tests exercise
the actual Bot packaged helper through the published SDK host. Adapter tests
exercise Bot's actual controller/child-helper boundary. They do not establish a
fresh model-mediated Bot/Wails UI run. Isolated Bot launch failed with LSOpen
-10810; no repeated launch or trust/permission change was attempted. Existing
user Bot and browser tabs were untouched. Optional blocked JS/CUA checks from
upstream work remain untested; they were not bypassed.
