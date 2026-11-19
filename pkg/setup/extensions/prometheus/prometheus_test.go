package prometheus

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/types"
)

func Test_applyDefaults(t *testing.T) {
	tests := []struct {
		name  string
		input Prometheus
		want  Prometheus
	}{
		{
			name:  "all defaults applied on zero value",
			input: Prometheus{},
			want: Prometheus{
				Namespace:             defaultNamespace,
				ClusterResourceSuffix: defaultSuffix,
				Image:                 defaultImage,
			},
		},
		{
			name: "existing values not overwritten",
			input: Prometheus{
				Namespace:             "custom-ns",
				ClusterResourceSuffix: "custom-suffix",
				Image:                 "custom-image:latest",
			},
			want: Prometheus{
				Namespace:             "custom-ns",
				ClusterResourceSuffix: "custom-suffix",
				Image:                 "custom-image:latest",
			},
		},
		{
			name: "partial: only namespace set",
			input: Prometheus{
				Namespace: "my-ns",
			},
			want: Prometheus{
				Namespace:             "my-ns",
				ClusterResourceSuffix: "openmcp-testing",
				Image:                 defaultImage,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.input.applyDefaults()
			assert.Equal(t, tt.want, tt.input)
		})
	}
}

func TestTargetCluster_validate(t *testing.T) {
	tests := []struct {
		name    string
		input   TargetCluster
		wantErr bool
	}{
		{
			name:    "missing both control plane and workload cluster",
			input:   TargetCluster{Label: "my-cluster"},
			wantErr: true,
		},
		{
			name: "both control plane and workload cluster set",
			input: TargetCluster{
				ControlPlane:           types.NamespacedName{Name: "test-cp", Namespace: "test-ns"},
				WorkloadClusterRequest: types.NamespacedName{Name: "test-wc", Namespace: "test-ns"},
				Label:                  "my-cluster",
			},
			wantErr: true,
		},
		{
			name:  "only control plane set",
			input: TargetCluster{ControlPlane: types.NamespacedName{Name: "test-cp", Namespace: "test-ns"}, Label: "my-cluster"},
		},
		{
			name:  "only workload cluster request set",
			input: TargetCluster{WorkloadClusterRequest: types.NamespacedName{Name: "test-wc", Namespace: "test-ns"}, Label: "my-cluster"},
		},
		{
			name:    "missing label",
			input:   TargetCluster{ControlPlane: types.NamespacedName{Name: "test-cp", Namespace: "test-ns"}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotErr := tt.input.validate()
			if tt.wantErr {
				assert.Error(t, gotErr)
			} else {
				assert.NoError(t, gotErr)
			}
		})
	}
}
