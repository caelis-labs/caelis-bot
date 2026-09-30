package productmanagement

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"github.com/caelis-labs/caelis-bot/internal/runtimemanagement"
)

type fixtureInstaller struct {
	requests []runtimemanagement.Request
	call     func(runtimemanagement.Request) (runtimemanagement.Status, error)
}

func (f *fixtureInstaller) Manage(_ context.Context, request runtimemanagement.Request) (runtimemanagement.Status, error) {
	f.requests = append(f.requests, request)
	if f.call != nil {
		return f.call(request)
	}
	return runtimemanagement.Status{Runtime: request.Runtime, RequestID: request.RequestID, Outcome: "accepted", Version: request.Version}, nil
}

type fixtureConfiguration struct {
	reads, changes int
	change         api.RuntimeConfigurationChange
	result         api.RuntimeMutationResult
	err            error
}

func (f *fixtureConfiguration) RuntimeConfiguration(context.Context) (api.RuntimeConfiguration, error) {
	f.reads++
	return api.RuntimeConfiguration{Revision: "42", Main: api.WorkExecutionSettings{Model: "public/model", Effort: "medium"}}, nil
}
func (f *fixtureConfiguration) ChangeRuntimeConfiguration(_ context.Context, change api.RuntimeConfigurationChange) (api.RuntimeMutationResult, error) {
	f.changes++
	f.change = change
	return f.result, f.err
}

func fixtureController() (*Controller, *fixtureInstaller, *fixtureConfiguration) {
	installation := &fixtureInstaller{}
	configuration := &fixtureConfiguration{result: api.RuntimeMutationResult{OperationID: "settings-original", Outcome: "accepted"}}
	return &Controller{scope: Scope{BotID: "bot-fixture", Generation: "generation-1"}, installation: installation, configuration: configuration, releases: []ReviewedRelease{{Runtime: "codex", Version: "0.159.2"}, {Runtime: "caelis", Version: "0.65.0"}}}, installation, configuration
}

func TestScopeFencesEverySettingsPortBeforeNativeCalls(t *testing.T) {
	controller, installation, configuration := fixtureController()
	for _, scope := range []Scope{{}, {BotID: controller.scope.BotID, Generation: "generation-2"}, {BotID: "other-bot", Generation: controller.scope.Generation}} {
		if _, err := controller.ReviewedReleases(scope); err == nil {
			t.Fatal("stale scope read catalog")
		}
		if _, err := controller.RuntimeStatus(t.Context(), scope, "codex"); err == nil {
			t.Fatal("stale scope inspected runtime")
		}
		if _, err := controller.RuntimeConfiguration(t.Context(), scope); err == nil {
			t.Fatal("stale scope inspected configuration")
		}
		result, _ := controller.ManageRuntime(t.Context(), RuntimeCommand{Scope: scope, ID: "command-1", Action: "install", Runtime: "codex", Version: "0.159.2"})
		if result.Outcome != "rejected" {
			t.Fatalf("stale scope dispatched runtime: %+v", result)
		}
		config, _ := controller.ChangeRuntimeConfiguration(t.Context(), ConfigurationCommand{Scope: scope, ID: "command-2", Change: api.RuntimeConfigurationChange{Action: "main", ExpectedRevision: "42", Selection: api.WorkExecutionSettings{Model: "public/model"}}})
		if config.Outcome != "rejected" {
			t.Fatalf("stale scope dispatched configuration: %+v", config)
		}
	}
	if len(installation.requests) != 0 || configuration.reads != 0 || configuration.changes != 0 {
		t.Fatal("stale scope reached native authority")
	}
}

