package setup

import (
	"context"
	"embed"
	"fmt"
	"os"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	apimachinerytypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/envfuncs"
	"sigs.k8s.io/e2e-framework/pkg/types"
	"sigs.k8s.io/e2e-framework/support/kind"

	"github.com/openmcp-project/openmcp-testing/pkg/platformservices"
	"github.com/openmcp-project/openmcp-testing/pkg/setup/extensions"

	"github.com/openmcp-project/openmcp-testing/internal"
	"github.com/openmcp-project/openmcp-testing/pkg/providers"
	"github.com/openmcp-project/openmcp-testing/pkg/resources"
)

//go:embed config/*
var configFS embed.FS

// DefaultPlatformClusterName is the kind cluster name used when
// OpenMCPSetup.PlatformClusterName is empty. Override the field in your
// TestMain if you need a per-project name (e.g. to avoid collisions across
// consumer repos sharing one machine).
const DefaultPlatformClusterName = "e2e-platform"

type OpenMCPSetup struct {
	Namespace string
	// PlatformClusterName overrides the kind cluster name used for the
	// platform cluster. Empty means DefaultPlatformClusterName.
	PlatformClusterName string
	Operator            OpenMCPOperatorSetup
	ClusterProviders    []providers.ClusterProviderSetup
	ServiceProviders    []providers.ServiceProviderSetup
	PlatformServices    []platformservices.PlatformServiceSetup
	Extensions          []extensions.Extension
	WaitOpts            []wait.Option
}

type OpenMCPOperatorSetup struct {
	Name         string
	Namespace    string
	Image        string
	Environment  string
	PlatformName string
	WaitOpts     []wait.Option
	// LoadImageToCluster allows using local images that have to be loaded into the kind cluster
	LoadImageToCluster bool
}

// Bootstrap sets up the minimum set of components of an openMCP installation and returns the platform cluster name.
//
// The platform kind cluster name comes from OpenMCPSetup.PlatformClusterName
// (default DefaultPlatformClusterName). When E2E_REUSE_CLUSTER is truthy, the
// kind cluster is reused across runs (kind's own Create handles the existence
// case) and the framework Finish/cleanup hooks are not registered so the
// cluster survives.
func (s *OpenMCPSetup) Bootstrap(testenv env.Environment) string {
	kindConfig := internal.MustTmpFileFromEmbedFS(configFS, "config/kind-config.yaml")
	operatorTemplate := internal.MustTmpFileFromEmbedFS(configFS, "config/operator.yaml.tmpl")
	platformClusterName := s.PlatformClusterName
	if platformClusterName == "" {
		platformClusterName = DefaultPlatformClusterName
	}
	reuseMode := IsReuseMode()
	if reuseMode {
		klog.Infof("reuse mode on: kind cluster %q (%s=true)", platformClusterName, EnvReuseCluster)
	}
	s.Operator.Namespace = s.Namespace
	testenv.Setup(createPlatformCluster(platformClusterName, kindConfig)).
		Setup(createNamespaceIdempotent(s.Namespace)).
		Setup(s.loadImagesToCluster(platformClusterName)).
		Setup(s.installOpenMCPOperator(operatorTemplate)).
		Setup(s.installClusterProviders()).
		Setup(s.managePlatformCluster(platformClusterName)).
		Setup(s.installExtensions()).
		Setup(s.verifyEnvironment()).
		Setup(s.installPlatformServices()).
		Setup(s.installServiceProviders())
	if !reuseMode {
		testenv.
			Finish(s.cleanup(kindConfig, operatorTemplate)).
			Finish(envfuncs.DestroyCluster(platformClusterName))
	}
	return platformClusterName
}

func createPlatformCluster(name string, kindConfig string) types.EnvFunc {
	klog.Info("create platform cluster...")
	return envfuncs.CreateClusterWithConfig(kind.NewProvider(), name, kindConfig)
}

