package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/openmcp-project/openmcp-testing/pkg/clusterutils"
	"github.com/openmcp-project/openmcp-testing/pkg/providers"
	"github.com/openmcp-project/openmcp-testing/pkg/setup/extensions/prometheus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	klientresources "sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
)

// TestServiceProviderObservability demonstrates how to use the prometheus extension to benchmark a service provider
func TestServiceProviderObservability(t *testing.T) {
	observabilityTest := features.New("observability").
		Assess("average pod cpu and memory usage", benchmark())
	testenv.Test(t, observabilityTest.Feature())
}

// TestAddClusters demonstrates how to use the prometheus extension to benchmark a domain service
func TestAddClusters(t *testing.T) {
	addClusters := features.New("addClusters").
		Setup(providers.CreateMCP("test")).
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			if _, err := clusterutils.ImportToPlatformCluster(ctx, c, "crossplaneconfig"); err != nil {
				t.Fatalf("provider config import failed: %v", err)
			}
			if _, err := clusterutils.ImportToOnboardingCluster(ctx, "onboarding"); err != nil {
				t.Fatalf("onboarding resources import failed: %v", err)
			}
			return ctx
		}).
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			p := prometheus.Prometheus{}
			if err := p.AddClusters(ctx, c, prometheus.TargetCluster{
				Name:  "test", // control plane name
				Label: "test-controlplane",
			}); err != nil {
				t.Errorf("add scrape job for test control plane failed: %v", err)
			}
			return ctx
		}).
		Assess("average pod cpu and memory usage", benchmark())

	testenv.Test(t, addClusters.Feature())
}

func benchmark() features.Func {
	return func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
		window := prometheus.ObservabilityWindow{
			Start: time.Now().Add(-15 * time.Minute),
			End:   time.Now(),
		}
		pods := &corev1.PodList{}
		if err := cfg.Client().Resources().List(ctx, pods, klientresources.WithLabelSelector("app.kubernetes.io/instance=crossplane")); err != nil {
			t.Errorf("failed to retrieve service provider pod: %v", err)
			return ctx
		}
		for _, p := range pods.Items {
			// ignore init pod in phase completed
			if p.Status.Phase == corev1.PodRunning {
				window.PodName = p.Name
				break
			}
		}
		results, err := prometheus.Query(ctx, cfg, prometheus.AllQueries, window, "")
		if err != nil {
			t.Errorf("promQL query failed: %v", err)
		}
		for _, qr := range results {
			klog.Infof("%s\n", qr)
		}
		return ctx
	}
}
