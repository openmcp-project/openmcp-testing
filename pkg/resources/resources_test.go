package resources_test

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/klog/v2"
	"sigs.k8s.io/e2e-framework/klient/decoder"
	"sigs.k8s.io/e2e-framework/pkg/envconf"

	"github.com/openmcp-project/openmcp-testing/pkg/resources"
)

func TestMain(m *testing.M) {
	// required to redirect log writes to a buffer (see captureLogs)
	klog.InitFlags(nil)
	_ = flag.Set("logtostderr", "false")
	klog.SetOutput(io.Discard)
	os.Setenv("RESOURCES_DRY_RUN", "true")
	os.Exit(m.Run())
}

type templateData struct {
	Name      string
	Namespace string
}

// redirect logs to a buffer to enable basic assertions based on the log output
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	klog.SetOutput(io.MultiWriter(buf, os.Stderr))
	t.Cleanup(func() { klog.SetOutput(io.Discard) })
	return buf
}

func TestMutateOptionsViaTemplateFile(t *testing.T) {
	tests := []struct {
		name       string
		namespace  string
		nameSuffix string
	}{
		{
			name:       "appends suffix to object name on creation",
			namespace:  "test",
			nameSuffix: "-testsuffix",
		},
		{
			name:       "empty suffix results in no name change",
			namespace:  "test",
			nameSuffix: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := captureLogs(t)
			cfg := envconf.New().WithNamespace("test-ns")
			data := templateData{Name: "base", Namespace: "test-ns"}
			_, err := resources.CreateObjectsFromTemplateFile(context.Background(), cfg, "testdata/configmap.tmpl", data, resources.MutateName(tt.nameSuffix), decoder.MutateNamespace(tt.namespace))
			require.NoError(t, err)
			// assert name change based on captured logs
			assert.Contains(t, logs.String(), tt.namespace+"/base"+tt.nameSuffix)
		})
	}
}

func TestCreateNObjectsFromTemplateFile(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		count    int
		wantErr  bool
	}{
		{
			name:     "missing template file returns io error",
			filePath: "testdata/nonexistent.tmpl",
			count:    1,
			wantErr:  true,
		},
		{
			name:     "zero count succeeds results in no resources",
			filePath: "testdata/nonexistent.tmpl",
			count:    0,
			wantErr:  false,
		},
		{
			name:     "count results in multiple resources",
			filePath: "testdata/configmap.tmpl",
			count:    2,
			wantErr:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := captureLogs(t)
			cfg := envconf.New().WithNamespace("test-ns")
			data := templateData{Name: "test", Namespace: "default"}
			gotErr := resources.CreateNObjectsFromTemplateFile(t.Context(), cfg, tt.filePath, tt.count, data)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("CreateNObjectsFromTemplateFile() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("CreateNObjectsFromTemplateFile() succeeded unexpectedly")
			}
			if tt.count < 1 {
				// nothing has been created
				assert.Equal(t, logs.Len(), 0)
				return
			}
			// assert name and namespace based on captured logs
			for i := 0; i < tt.count; i++ {
				assert.Contains(t, logs.String(), fmt.Sprintf("%s/%s-%d", data.Namespace, data.Name, i))
			}
		})
	}
}
