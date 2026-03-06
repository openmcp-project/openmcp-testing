package clusterutils

import (
	"context"
	"testing"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	"github.com/openmcp-project/openmcp-operator/api/common"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
)

const fakeKubeconfig = `
apiVersion: v1
kind: Config
clusters:
- name: test-cluster
  cluster:
    server: https://127.0.0.1:6443
contexts:
- name: test-context
  context:
    cluster: test-cluster
    user: test-user
current-context: test-context
users:
- name: test-user
  user:
    token: fake-token
`

var _ ClusterProvider = fakeClusterProvider{}

type fakeClusterProvider struct {
	kubeconfig string
	clusters   []string
}

// KubeConfig implements [ClusterProvider].
func (f fakeClusterProvider) KubeConfig(string, bool) (string, error) {
	return f.kubeconfig, nil
}

// List implements [ClusterProvider].
func (f fakeClusterProvider) List() ([]string, error) {
	return f.clusters, nil
}

var _ ListResources = fakeListResources{}

type fakeListResources struct {
	objs k8s.ObjectList
}

// List implements [ListResources].
func (f fakeListResources) List(ctx context.Context, objs k8s.ObjectList, opts ...resources.ListOption) error {
	objList := objs.(*unstructured.UnstructuredList)
	return meta.EachListItem(f.objs, func(o runtime.Object) error {
		u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(o)
		if err != nil {
			return err
		}
		objList.Items = append(objList.Items, unstructured.Unstructured{
			Object: u,
		})
		return nil
	})
}

func TestMCPConfig(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		mcpName string
		wantErr error
	}{
		{
			name:    "MCP found",
			mcpName: "test-mcp",
			wantErr: nil,
		},
		{
			name:    "MCP not found",
			mcpName: "not-found",
			wantErr: errClusterNotFound,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clusterProvider = func() ClusterProvider {
				return fakeClusterProvider{
					kubeconfig: fakeKubeconfig,
					clusters:   []string{"test-mcp"},
				}
			}
			listResources = func(c *envconf.Config) ListResources {
				return fakeListResources{
					objs: &clustersv1alpha1.ClusterRequestList{
						Items: []clustersv1alpha1.ClusterRequest{
							{
								ObjectMeta: metav1.ObjectMeta{
									Name: "test-mcp",
								},
								Spec: clustersv1alpha1.ClusterRequestSpec{},
								Status: clustersv1alpha1.ClusterRequestStatus{
									Cluster: &common.ObjectReference{
										Name: "test-mcp",
									},
								},
							},
						},
					},
				}
			}
			_, gotErr := MCPConfig(context.Background(), envconf.New(), tt.mcpName)
			if gotErr != nil {
				assert.NotNil(t, tt.wantErr)
				assert.ErrorIs(t, gotErr, tt.wantErr)
				return
			}
			if tt.wantErr != nil {
				t.Fatal("MCPConfig() succeeded unexpectedly")
			}
		})
	}
}
