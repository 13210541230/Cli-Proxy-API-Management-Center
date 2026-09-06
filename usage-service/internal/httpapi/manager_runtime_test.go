package httpapi

import (
	"reflect"
	"testing"

	"github.com/seakee/cpa-manager/usage-service/internal/store"
	"github.com/seakee/cpa-manager/usage-service/internal/supervisor"
)

func TestDefaultManagerConfigPreservesExplicitRuntimeDisable(t *testing.T) {
	controller := supervisor.New()
	controller.Configure(supervisor.Config{Enabled: false, AutoStart: false})
	server := &Server{supervisor: controller}

	config := server.defaultManagerConfig()
	if config.LocalRuntime.Enabled || config.LocalRuntime.AutoStart {
		t.Fatalf("default runtime config = %#v", config.LocalRuntime)
	}
}

func TestMergeSubmittedManagerConfigPreservesExplicitRuntimeDisable(t *testing.T) {
	server := &Server{}
	base := store.ManagerConfig{
		LocalRuntime: store.LocalRuntimeConfig{
			Enabled:           true,
			CPAExecutablePath: "cli-proxy-api",
			WorkingDirectory:  "runtime",
			AutoStart:         true,
		},
	}
	submitted := store.ManagerConfig{}

	merged := server.mergeSubmittedManagerConfig(base, submitted, true)
	if merged.LocalRuntime.Enabled || merged.LocalRuntime.AutoStart ||
		merged.LocalRuntime.CPAExecutablePath != "" || merged.LocalRuntime.WorkingDirectory != "" {
		t.Fatalf("merged runtime config = %#v", merged.LocalRuntime)
	}
}

func TestMergeSubmittedManagerConfigPreservesOmittedRuntime(t *testing.T) {
	server := &Server{}
	base := store.ManagerConfig{
		LocalRuntime: store.LocalRuntimeConfig{
			Enabled:           true,
			CPAExecutablePath: "cli-proxy-api",
			AutoStart:         true,
		},
	}

	merged := server.mergeSubmittedManagerConfig(base, store.ManagerConfig{}, false)
	if !reflect.DeepEqual(merged.LocalRuntime, base.LocalRuntime) {
		t.Fatalf("merged runtime config = %#v, want %#v", merged.LocalRuntime, base.LocalRuntime)
	}
}
