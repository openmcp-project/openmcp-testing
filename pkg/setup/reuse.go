package setup

import (
	"os"
	"strconv"
)

// EnvReuseCluster, when set to a truthy value, opts the test harness into
// reuse mode: an existing kind cluster (created by a previous run) is
// reused, install paths are still applied (idempotently), and the
// framework-level Finish/cleanup chain is skipped so the cluster survives
// the run.
//
// The cluster name is taken from OpenMCPSetup.PlatformClusterName (defaults
// to "e2e-platform"); set it in your TestMain to a project-specific value if
// you run multiple consumers on the same machine.
const EnvReuseCluster = "E2E_REUSE_CLUSTER"

// IsReuseMode reports whether E2E_REUSE_CLUSTER is set to a truthy value.
// Use this in test code to gate feature.Teardown calls (e.g. providers.DeleteMCP)
// that you want to skip when reusing the cluster across runs:
//
//	f := feature.New("...").Setup(providers.CreateMCP(name))
//	if !setup.IsReuseMode() {
//	    f = f.Teardown(providers.DeleteMCP(name))
//	}
func IsReuseMode() bool {
	return parseBoolEnv(os.Getenv(EnvReuseCluster))
}

func parseBoolEnv(v string) bool {
	if v == "" {
		return false
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false
	}
	return b
}
