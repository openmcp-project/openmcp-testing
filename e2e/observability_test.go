package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/openmcp-project/openmcp-testing/pkg/setup/extensions/prometheus"
	"k8s.io/klog/v2"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
)

func TestObservability(t *testing.T) {
	observabilityTest := features.New("observability").
		Assess("average pod cpu and memory usage", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			window := prometheus.ObservabilityWindow{
				Start:   time.Now().Add(-15 * time.Minute),
				End:     time.Now(),
				PodName: "my-pod",
			}
			queries := []prometheus.NamedQuery{
				prometheus.QueryAveragePodCPU,
				prometheus.QueryAveragePodMemoryMiB,
			}
			logs, err := prometheus.Query(ctx, cfg, queries, window, "")
			if err != nil {
				t.Error(err)
			}
			klog.Info(logs)
			return ctx
		})
	testenv.Test(t, observabilityTest.Feature())
}
