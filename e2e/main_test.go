package e2e

import (
	"flag"
	"os"
	"testing"

	"github.com/openmcp-project/openmcp-testing/pkg/platformservices"
	"github.com/openmcp-project/openmcp-testing/pkg/providers"
	"github.com/openmcp-project/openmcp-testing/pkg/setup"
	"github.com/openmcp-project/openmcp-testing/pkg/setup/extensions"
	"github.com/openmcp-project/openmcp-testing/pkg/setup/extensions/fluxcd"
	"k8s.io/klog/v2"
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
			Image:        "ghcr.io/openmcp-project/images/openmcp-operator:v0.18.1",
			Environment:  "debug",
			PlatformName: "platform",
		},
		ClusterProviders: []providers.ClusterProviderSetup{
			{
				Name:  "kind",
				Image: "ghcr.io/openmcp-project/images/cluster-provider-kind:v0.1.0",
			},
		},
		ServiceProviders: []providers.ServiceProviderSetup{
			{
				Name:  "crossplane",
				Image: "ghcr.io/openmcp-project/images/service-provider-crossplane:v0.1.4",
			},
		},
		PlatformServices: []platformservices.PlatformServiceSetup{
			{
				Name:                      "gateway",
				Image:                     "ghcr.io/openmcp-project/images/platform-service-gateway:v0.0.9",
				PlatformServiceConfigsDir: "platformservice-gateway",
			},
		},
		Extensions: []extensions.Extension{
			&fluxcd.FluxCD{},
		},
	}
	testenv = env.NewWithConfig(envconf.New().WithNamespace(openmcp.Namespace))
	openmcp.Bootstrap(testenv)
	os.Exit(testenv.Run(m))
}

func initLogging() {
	klog.InitFlags(nil)
	if err := flag.Set("v", "2"); err != nil {
		panic(err)
	}
	flag.Parse()
}
