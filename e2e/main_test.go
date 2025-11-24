package e2e

import (
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/christophrj/openmcp-testing/pkg/providers"
	"github.com/christophrj/openmcp-testing/pkg/setup"
	"k8s.io/klog/v2"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
)

var testenv env.Environment

func TestMain(m *testing.M) {
	initLogging()
	openmcp := setup.OpenMCPSetup{
		Namespace: "openmcp-system",
		Operator: setup.OpenMCPOperatorSetup{
			Name:         "openmcp-operator",
			Image:        "ghcr.io/openmcp-project/images/openmcp-operator:v0.13.0",
			Environment:  "debug",
			PlatformName: "platform",
		},
		ClusterProviders: []providers.ClusterProviderSetup{
			{
				Name:  "kind",
				Image: "ghcr.io/openmcp-project/images/cluster-provider-kind:v0.0.15",
				Opts: []wait.Option{
					wait.WithTimeout(time.Minute),
				},
			},
		},
		ServiceProviders: []providers.ServiceProviderSetup{
			{
				Name:  "crossplane",
				Image: "ghcr.io/openmcp-project/images/service-provider-crossplane:v0.0.4",
				Opts: []wait.Option{
					wait.WithTimeout(time.Minute),
				},
			},
		},
	}
	testenv = env.NewWithConfig(envconf.New().WithNamespace(openmcp.Namespace))
	if err := openmcp.Bootstrap(testenv); err != nil {
		panic(fmt.Errorf("openmcp bootstrap failed: %v", err))
	}
	os.Exit(testenv.Run(m))
}

func initLogging() {
	klog.InitFlags(nil)
	if err := flag.Set("v", "2"); err != nil {
		panic(err)
	}
	flag.Parse()
}
