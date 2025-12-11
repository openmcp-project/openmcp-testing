package setup

import (
	"context"
	"embed"
	"os"

	apimachinerytypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/envfuncs"
	"sigs.k8s.io/e2e-framework/pkg/types"
	"sigs.k8s.io/e2e-framework/support/kind"

	"github.com/openmcp-project/openmcp-testing/internal"
	"github.com/openmcp-project/openmcp-testing/pkg/providers"
	"github.com/openmcp-project/openmcp-testing/pkg/resources"
)

//go:embed config/*
var configFS embed.FS

type OpenMCPSetup struct {
	Namespace        string
	Operator         OpenMCPOperatorSetup
	ClusterProviders []providers.ClusterProviderSetup
	ServiceProviders []providers.ServiceProviderSetup
	WaitOpts         []wait.Option
}

type OpenMCPOperatorSetup struct {
	Name         string
	Namespace    string
	Image        string
	Environment  string
	PlatformName string
	WaitOpts     []wait.Option
}

// Bootstrap sets up a the minimum set of components of an openMCP installation
func (s *OpenMCPSetup) Bootstrap(testenv env.Environment) error {
	kindConfig := internal.MustTmpFileFromEmbedFS(configFS, "config/kind-config.yaml")
	operatorTemplate := internal.MustTmpFileFromEmbedFS(configFS, "config/operator.yaml.tmpl")
	platformClusterName := envconf.RandomName("platform", 16)
	s.Operator.Namespace = s.Namespace
	testenv.Setup(createPlatformCluster(platformClusterName, kindConfig)).
		Setup(envfuncs.CreateNamespace(s.Namespace)).
		Setup(s.installOpenMCPOperator(operatorTemplate)).
		Setup(s.installClusterProviders()).
		Setup(s.loadServiceProviderImages(platformClusterName)).
		Setup(s.installServiceProviders()).
		Setup(s.verifyEnvironment()).
		Finish(s.cleanup(kindConfig, operatorTemplate)).
		Finish(envfuncs.DestroyCluster(platformClusterName))
	return nil
}

func createPlatformCluster(name string, kindConfig string) types.EnvFunc {
	klog.Info("create platform cluster...")
	return envfuncs.CreateClusterWithConfig(kind.NewProvider(), name, kindConfig)
}

func (s *OpenMCPSetup) cleanup(tmpFiles ...string) types.EnvFunc {
	return func(ctx context.Context, c *envconf.Config) (context.Context, error) {
		klog.Info("cleaning up environment...")
		for _, f := range tmpFiles {
			os.RemoveAll(f)
		}
		for _, sp := range s.ServiceProviders {
			if err := providers.DeleteServiceProvider(ctx, c, sp.Name, sp.WaitOpts...); err != nil {
				klog.Errorf("delete service provider failed: %v", err)
			}
		}
		if err := providers.DeleteCluster(ctx, c, apimachinerytypes.NamespacedName{Namespace: s.Namespace, Name: "onboarding"},
			s.WaitOpts...); err != nil {
			klog.Errorf("delete cluster failed: %v", err)
		}
		for _, cp := range s.ClusterProviders {
			if err := providers.DeleteClusterProvider(ctx, c, cp.Name, cp.WaitOpts...); err != nil {
				klog.Errorf("delete cluster provider failed: %v", err)
			}
		}
		return ctx, nil
	}
}

func (s *OpenMCPSetup) verifyEnvironment() types.EnvFunc {
	return func(ctx context.Context, c *envconf.Config) (context.Context, error) {
		klog.Info("verify environment...")
		return ctx, providers.ClustersReady(ctx, c, s.WaitOpts...)
	}
}

func (s *OpenMCPSetup) installOpenMCPOperator(tmpl string) types.EnvFunc {
	return func(ctx context.Context, c *envconf.Config) (context.Context, error) {
		// apply openmcp operator manifests
		if _, err := resources.CreateObjectsFromTemplateFile(ctx, c, tmpl, s.Operator); err != nil {
			return ctx, err
		}
		// wait for deployment to be ready
		if err := wait.For(conditions.New(c.Client().Resources()).
			DeploymentAvailable(s.Operator.Name, s.Operator.Namespace), s.Operator.WaitOpts...); err != nil {
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

func (s *OpenMCPSetup) loadServiceProviderImages(platformCluster string) env.Func {
	funcs := []env.Func{}
	for _, sp := range s.ServiceProviders {
		funcs = append(funcs, envfuncs.LoadDockerImageToCluster(platformCluster, sp.Image))
	}
	return Compose(funcs...)
}

// Compose executes multiple env.Funcs in a row
func Compose(envfuncs ...env.Func) env.Func {
	return func(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
		for _, envfunc := range envfuncs {
			var err error
			if ctx, err = envfunc(ctx, cfg); err != nil {
				return ctx, err
			}
		}
		return ctx, nil
	}
}
