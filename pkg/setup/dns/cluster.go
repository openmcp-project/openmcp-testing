package dns

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/pkg/envconf"

	"github.com/openmcp-project/openmcp-testing/pkg/providers"
	"github.com/openmcp-project/openmcp-testing/pkg/resources"

	openmcpconditions "github.com/openmcp-project/openmcp-testing/pkg/conditions"
)

const clusterRequestTemplate = `
apiVersion: clusters.openmcp.cloud/v1alpha1
kind: ClusterRequest
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
spec:
  purpose: {{.Purpose}}
`

const accessRequestTemplate = `
apiVersion: clusters.openmcp.cloud/v1alpha1
kind: AccessRequest
metadata:
  name: {{.Name}}
  namespace: {{.Namespace}}
spec:
  requestRef:
    name: {{.RequestName}}
    namespace: {{.Namespace}}
  token:
    roleRefs:
    - kind: ClusterRole
      name: cluster-admin
`

type ClusterRequest struct {
	Name      string
	Namespace string
	Purpose   string
}

type accessRequest struct {
	Name        string
	Namespace   string
	RequestName string
}

func createCluster(ctx context.Context, config *envconf.Config, cr ClusterRequest) error {
	crObj, err := resources.CreateObjectFromTemplate(ctx, config, clusterRequestTemplate, cr)
	if err != nil {
		return fmt.Errorf("failed to create cluster request: %v", err)
	}
	if err := wait.For(openmcpconditions.Status(crObj, config, "phase", "Granted")); err != nil {
		return fmt.Errorf("cluster request failed to get ready: %v", err)
	}
	if err := providers.ClustersReady(ctx, config); err != nil {
		return fmt.Errorf("MCP cluster failed to get ready: %v", err)
	}
	ar := accessRequest{
		Name:        cr.Name,
		Namespace:   cr.Namespace,
		RequestName: cr.Name,
	}
	arObj, err := resources.CreateObjectFromTemplate(ctx, config, accessRequestTemplate, ar)
	if err != nil {
		return fmt.Errorf("failed to create access request: %v", err)
	}
	if err := wait.For(openmcpconditions.Status(arObj, config, "phase", "Granted")); err != nil {
		return fmt.Errorf("access request failed to get ready: %v", err)
	}
	if err := wait.For(func(ctx context.Context) (bool, error) {
		if err := config.Client().Resources().Get(ctx, ar.Name, ar.Namespace, arObj); err != nil {
			return false, err
		}
		// TODO verify this works as expected
		_, found, err := unstructured.NestedFieldNoCopy(arObj.Object, "Status", "SecretRef")
		return found, err
	}); err != nil {
		return fmt.Errorf("failed to retrieve kubeconfig of dns access request")
	}
	return nil
}
