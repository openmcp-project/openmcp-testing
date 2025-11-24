package clusterutils

import (
	"context"
	"fmt"
	"strings"

	"github.com/christophrj/openmcp-testing/pkg/resources"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/e2e-framework/klient"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/kind/pkg/cluster"
)

// ConfigByPrefix returns an environment Config with the passed in namespace and
// a klient that is set up to interact with the cluster identified by the passed
// in cluster name prefix
func ConfigByPrefix(prefix string, namespace string) (*envconf.Config, error) {
	kind := cluster.NewProvider()
	clusterName, err := retrieveKindClusterNameByPrefix(prefix)
	if err != nil {
		return nil, err
	}
	kubeConfig, err := kind.KubeConfig(clusterName, false)
	if err != nil {
		return nil, err
	}
	restConfig, err := clientcmd.RESTConfigFromKubeConfig([]byte(kubeConfig))
	if err != nil {
		return nil, err
	}
	onboardingClient, err := klient.New(restConfig)
	if err != nil {
		return nil, err
	}
	return envconf.New().WithClient(onboardingClient).WithNamespace(namespace), nil
}

// OnboardingConfig is a utility function to return an environment config to work
// with the onboarding cluster and default namespace
// In scenarios where you work with multiple onboarding clusters, use ConfigByPrefix instead
func OnboardingConfig() (*envconf.Config, error) {
	return ConfigByPrefix("onboarding", corev1.NamespaceDefault)
}

// McpConfig is a utility function to return an environment config to work
// with the mcp cluster and default namespace.
// In scenarios where you work with multiple MCPs, use ConfigByPrefix instead
func McpConfig() (*envconf.Config, error) {
	return ConfigByPrefix("mcp", corev1.NamespaceDefault)
}

func retrieveKindClusterNameByPrefix(prefix string) (string, error) {
	kind := cluster.NewProvider()
	clusters, err := kind.List()
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
func ImportToMcpCluster(ctx context.Context, dir string, options ...wait.Option) (*unstructured.UnstructuredList, error) {
	c, err := McpConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve mcp cluster config: %v", err)
	}
	return importFromDir(ctx, c, dir, options...)
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
