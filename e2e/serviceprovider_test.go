package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/openmcp-project/openmcp-testing/pkg/conditions"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/openmcp-project/openmcp-testing/pkg/clusterutils"
	"github.com/openmcp-project/openmcp-testing/pkg/providers"
	"github.com/openmcp-project/openmcp-testing/pkg/setup"
)

func TestServiceProvider(t *testing.T) {
	basicProviderTest := features.New("provider test").
		Setup(providers.CreateMCP("test-mcp", wait.WithTimeout(2*time.Minute))).
		Setup(providers.ImportServiceProviderAPIs("serviceproviderobjects", wait.WithTimeout(time.Minute))).
		Setup(providers.ImportDomainAPIs("test-mcp", "domainobjects", wait.WithTimeout(time.Minute))).
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
			cfg, err := clusterutils.MCPConfig(ctx, c, "test-mcp")
			if err != nil {
				t.Error(err)
				return ctx
			}
			assertDummyConfigMap(ctx, t, cfg)
			return ctx
		}).
		Assess("verify default gateway", func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			if installErr := gatewayv1.Install(c.Client().Resources().GetScheme()); installErr != nil {
				t.Error(installErr)
			}

			gateway := &gatewayv1.Gateway{}
			gateway.SetName("default")
			gateway.SetNamespace("openmcp-system")

			if err := wait.For(conditions.Match(gateway, c, "Accepted", corev1.ConditionTrue), wait.WithTimeout(time.Minute)); err != nil {
				t.Error(err)
			}
			return ctx
		}).
		// Delete the dummy ConfigMaps so the next reuse run starts without them.
		// On a re-run, the import Setups go through CreateOrUpdate, see NotFound,
		// and re-create them — the earlier "verify ... objects" assesses then
		// pass again. This is the built-in regression check for the
		// reuse-cluster idempotency contract.
		Assess("dummy configmaps can be deleted (re-created on next reuse run)", func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
			onboardingCfg, err := clusterutils.OnboardingConfig()
			if err != nil {
				t.Error(err)
				return ctx
			}
			deleteDummyConfigMap(ctx, t, onboardingCfg)
			mcpCfg, err := clusterutils.MCPConfig(ctx, c, "test-mcp")
			if err != nil {
				t.Error(err)
				return ctx
			}
			deleteDummyConfigMap(ctx, t, mcpCfg)
			return ctx
		})
	if !setup.IsReuseMode() {
		basicProviderTest = basicProviderTest.
			Teardown(providers.DeleteMCP("test-mcp", wait.WithTimeout(time.Minute)))
	}
	testenv.Test(t, basicProviderTest.Feature())
}

func assertDummyConfigMap(ctx context.Context, t *testing.T, cfg *envconf.Config) {
	cm := &corev1.ConfigMap{}
	if err := cfg.Client().Resources().Get(ctx, "dummy", corev1.NamespaceDefault, cm); err != nil {
		t.Error(err)
		return
	}
	v, ok := cm.Data["foo"]
	if !ok || v != "bar" {
		t.Errorf("expected foo:bar; got: %t %v", ok, v)
	}
}

func deleteDummyConfigMap(ctx context.Context, t *testing.T, cfg *envconf.Config) {
	cm := &corev1.ConfigMap{}
	cm.SetName("dummy")
	cm.SetNamespace(corev1.NamespaceDefault)
	if err := cfg.Client().Resources().Delete(ctx, cm); err != nil {
		t.Errorf("delete dummy configmap: %v", err)
	}
}