func TestInstallerReceiptUsesScopedStableIDAndExplicitResolve(t *testing.T) {
	controller, installation, _ := fixtureController()
	installation.call = func(request runtimemanagement.Request) (runtimemanagement.Status, error) {
		outcome := "unknown"
		if request.Action == "resolve" {
			outcome = "accepted"
		}
		return runtimemanagement.Status{Runtime: request.Runtime, RequestID: request.RequestID, Outcome: outcome, Version: request.Version}, nil
	}
	command := RuntimeCommand{Scope: controller.scope, ID: "external:stable", Action: "update", Runtime: "codex", Version: "0.159.2", ExpectedVersion: "0.153.4"}
	result, err := controller.ManageRuntime(t.Context(), command)
	if err != nil || result.Outcome != "unknown" || result.ID != command.ID || result.Status.RequestID != command.ID || len(installation.requests) != 1 {
		t.Fatalf("original unknown receipt: %+v %v", result, err)
	}
	if installation.requests[0].RequestID == command.ID {
		t.Fatal("native ID omitted inspected identity")
	}
	command.Action = "resolve"
	result, err = controller.ManageRuntime(t.Context(), command)
	if err != nil || result.Outcome != "accepted" || len(installation.requests) != 2 || installation.requests[0].RequestID != installation.requests[1].RequestID {
		t.Fatalf("resolve changed original native request ID: %+v %v", result, err)
	}
	if scopedRequestID(controller.scope, command.ID) == scopedRequestID(Scope{BotID: controller.scope.BotID, Generation: "generation-2"}, command.ID) {
		t.Fatal("different service generation reused native authority")
	}
}

func TestKnownInstallationRejectionAndInvalidNativeReceiptStayDistinct(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		controller, installation, _ := fixtureController()
		installation.call = func(request runtimemanagement.Request) (runtimemanagement.Status, error) {
			id := request.RequestID
			if invalid {
				id = "unrelated-operation"
			}
			return runtimemanagement.Status{Runtime: request.Runtime, RequestID: id, Outcome: "rejected", Installed: true, Version: "0.153.4"}, errors.New("native rejection")
		}
		result, err := controller.ManageRuntime(t.Context(), RuntimeCommand{Scope: controller.scope, ID: "update-1", Action: "update", Runtime: "codex", Version: "0.159.2", ExpectedVersion: "stale"})
		expected := "rejected"
		if invalid {
			expected = "unknown"
		}
		if err != nil || result.Outcome != expected {
			t.Fatalf("receipt authority lost: %+v %v", result, err)
		}
	}
}

func TestConfigurationUnknownDoesNotRetryOrInferSuccessFromCurrentValues(t *testing.T) {
	controller, _, configuration := fixtureController()
	configuration.result = api.RuntimeMutationResult{OperationID: "settings-original", Outcome: "unknown", Message: "Native response unavailable"}
	command := ConfigurationCommand{Scope: controller.scope, ID: "config-1", Change: api.RuntimeConfigurationChange{Action: "main", ExpectedRevision: "42", Selection: api.WorkExecutionSettings{Model: "public/model", Effort: "medium"}}}
	result, err := controller.ChangeRuntimeConfiguration(t.Context(), command)
	if err != nil || result.Outcome != "unknown" || result.Native.OperationID != "settings-original" || configuration.changes != 1 || configuration.reads != 0 || configuration.change != command.Change {
		t.Fatalf("configuration original uncertainty changed: %+v %v", result, err)
	}
	view, err := controller.RuntimeConfiguration(t.Context(), controller.scope)
	if err != nil || view.Main != command.Change.Selection || result.Outcome != "unknown" || configuration.changes != 1 {
		t.Fatal("read of matching values inferred original receipt or redispatched")
	}
	if controller.Capabilities().ConfigurationReceiptLookup {
		t.Fatal("configuration receipt lookup advertised without native port")
	}
	configuration.err = errors.New("private network detail must not be returned")
	result, _ = controller.ChangeRuntimeConfiguration(t.Context(), ConfigurationCommand{Scope: controller.scope, ID: "new-explicit-intent", Change: command.Change})
	encoded, _ := json.Marshal(result)
	if result.Outcome != "unknown" || result.Native.OperationID != "settings-original" || strings.Contains(string(encoded), "private network") {
		t.Fatalf("native error lost original operation or leaked details: %s", encoded)
	}
}

