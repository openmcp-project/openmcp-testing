package providers

import (
	"context"
	"testing"

	"github.com/christophrj/openmcp-testing/pkg/clusterutils"
	"github.com/christophrj/openmcp-testing/pkg/conditions"
	"github.com/christophrj/openmcp-testing/pkg/resources"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
)

const serviceProviderTemplate = `
apiVersion: openmcp.cloud/v1alpha1
kind: ServiceProvider
metadata:
  name: {{.Name}}
spec:
  image: {{.Image}}
`

// ServiceProviderSetup represents the configuration parameters to set up a service provider
type ServiceProviderSetup struct {
	Name  string
	Image string
	Opts  []wait.Option
}

func serviceProviderRef(name string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetName(name)
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "openmcp.cloud",
		Version: "v1alpha1",
		Kind:    "ServiceProvider",
	})
	return obj
}

// InstallServiceProvider creates a service provider object on the platform cluster and waits until it is ready
func InstallServiceProvider(ctx context.Context, c *envconf.Config, sp ServiceProviderSetup) error {
	klog.Infof("create service provider: %s", sp.Name)
	obj, err := resources.CreateObjectFromTemplate(ctx, c, serviceProviderTemplate, sp)
	if err != nil {
		return err
	}
	return wait.For(conditions.Match(obj, c, "Ready", corev1.ConditionTrue), sp.Opts...)
}

// ImportServiceProviderAPIs iterates over each resource from the passed in directory
// and applies it to the onboarding cluster
func ImportServiceProviderAPIs(directory string, opts ...wait.Option) features.Func {
	return func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
		klog.Infof("apply service provider resources to onboarding cluster from %s ...", directory)
		if _, err := clusterutils.ImportToOnboardingCluster(ctx, directory, opts...); err != nil {
			t.Error(err)
		}
		return ctx
	}
}

// ImportDomainAPIs iterates over each resource from the passed in directory
// and applies it to a MCP cluster
func ImportDomainAPIs(directory string, opts ...wait.Option) features.Func {
	return func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
		klog.Infof("apply service provider resources to MCP cluster from %s ...", directory)
		if _, err := clusterutils.ImportToMcpCluster(ctx, directory, opts...); err != nil {
			t.Error(err)
		}
		return ctx
	}
}

// DeleteServiceProvider deletes the service provider object on the platform cluster and waits until the object has been deleted
func DeleteServiceProvider(ctx context.Context, c *envconf.Config, name string, opts ...wait.Option) error {
	klog.Infof("delete service provider: %s", name)
	return resources.DeleteObject(ctx, c, serviceProviderRef(name), opts...)
}
