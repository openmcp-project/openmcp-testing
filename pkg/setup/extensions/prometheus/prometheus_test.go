package prometheus_test

import (
	"testing"

	"sigs.k8s.io/e2e-framework/pkg/envconf"

	"github.com/openmcp-project/openmcp-testing/pkg/setup/extensions/prometheus"
)

func TestPrometheus_Install(t *testing.T) {
	t.Run("test install", func(t *testing.T) {
		cfg, err := envconf.NewFromFlags()
		if err != nil {
			t.Errorf("config failed: %v", err)
			return
		}
		p := prometheus.Prometheus{
			RemoteClusters: []prometheus.RemoteCluster{
				{
					Name:             "onboarding",
					KubeconfigSecret: "managedcontrolplane--onboarding.kubeconfig",
					SecretNamespace:  "openmcp-system",
				},
			},
		}
		gotErr := p.Install(t.Context(), cfg)
		if gotErr != nil {
			t.Errorf("Install() failed: %v", gotErr)
			return
		}
		t.Fatal("Install() succeeded unexpectedly")
	})
}
