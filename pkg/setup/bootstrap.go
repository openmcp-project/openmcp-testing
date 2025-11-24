package setup

import (
	"context"
	"time"

	"github.com/christophrj/openmcp-testing/pkg/providers"
	"github.com/christophrj/openmcp-testing/pkg/resources"
	apimachinerytypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/envfuncs"
	"sigs.k8s.io/e2e-framework/pkg/types"
	"sigs.k8s.io/e2e-framework/support/kind"
)

type OpenMCPSetup struct {
	Namespace        string
	Operator         OpenMCPOperatorSetup
	ClusterProviders []providers.ClusterProviderSetup
	ServiceProviders []providers.ServiceProviderSetup
}

type OpenMCPOperatorSetup struct {
	Name         string
	Namespace    string
	Image        string
	Environment  string
	PlatformName string
}

// Bootstrap sets up a the minimum set of components of an openMCP installation
func (s *OpenMCPSetup) Bootstrap(testenv env.Environment) error {
	platformClusterName := envconf.RandomName("platform-cluster", 16)
	s.Operator.Namespace = s.Namespace
	testenv.Setup(createPlatformCluster(platformClusterName)).
		Setup(envfuncs.CreateNamespace(s.Namespace)).
		Setup(s.installOpenMCPOperator()).
		Setup(s.installClusterProviders()).
		Setup(s.installServiceProviders()).
		Setup(s.verifyEnvironment()).
		Finish(s.cleanup()).
		Finish(envfuncs.DestroyCluster(platformClusterName))
	return nil
}

func createPlatformCluster(name string) types.EnvFunc {
	klog.Info("create platform cluster...")
	return envfuncs.CreateClusterWithConfig(kind.NewProvider(), name, "../pkg/setup/kind/config.yaml")
}

func (s *OpenMCPSetup) cleanup() types.EnvFunc {
	return func(ctx context.Context, c *envconf.Config) (context.Context, error) {
		klog.Info("cleaning up environment...")
		for _, sp := range s.ServiceProviders {
			if err := providers.DeleteServiceProvider(ctx, c, sp.Name, wait.WithTimeout(time.Minute)); err != nil {
				klog.Errorf("delete service provider failed: %v", err)
			}
		}
		if err := providers.DeleteCluster(ctx, c, apimachinerytypes.NamespacedName{Namespace: s.Namespace, Name: "onboarding"},
			wait.WithTimeout(time.Second*20)); err != nil {
			klog.Errorf("delete cluster failed: %v", err)
		}
		for _, cp := range s.ClusterProviders {
			if err := providers.DeleteClusterProvider(ctx, c, cp.Name, wait.WithTimeout(time.Minute)); err != nil {
				klog.Errorf("delete cluster provider failed: %v", err)
			}
		}
		return ctx, nil
	}
}

func (s *OpenMCPSetup) verifyEnvironment() types.EnvFunc {
	return func(ctx context.Context, c *envconf.Config) (context.Context, error) {
		klog.Info("verify environment...")
		return ctx, providers.ClusterReady(ctx, c, apimachinerytypes.NamespacedName{Namespace: s.Namespace, Name: "onboarding"},
			wait.WithTimeout(time.Minute))
	}
}

func (s *OpenMCPSetup) installOpenMCPOperator() types.EnvFunc {
	return func(ctx context.Context, c *envconf.Config) (context.Context, error) {
		// apply openmcp operator manifests
		if _, err := resources.CreateObjectsFromTemplateFile(ctx, c, "../pkg/setup/templates/openmcp-operator.yaml", s.Operator); err != nil {
			return ctx, err
		}
		// wait for deployment to be ready
		if err := wait.For(conditions.New(c.Client().Resources()).
			DeploymentAvailable(s.Operator.Name, s.Operator.Namespace),
			wait.WithTimeout(time.Minute)); err != nil {
			return ctx, err
		}
		klog.Info("openmcp operator ready")
		return ctx, nil
	}
}

func (s *OpenMCPSetup) installClusterProviders() env.Func {
	return func(ctx context.Context, c *envconf.Config) (context.Context, error) {
		for _, cp := range s.ClusterProviders {
			if err := providers.InstallClusterProvider(ctx, c, cp); err != nil {
				return ctx, err
			}
		}
		return ctx, nil
	}
}

// InstallServiceProvider creates a service provider object on the platform cluster and waits until it is ready
func (s *OpenMCPSetup) installServiceProviders() env.Func {
	return func(ctx context.Context, c *envconf.Config) (context.Context, error) {
		for _, sp := range s.ServiceProviders {
			if err := providers.InstallServiceProvider(ctx, c, sp); err != nil {
				return ctx, err
			}
		}
		return ctx, nil
	}
}