// createNamespaceIdempotent wraps envfuncs.CreateNamespace so a re-run against
// an existing cluster does not error on AlreadyExists. It mirrors what
// envfuncs.CreateNamespace puts on the env config (default namespace) so
// downstream Setup steps see the same state regardless.
func createNamespaceIdempotent(name string) types.EnvFunc {
	return func(ctx context.Context, c *envconf.Config) (context.Context, error) {
		ctx, err := envfuncs.CreateNamespace(name)(ctx, c)
		if err != nil && apierrors.IsAlreadyExists(err) {
			c.WithNamespace(name)
			return ctx, nil
		}
		return ctx, err
	}
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
		for _, ps := range s.PlatformServices {
			if err := platformservices.DeletePlatformService(ctx, c, ps.Name); err != nil {
				klog.Errorf("delete platform service failed: %v", err)
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

func (s *OpenMCPSetup) managePlatformCluster(platformClusterName string) env.Func {
	return func(ctx context.Context, c *envconf.Config) (context.Context, error) {
		if len(s.ClusterProviders) == 0 {
			return ctx, fmt.Errorf("no cluster providers found")
		}

		// Use the first cluster provider for the platform cluster
		// TODO: Consider adding explicit PlatformClusterProvider field to OpenMCPSetup
		platformClusterClusterProvider := s.ClusterProviders[0]

		// Currently only kind provider is supported for platform cluster management
		if platformClusterClusterProvider.Name != "kind" {
			klog.Warningf("platform cluster provider type '%s' is not 'kind', skipping platform cluster resource creation", platformClusterClusterProvider.Name)
			return ctx, nil
		}

		klog.Info("create platform cluster resource...")

		platformCluster := &unstructured.Unstructured{
			Object: map[string]any{
				"apiVersion": "clusters.openmcp.cloud/v1alpha1",
				"kind":       "Cluster",
				"metadata": map[string]any{
					"name":      "platform",
					"namespace": s.Namespace,
					"annotations": map[string]any{
						"kind.clusters.openmcp.cloud/name": platformClusterName,
					},
				},
				"spec": map[string]any{
					"kubernetes": map[string]any{},
					"profile":    "kind",
					"purposes": []any{
						clustersv1alpha1.PURPOSE_PLATFORM,
					},
					"tenancy": string(clustersv1alpha1.TENANCY_SHARED),
				},
			},
		}

		// Create the platform cluster object in Kubernetes
		if createErr := resources.CreateOrUpdate(ctx, c, platformCluster); createErr != nil {
			return ctx, createErr
		}

		klog.Info("platform cluster resource created")
		return ctx, nil
	}
}

func (s *OpenMCPSetup) installExtensions() env.Func {
	return func(ctx context.Context, c *envconf.Config) (context.Context, error) {
		klog.Info("install extensions...")
		for _, ext := range s.Extensions {
			klog.Infof("install extension %s", ext.Name())
			if installErr := ext.Install(ctx, c); installErr != nil {
				return ctx, fmt.Errorf("install extension %s failed: %v", ext.Name(), installErr)
			}
			if schemeErr := ext.RegisterSchemes(ctx, c.Client().Resources().GetScheme()); schemeErr != nil {
				return ctx, fmt.Errorf("install extension scheme %s failed: %v", ext.Name(), schemeErr)
			}
		}
		return ctx, nil
	}
}

func (s *OpenMCPSetup) installPlatformServices() env.Func {
	return func(ctx context.Context, c *envconf.Config) (context.Context, error) {
		for _, ps := range s.PlatformServices {
			if err := platformservices.InstallPlatformService(ctx, c, ps); err != nil {
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

func (s *OpenMCPSetup) loadImagesToCluster(platformCluster string) env.Func {
	funcs := []env.Func{}
	if s.Operator.LoadImageToCluster {
		funcs = append(funcs, envfuncs.LoadDockerImageToCluster(platformCluster, s.Operator.Image))
	}
	for _, cp := range s.ClusterProviders {
		if cp.LoadImageToCluster {
			funcs = append(funcs, envfuncs.LoadDockerImageToCluster(platformCluster, cp.Image))
		}
	}
	for _, sp := range s.ServiceProviders {
		if sp.LoadImageToCluster {
			funcs = append(funcs, envfuncs.LoadDockerImageToCluster(platformCluster, sp.Image))
		}
	}
	for _, cp := range s.PlatformServices {
		if cp.LoadImageToCluster {
			funcs = append(funcs, envfuncs.LoadDockerImageToCluster(platformCluster, cp.Image))
		}
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
