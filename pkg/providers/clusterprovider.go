package providers

import (
	"context"
	"testing"
	"time"

	"github.com/christophrj/openmcp-testing/internal"
	"github.com/christophrj/openmcp-testing/pkg/clusterutils"
	"github.com/christophrj/openmcp-testing/pkg/conditions"
	"github.com/christophrj/openmcp-testing/pkg/resources"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
)

const clusterProviderTemplate = `
apiVersion: openmcp.cloud/v1alpha1
kind: ClusterProvider
metadata:
  name: {{.Name}}
spec:
  image: {{.Image}}
  extraVolumeMounts:
    - mountPath: /var/run/docker.sock
      name: docker
  extraVolumes:
    - name: docker
      hostPath:
        path: /var/run/host-docker.sock
        type: Socket
`

const mcpTemplate = `
apiVersion: core.openmcp.cloud/v2alpha1
kind: ManagedControlPlaneV2
metadata:
  name: {{.Name}}
spec:
  iam: {}
`

// ClusterProviderSetup represents the configuration parameters to set up a cluster provider
type ClusterProviderSetup struct {
	Name  string
	Image string
	Opts  []wait.Option
}

func mcpRef(ref types.NamespacedName) *unstructured.Unstructured {
	return internal.UnstructuredRef(ref.Name, ref.Namespace, schema.GroupVersionKind{
		Group:   "core.openmcp.cloud",
		Version: "v2alpha1",
		Kind:    "managedcontrolplanev2",
	})
}

func clusterRef(ref types.NamespacedName) *unstructured.Unstructured {
	return internal.UnstructuredRef(ref.Name, ref.Namespace, schema.GroupVersionKind{
		Group:   "clusters.openmcp.cloud",
		Version: "v1alpha1",
		Kind:    "cluster",
	})
}

func clusterProviderRef(name string) *unstructured.Unstructured {
	return internal.UnstructuredRef(name, "", schema.GroupVersionKind{
		Group:   "openmcp.cloud",
		Version: "v1alpha1",
		Kind:    "clusterprovider",
	})
}

// InstallClusterProvider creates a cluster provider object on the platform cluster and waits until it is ready
func InstallClusterProvider(ctx context.Context, c *envconf.Config, clusterProvider ClusterProviderSetup) error {
	klog.Infof("create cluster provider %s", clusterProvider.Name)
	obj, err := resources.CreateObjectFromTemplate(ctx, c, clusterProviderTemplate, clusterProvider)
	if err != nil {
		return err
	}
	return wait.For(conditions.Match(obj, c, "Ready", corev1.ConditionTrue), clusterProvider.Opts...)
}

// DeleteClusterProvider deletes the cluster provider object and waits until the object has been deleted
func DeleteClusterProvider(ctx context.Context, c *envconf.Config, name string, opts ...wait.Option) error {
	klog.Infof("delete cluster provider: %s", name)
	return resources.DeleteObject(ctx, c, clusterProviderRef(name), opts...)
}

// CreateMCP creates an MCP object on the onboarding cluster and waits until it is ready
func CreateMCP(name string, timeout time.Duration) features.Func {
	return func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
		klog.Infof("create MCP: %s", name)
		onboardingCfg, err := clusterutils.OnboardingConfig()
		if err != nil {
			t.Error(err)
			return ctx
		}
		obj, err := resources.CreateObjectFromTemplate(ctx, onboardingCfg, mcpTemplate, struct{ Name string }{Name: name})
		if err != nil {
			t.Errorf("failed to create MCP: %v", err)
			return ctx
		}
		if err := wait.For(
			conditions.Status(obj, onboardingCfg, "phase", "Ready"),
			wait.WithTimeout(timeout),
		); err != nil {
			t.Errorf("MCP failed to get ready: %v", err)
		}
		return ctx
	}
}

// DeleteMCP deletes the MCP object on the onboarding cluster and waits until the object has been deleted
func DeleteMCP(name string, opts ...wait.Option) features.Func {
	return func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
		klog.Infof("delete MCP: %s", name)
		onboardingCfg, err := clusterutils.OnboardingConfig()
		if err != nil {
			t.Error(err)
			return ctx
		}
		mcp := mcpRef(types.NamespacedName{
			Namespace: corev1.NamespaceDefault,
			Name:      name,
		})
		err = resources.DeleteObject(ctx, onboardingCfg, mcp, opts...)
		if err != nil {
			t.Errorf("failed to delete MCP %s: %v", name, err)
			return ctx
		}
		return ctx
	}
}

// ClusterReady returns true if the referenced cluster object is ready
func ClusterReady(ctx context.Context, c *envconf.Config, ref types.NamespacedName, options ...wait.Option) error {
	if err := wait.For(conditions.Match(clusterRef(ref), c, "Ready", corev1.ConditionTrue), options...); err != nil {
		return err
	}
	klog.Infof("cluster ready: %s", ref)
	return nil
}

// DeleteCluster deletes the referenced cluster object
func DeleteCluster(ctx context.Context, c *envconf.Config, ref types.NamespacedName, options ...wait.Option) error {
	klog.Infof("delete cluster: %s", ref)
	return resources.DeleteObject(ctx, c, clusterRef(ref), options...)
}
