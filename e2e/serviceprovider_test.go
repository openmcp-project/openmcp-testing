package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/christophrj/openmcp-testing/pkg/clusterutils"
	"github.com/christophrj/openmcp-testing/pkg/providers"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
)

func TestServiceProvider(t *testing.T) {
	basicProviderTest := features.New("provider test").
		Setup(providers.CreateMCP("test-mcp", time.Minute)).
		Setup(providers.ImportServiceProviderAPIs("serviceproviderobjects", wait.WithTimeout(time.Minute))).
		Setup(providers.ImportDomainAPIs("domainobjects", wait.WithTimeout(time.Minute))).
		Assess("verify onboarding cluster objects", func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			cfg, err := clusterutils.OnboardingConfig()
			if err != nil {
				t.Error(err)
				return ctx
			}
			assertDummyConfigMap(ctx, t, cfg)
			return ctx
		}).
		Assess("verify mcp cluster objects", func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			cfg, err := clusterutils.McpConfig()
			if err != nil {
				t.Error(err)
				return ctx
			}
			assertDummyConfigMap(ctx, t, cfg)
			return ctx
		}).
		Teardown(providers.DeleteMCP("test-mcp", wait.WithTimeout(time.Minute)))
	testenv.Test(t, basicProviderTest.Feature())
}

func assertDummyConfigMap(ctx context.Context, t *testing.T, cfg *envconf.Config) {
	cm := &v1.ConfigMap{}
	if err := cfg.Client().Resources().Get(ctx, "dummy", v1.NamespaceDefault, cm); err != nil {
		t.Error(err)
		return
	}
	v, ok := cm.Data["foo"]
	if !ok || v != "bar" {
		t.Errorf("expected foo:bar; got: %t %v", ok, v)
	}
}
