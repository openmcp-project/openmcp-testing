package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/openmcp-project/openmcp-testing/pkg/clusterutils"
	"github.com/openmcp-project/openmcp-testing/pkg/conditions"
	"github.com/openmcp-project/openmcp-testing/pkg/providers"
	"github.com/openmcp-project/openmcp-testing/pkg/resources"
	"github.com/openmcp-project/openmcp-testing/pkg/setup/extensions/prometheus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
)

// TestServiceProviderObservability demonstrates how to use the prometheus extension to benchmark a service provider
func TestServiceProviderObservability(t *testing.T) {
	observabilityTest := features.New("observability").
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			klog.Info("waiting one scrape interval")
			select {
			case <-time.After(time.Second * 30):
			case <-ctx.Done():
			}
			return ctx
		}).
		Assess("average container cpu and memory usage", benchmark("sp-externalsecrets", "externalsecrets", prometheus.AllQueries))
	testenv.Test(t, observabilityTest.Feature())
}

// TestAddClusters demonstrates how to use the prometheus extension to benchmark a domain service
func TestAddClusters(t *testing.T) {
	addClusters := features.New("addClusters").
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			if _, err := clusterutils.ImportToPlatformCluster(ctx, c, "platform"); err != nil {
				t.Fatalf("provider config import failed: %v", err)
			}
			return ctx
		}).
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			onboardingCfg, err := clusterutils.OnboardingConfig()
			if err != nil {
				t.Fatalf("failed to retrieve onboarding cluster config: %v", err)
			}
			if err := resources.CreateNObjectsFromTemplateFile(ctx, onboardingCfg, "onboarding/cp-with-eso.yaml", 3, nil); err != nil {
				t.Fatalf("applying test data failed: %v", err)
			}
			if err := providers.ClustersReady(ctx, c); err != nil {
				t.Fatalf("clusters failed to get ready: %v", err)
			}
			return ctx
		}).
		Setup(func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			p := prometheus.Prometheus{}
			if err := p.AddClusters(ctx, c, prometheus.TargetCluster{
				Name:  "test-1", // control plane name
				Label: "test-controlplane",
			}); err != nil {
				t.Errorf("add scrape job for test control plane failed: %v", err)
			}
			return ctx
		}).
		Assess("test resources must be ready", func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			onboardingClient, err := clusterutils.OnboardingConfig()
			if err != nil {
				t.Errorf("failed to retrieve onboarding client: %v", err)
				return ctx
			}
			for i := 0; i < 3; i++ {
				eso := &unstructured.Unstructured{}
				eso.SetName(fmt.Sprintf("test-%d", i))
				eso.SetNamespace(corev1.NamespaceDefault)
				eso.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "external-secrets.services.open-control-plane.io",
					Version: "v1alpha1",
					Kind:    "ExternalSecretsOperator",
				})
				if err := wait.For(conditions.Match(eso, onboardingClient, "Ready", corev1.ConditionTrue)); err != nil {
					t.Errorf("eso not ready: %v", err)
				}
			}
			klog.Info("waiting one scrape interval")
			select {
			case <-time.After(time.Second * 30):
			case <-ctx.Done():
			}
			return ctx
		}).
		// dummy query etcd metrics
		Assess("benchmark service provider (eso)", benchmark("sp-externalsecrets", "externalsecrets", prometheus.AllQueries)).
		Assess("benchmark domain service (eso)", benchmark("external-secrets", "externalsecrets", []prometheus.NamedQuery{
			prometheus.ContainerAverageCPUUsage,
			prometheus.ContainerAverageMemoryUsage,
		}))

	testenv.Test(t, addClusters.Feature())
}

func benchmark(container string, controller string, queries []prometheus.NamedQuery) features.Func {
	return func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
		params := prometheus.QueryParameters{
			Container:  container,
			Controller: controller,
			Window: prometheus.ObservabilityWindow{
				Start: time.Now().Add(-15 * time.Minute),
				End:   time.Now(),
			},
			JobName: fmt.Sprintf("query-%s", container),
		}
		results, err := prometheus.Query(ctx, cfg, queries, params)
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
