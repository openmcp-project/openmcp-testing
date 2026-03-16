package platformservices

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/pkg/envconf"

	"github.com/openmcp-project/openmcp-testing/pkg/clusterutils"
	"github.com/openmcp-project/openmcp-testing/pkg/conditions"
	"github.com/openmcp-project/openmcp-testing/pkg/resources"
)

const platformServiceTemplate = `
apiVersion: openmcp.cloud/v1alpha1
kind: PlatformService
metadata:
  name: {{.Name}}
spec:
  image: {{.Image}}
`

// PlatformServiceSetup represents the configuration parameters to set up a platform service
type PlatformServiceSetup struct {
	Name     string
	Image    string
	WaitOpts []wait.Option
	// LoadImageToCluster allows using local images that have to be loaded into the kind cluster
	LoadImageToCluster bool
	// PlatformServiceConfigsDir is an optional directory containing the platform service specific
	// resources that shall be applied during deployment of the platform service
	PlatformServiceConfigsDir string
}

func platformServiceRef(name string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetName(name)
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "openmcp.cloud",
		Version: "v1alpha1",
		Kind:    "PlatformService",
	})
	return obj
}

// InstallPlatformService creates a platform service object on the platform cluster and waits until it is ready
func InstallPlatformService(ctx context.Context, c *envconf.Config, ps PlatformServiceSetup) error {
	klog.Infof("create platform service: %s", ps.Name)
	obj, err := resources.CreateObjectFromTemplate(ctx, c, platformServiceTemplate, ps)
	if err != nil {
		return err
	}
	if len(ps.PlatformServiceConfigsDir) > 0 {
		// Import platform service configs with retry logic
		// The wait.For with ps.WaitOpts provides the timeout and retry mechanism
		waitForImportErr := wait.For(func(ctx context.Context) (done bool, err error) {
			if _, importErr := clusterutils.ImportToPlatformCluster(ctx, c, ps.PlatformServiceConfigsDir, ps.WaitOpts...); importErr != nil {
				klog.Infof("failed to import platform service configs, will retry: %v", importErr)
				// Return false to retry, but don't return error to allow retries
				return false, nil
			}
			klog.Infof("successfully imported platform service configs for %s", ps.Name)
			return true, nil
		}, ps.WaitOpts...)
		if waitForImportErr != nil {
			return waitForImportErr
		}
	}
	return wait.For(conditions.Match(obj, c, "Ready", corev1.ConditionTrue), ps.WaitOpts...)
}

// DeletePlatformService deletes the platform service object on the platform cluster and waits until the object has been deleted
func DeletePlatformService(ctx context.Context, c *envconf.Config, name string, opts ...wait.Option) error {
	klog.Infof("delete platform service: %s", name)
	return resources.DeleteObject(ctx, c, platformServiceRef(name), opts...)
}
