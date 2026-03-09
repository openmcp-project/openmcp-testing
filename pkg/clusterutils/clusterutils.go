package clusterutils

import (
	"context"
	"errors"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/e2e-framework/klient"
	"sigs.k8s.io/e2e-framework/klient/k8s"
	k8sresources "sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/kind/pkg/cluster"

	"github.com/openmcp-project/openmcp-testing/pkg/resources"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
)

var errClusterNotFound = errors.New("cluster not found")

type ClusterProvider interface {
	KubeConfig(string, bool) (string, error)
	List() ([]string, error)
}

var clusterProvider = func() ClusterProvider {
	return cluster.NewProvider()
}

type ListResources interface {
	List(ctx context.Context, objs k8s.ObjectList, opts ...k8sresources.ListOption) error
}

var listResources = func(c *envconf.Config) ListResources {
	return c.Client().Resources()
}

// ConfigByPrefix returns an environment Config with the passed in namespace and
// a klient that is set up to interact with the cluster identified by the passed
// in cluster name prefix
func ConfigByPrefix(prefix string, namespace string) (*envconf.Config, error) {
	kind := clusterProvider()
	clusterName, err := retrieveKindClusterNameByPrefix(prefix, kind)
	if err != nil {
		return nil, err
	}
	if clusterName == "" {
		return nil, fmt.Errorf("prefix %s: %w", prefix, errClusterNotFound)
	}
	kubeConfig, err := kind.KubeConfig(clusterName, false)
	if err != nil {
		return nil, err
	}
	restConfig, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeConfig))
	if err != nil {
		return nil, err
	}
	client, err := klient.New(restConfig)
	if err != nil {
		return nil, err
	}
	return envconf.New().WithClient(client).WithNamespace(namespace), nil
}

// OnboardingConfig is a utility function to return an environment config to work
// with the onboarding cluster and default namespace
// In scenarios where you work with multiple onboarding clusters, use ConfigByPrefix instead
func OnboardingConfig() (*envconf.Config, error) {
	return ConfigByPrefix("onboarding", corev1.NamespaceDefault)
}

// McpConfig is a utility function to return an environment config to work
// with the mcp cluster and default namespace.
func MCPConfig(ctx context.Context, platformCluster *envconf.Config, mcpName string) (*envconf.Config, error) {
	mcpCluster, err := retrieveMCPClusterName(ctx, platformCluster, mcpName)
	if err != nil {
		return nil, err
	}
	if mcpCluster == "" {
		return nil, fmt.Errorf("mcp %s: %w", mcpName, errClusterNotFound)
	}
	return ConfigByPrefix(mcpCluster, corev1.NamespaceDefault)
}

func retrieveMCPClusterName(ctx context.Context, platformCluster *envconf.Config, mcpName string) (string, error) {
	cr := &clustersv1alpha1.ClusterRequest{}
	u := &unstructured.UnstructuredList{}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "clusters.openmcp.cloud",
		Version: "v1alpha1",
		Kind:    "ClusterRequest",
	})
	if err := listResources(platformCluster).List(ctx, u); err != nil {
		return "", err
	}
	for _, item := range u.Items {
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, cr); err != nil {
			return "", err
		}
		if cr.GetName() == mcpName {
			return cr.Status.Cluster.Name, nil
		}
	}
	return "", nil
}

func retrieveKindClusterNameByPrefix(prefix string, provider ClusterProvider) (string, error) {
	clusters, err := provider.List()
	if err != nil {
		return "", err
	}
	for _, clusterName := range clusters {
		if strings.HasPrefix(clusterName, prefix) {
			return clusterName, nil
		}
	}
	return "", fmt.Errorf("no cluster found with prefix %s", prefix)
}

// ImportToOnboardingCluster applies a set of resources from a directory to the onboarding cluster
func ImportToOnboardingCluster(ctx context.Context, dir string, options ...wait.Option) (*unstructured.UnstructuredList, error) {
	c, err := OnboardingConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve onboarding cluster config: %v", err)
	}
	return importFromDir(ctx, c, dir, options...)
}

// ImportToMcpCluster applies a set of resources from a directory to the mcp cluster
func ImportToMCPCluster(ctx context.Context, c *envconf.Config, mcpName, dir string, options ...wait.Option) (*unstructured.UnstructuredList, error) {
	mcpConfig, err := MCPConfig(ctx, c, mcpName)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve mcp cluster config: %v", err)
	}
	return importFromDir(ctx, mcpConfig, dir, options...)
}

func importFromDir(ctx context.Context, c *envconf.Config, dir string, options ...wait.Option) (*unstructured.UnstructuredList, error) {
	objList, err := resources.CreateObjectsFromDir(ctx, c, dir)
	if err != nil {
		return nil, fmt.Errorf("failed to create objects from %s: %v", dir, err)
	}
	if options != nil {
		if err := wait.For(conditions.New(c.Client().Resources()).ResourcesFound(objList), options...); err != nil {
			return nil, err
		}
	}
	return objList, nil
}