func TestInvalidConfigurationAndRuntimeCommandsCannotDispatch(t *testing.T) {
	controller, installation, configuration := fixtureController()
	for _, command := range []RuntimeCommand{
		{Scope: controller.scope, ID: "install", Action: "shell", Runtime: "codex", Version: "0.159.2"},
		{Scope: controller.scope, ID: "update", Action: "update", Runtime: "codex", Version: "0.159.2"},
		{Scope: controller.scope, ID: "detect", Action: "detect", Runtime: "codex", Version: "0.159.2"},
		{Scope: controller.scope, ID: "install", Action: "install", Runtime: "other", Version: "0.159.2"},
	} {
		result, _ := controller.ManageRuntime(t.Context(), command)
		if result.Outcome != "rejected" {
			t.Fatalf("invalid runtime action admitted: %+v", result)
		}
	}
	for _, change := range []api.RuntimeConfigurationChange{
		{Action: "main", Selection: api.WorkExecutionSettings{Model: "public/model"}},
		{Action: "main", ExpectedRevision: "18446744073709551616", Selection: api.WorkExecutionSettings{Model: "public/model"}},
		{Action: "auth-setup", ExpectedRevision: "42"},
		{Action: "main", ExpectedRevision: "42", Name: "extra payload", Selection: api.WorkExecutionSettings{Model: "public/model"}},
		{Action: "reset", ExpectedRevision: "42", ID: "role", Selection: api.WorkExecutionSettings{Model: "extra payload"}},
	} {
		result, _ := controller.ChangeRuntimeConfiguration(t.Context(), ConfigurationCommand{Scope: controller.scope, ID: "configuration", Change: change})
		if result.Outcome != "rejected" {
			t.Fatalf("invalid configuration action admitted: %+v", result)
		}
	}
	if len(installation.requests) != 0 || configuration.changes != 0 {
		t.Fatal("invalid settings reached native authority")
	}
}

func TestCancelledBeforeDispatchAndUnavailableCapabilities(t *testing.T) {
	controller, installation, configuration := fixtureController()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result, _ := controller.ManageRuntime(ctx, RuntimeCommand{Scope: controller.scope, ID: "install", Action: "install", Runtime: "codex", Version: "0.159.2"})
	config, _ := controller.ChangeRuntimeConfiguration(ctx, ConfigurationCommand{Scope: controller.scope, ID: "config", Change: api.RuntimeConfigurationChange{Action: "main", ExpectedRevision: "42", Selection: api.WorkExecutionSettings{Model: "public/model"}}})
	if result.Outcome != "rejected" || config.Outcome != "rejected" || len(installation.requests) != 0 || configuration.changes != 0 {
		t.Fatal("cancelled request reached native mutation")
	}
	controller.installation, controller.configuration = nil, nil
	if controller.Capabilities() != (Capabilities{}) {
		t.Fatal("unbound settings advertised capability")
	}
	if _, err := controller.RuntimeConfiguration(t.Context(), controller.scope); err == nil {
		t.Fatal("unbound configuration port accepted read")
	}
}

func TestCatalogIsPublicDetachedProjectionAndSecretsHaveNoWireFields(t *testing.T) {
	controller, _, _ := fixtureController()
	releases, err := controller.ReviewedReleases(controller.scope)
	if err != nil {
		t.Fatal(err)
	}
	releases[0].Version = "changed"
	if controller.releases[0].Version == "changed" {
		t.Fatal("caller changed reviewed catalog")
	}
	for _, payload := range []string{
		`{"id":"install","action":"install","runtime":"codex","version":"0.159.2","url":"https://other.invalid"}`,
		`{"id":"config","change":{"action":"main","expectedRevision":"42","apiKey":"never echo"}}`,
		`{"id":"config","change":{"action":"main","expectedRevision":"42","selection":{"model":"public/model","token":"never echo"}}}`,
	} {
		decoder := json.NewDecoder(strings.NewReader(payload))
		decoder.DisallowUnknownFields()
		var err error
		if strings.Contains(payload, `"change"`) {
			err = decoder.Decode(new(ConfigurationCommand))
		} else {
			err = decoder.Decode(new(RuntimeCommand))
		}
		if err == nil {
			t.Fatal("secret or source authority was accepted by closed settings DTO")
		}
	}
}
