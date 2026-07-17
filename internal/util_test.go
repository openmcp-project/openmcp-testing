package internal_test

import (
	"bufio"
	"bytes"
	"embed"
	"os"
	"testing"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openmcp-project/openmcp-testing/internal"
	"github.com/openmcp-project/openmcp-testing/pkg/providers"
	"github.com/openmcp-project/openmcp-testing/pkg/setup"
)

//go:embed testdata/*
var testFS embed.FS

func TestMustTmpFileFromEmbedFS(t *testing.T) {
	result := internal.MustTmpFileFromEmbedFS(testFS, "testdata/test.txt")
	file, err := os.Open(result)
	require.NoError(t, err)
	scanner := bufio.NewScanner(file)
	require.True(t, scanner.Scan())
	require.Equal(t, scanner.Text(), "hello openmcp!")
	os.RemoveAll(result)
}

func TestExecTemplateFile(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		filePath string
		data     interface{}
		wantFile string
		wantErr  bool
	}{
		{
			name:     "test openmcp-operator purpose mapping",
			filePath: "testdata/config.tmpl",
			wantFile: "testdata/expected_config.yaml",
			data: setup.OpenMCPOperatorSetup{
				Name:      "openmcp-operator",
				Namespace: "openmcp-system",
				ExtraClusterPurposeMapping: []providers.ClusterPurposeMapping{
					{
						Purpose: "test1",
						Profile: "profile1",
						Tenancy: clustersv1alpha1.TENANCY_SHARED,
					},
					{
						Purpose: "test2",
						Profile: "profile2",
						Tenancy: clustersv1alpha1.TENANCY_EXCLUSIVE,
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotErr := internal.ExecTemplateFile(tt.filePath, tt.data)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("ExecTemplateFile() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("ExecTemplateFile() succeeded unexpectedly")
			}
			// compare to expected file
			expected, err := os.ReadFile(tt.wantFile)
			require.NoError(t, err)
			assert.Equal(t, bytes.NewBuffer(expected).String(), got)
		})
	}
}
