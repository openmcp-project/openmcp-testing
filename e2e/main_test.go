package e2e

import (
	"flag"
	"fmt"
	"os"
	"testing"

	"k8s.io/klog/v2"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"

	"github.com/openmcp-project/openmcp-testing/pkg/providers"
	"github.com/openmcp-project/openmcp-testing/pkg/setup"
	"github.com/vladimirvivien/gexe"
)

var testenv env.Environment

func TestMain(m *testing.M) {
	initLogging()
	serviceProviderImage := "ghcr.io/openmcp-project/images/service-provider-crossplane:v0.1.4"
	mustPullImage(serviceProviderImage)
	openmcp := setup.OpenMCPSetup{
		Namespace: "openmcp-system",
		Operator: setup.OpenMCPOperatorSetup{
			Name:         "openmcp-operator",
			Image:        "ghcr.io/openmcp-project/images/openmcp-operator:v0.17.1",
			Environment:  "debug",
			PlatformName: "platform",
		},
		ClusterProviders: []providers.ClusterProviderSetup{
			{
				Name:  "kind",
				Image: "ghcr.io/openmcp-project/images/cluster-provider-kind:v0.0.15",
			},
		},
		ServiceProviders: []providers.ServiceProviderSetup{
			{
				Name:  "crossplane",
				Image: serviceProviderImage,
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

func mustPullImage(image string) {
	klog.Info("Pulling ", image)
	runner := gexe.New()
	p := runner.RunProc(fmt.Sprintf("docker pull %s", image))
	klog.V(4).Info(p.Out())
	if p.Err() != nil {
		panic(fmt.Errorf("docker pull %v failed: %w: %s", image, p.Err(), p.Result()))
	}
}
