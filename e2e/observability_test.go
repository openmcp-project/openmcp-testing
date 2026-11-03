package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/openmcp-project/openmcp-testing/pkg/clusterutils"
	"github.com/openmcp-project/openmcp-testing/pkg/providers"
	"github.com/openmcp-project/openmcp-testing/pkg/setup/extensions/prometheus"
	"k8s.io/klog/v2"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
)

// TestServiceProviderObservability demonstrates how to use the prometheus extension to benchmark a service provider
func TestServiceProviderObservability(t *testing.T) {
	observabilityTest := features.New("observability").
		Assess("average container cpu and memory usage", benchmark("sp-externalsecrets", prometheus.AllQueries))
	testenv.Test(t, observabilityTest.Feature())
}

// TestAddClusters demonstrates how to use the prometheus extension to benchmark a domain service
func TestAddClusters(t *testing.T) {
	addClusters := features.New("addClusters").
		Setup(providers.CreateMCP("test")).
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			if _, err := clusterutils.ImportToPlatformCluster(ctx, c, "platform"); err != nil {
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
		// dummy query etcd metrics
		Assess("benchmark service provider (eso)", benchmark("sp-externalsecrets", prometheus.AllQueries)).
		Assess("benchmark domain service (eso)", benchmark("external-secrets", []prometheus.NamedQuery{
			prometheus.QueryAverageContainerCPU,
			prometheus.QueryAverageContainerMemoryMiB,
		}))

	testenv.Test(t, addClusters.Feature())
}

func benchmark(container string, queries []prometheus.NamedQuery) features.Func {
	return func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
		window := prometheus.ObservabilityWindow{
			Start:         time.Now().Add(-15 * time.Minute),
			End:           time.Now(),
			ContainerName: container,
		}
		results, err := prometheus.Query(ctx, cfg, queries, window, "", "query-"+container)
		if err != nil {
			t.Errorf("promQL query failed: %v", err)
		}
		klog.Infof("query results for container %s", container)
		for _, qr := range results {
			klog.Infof("%s\n", qr)
		}
		return ctx
	}
}
