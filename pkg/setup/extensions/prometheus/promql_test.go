package prometheus

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_duration(t *testing.T) {
	tests := []struct {
		name string
		w    ObservabilityWindow
		want string
	}{
		{
			name: "15 minutes",
			w: ObservabilityWindow{
				Start: time.Unix(0, 0),
				End:   time.Unix(900, 0),
			},
			want: "900s",
		},
		{
			name: "subsecond truncated (theoretical - doesn't make sense in practice)",
			w: ObservabilityWindow{
				Start: time.Unix(0, 0),
				End:   time.Unix(0, 500_000_000),
			},
			want: "0s",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.w.duration())
		})
	}
}

func Test_validate(t *testing.T) {
	tests := []struct {
		name    string
		p       QueryParameters
		wantErr string
	}{
		{
			name: "valid",
			p: QueryParameters{
				JobName: "test",
				Window:  ObservabilityWindow{Start: time.Unix(0, 0), End: time.Unix(900, 0)},
			},
		},
		{
			name: "missing job name",
			p: QueryParameters{
				Window: ObservabilityWindow{Start: time.Unix(0, 0), End: time.Unix(900, 0)},
			},
			wantErr: "jobname must be provided",
		},
		{
			name: "zero start",
			p: QueryParameters{
				JobName: "test",
				Window:  ObservabilityWindow{End: time.Unix(900, 0)},
			},
			wantErr: "window start must be provided",
		},
		{
			name: "zero end",
			p: QueryParameters{
				JobName: "test",
				Window:  ObservabilityWindow{Start: time.Unix(900, 0)},
			},
			wantErr: "window end must be provided",
		},
		{
			name: "end before start",
			p: QueryParameters{
				JobName: "test",
				Window:  ObservabilityWindow{Start: time.Unix(1, 0), End: time.Unix(0, 0)},
			},
			wantErr: "start must be before end",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.p.validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tt.wantErr)
			}
		})
	}
}

func Test_QueryResult_String(t *testing.T) {
	tests := []struct {
		name   string
		result QueryResult
		want   string
	}{
		{
			name:   "empty result",
			result: QueryResult{Name: "my query"},
			want:   "my query: ",
		},
		{
			name: "result with no values",
			result: QueryResult{
				Name:   "my query",
				Result: model.Matrix{&model.SampleStream{}},
			},
			want: "my query: ",
		},
		{
			name: "result with single value",
			result: QueryResult{
				Name: "my query",
				Result: model.Matrix{
					&model.SampleStream{
						Values: []model.SamplePair{{Value: 42.5}},
					},
				},
			},
			want: "my query: 42.5",
		},
		{
			name: "result with multiple values -> only prints first value",
			result: QueryResult{
				Name: "my query",
				Result: model.Matrix{
					&model.SampleStream{
						Values: []model.SamplePair{{Value: 42.5}, {Value: 50.0}},
					},
				},
			},
			want: "my query: 42.5",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.result.String())
		})
	}
}

func Test_promtoolJob(t *testing.T) {
	window := ObservabilityWindow{
		Start: time.Unix(1000, 0),
		End:   time.Unix(1900, 0),
	}
	params := QueryParameters{
		Container:  "test-container",
		Controller: "test-controller",
		JobName:    "test-job",
		Window:     window,
	}
	p := Prometheus{Namespace: "test-ns", Image: "prom/prometheus:custom"}

	tests := []struct {
		name          string
		queries       []NamedQuery
		wantNamespace string
		wantImage     string
		wantCommand   []string
	}{
		{
			name: "substitute container placeholder",
			queries: []NamedQuery{
				{Name: "cpu", Query: `rate(cpu{container="$container"}[$duration])`},
			},
			wantNamespace: "test-ns",
			wantImage:     "prom/prometheus:custom",
			wantCommand: []string{
				`printf '%s\n' "cpu"`,
				"promtool query range",
				defaultEndpoint,
				`rate(cpu{container=\"test-container\"}[900s])`,
				"--start 1000",
				"--end 1900",
				"--step 900s",
			},
		},
		{
			name: "substitute controller placeholder",
			queries: []NamedQuery{
				{Name: "reconcile", Query: `rate(reconcile{controller="$controller"}[$duration])`},
			},
			wantNamespace: "test-ns",
			wantImage:     "prom/prometheus:custom",
			wantCommand: []string{
				`printf '%s\n' "reconcile"`,
				"promtool query range",
				defaultEndpoint,
				`rate(reconcile{controller=\"test-controller\"}[900s])`,
				"--start 1000",
				"--end 1900",
				"--step 900s",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := p.promtoolJob(tt.queries, params)
			require.NotNil(t, job)
			assert.Equal(t, tt.wantNamespace, job.Namespace)
			require.Len(t, job.Spec.Template.Spec.Containers, 1)
			container := job.Spec.Template.Spec.Containers[0]
			assert.Equal(t, tt.wantImage, container.Image)
			script := strings.Join(container.Command, " ")
			for _, s := range tt.wantCommand {
				assert.Contains(t, script, s)
			}
		})
	}
}

func Test_marshal(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		promtoolresult string
		want           *model.Matrix
		wantErr        bool
	}{
		{
			name:           "simple metric",
			promtoolresult: `[{"metric":{"interface":"eth0"},"values":[[1791906056.369,"594.555720976818"]]}]`,
			want: &model.Matrix{
				&model.SampleStream{
					Metric: model.Metric{
						"interface": "eth0",
					},
					Values: []model.SamplePair{
						{
							Timestamp: model.Time(1791906056369),
							Value:     model.SampleValue(594.555720976818),
						},
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotErr := parseQueryResult(tt.promtoolresult)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("marshal() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("marshal() succeeded unexpectedly")
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

type namedResult struct {
	name  string
	value float64
}

func Test_parseLog(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		log     string
		want    []namedResult
		wantErr bool
	}{
		{
			name: "test valid query log output",
			log: `
average container cpu usage (cores)
[{"metric":{"beta_kubernetes_io_arch":"arm64","beta_kubernetes_io_os":"linux","container":"external-secrets","instance":"mcp-a2uozj45.ae9d6d51-control-plane","job":"test-controlplane-kubelet-metrics","kubernetes_io_arch":"arm64","kubernetes_io_hostname":"mcp-a2uozj45.ae9d6d51-control-plane","kubernetes_io_os":"linux","namespace":"external-secrets","pod":"external-secrets-external-secrets-6bbbcd9c46-4cwz2"},"values":[[1794836982,"0.00001814146116841061"]]}]
average container memory usage (MiB)
[{"metric":{"beta_kubernetes_io_arch":"arm64","beta_kubernetes_io_os":"linux","container":"external-secrets","instance":"mcp-a2uozj45.ae9d6d51-control-plane","job":"test-controlplane-kubelet-metrics","kubernetes_io_arch":"arm64","kubernetes_io_hostname":"mcp-a2uozj45.ae9d6d51-control-plane","kubernetes_io_os":"linux","namespace":"external-secrets","pod":"external-secrets-external-secrets-6bbbcd9c46-4cwz2"},"values":[[1794836982,"33.721354166666664"]]}]
`,
			want: []namedResult{
				{
					name:  "average container cpu usage (cores)",
					value: 0.00001814146116841061,
				},
				{
					name:  "average container memory usage (MiB)",
					value: 33.721354166666664,
				},
			},
			wantErr: false,
		},
		{
			name:    "invalid log output",
			log:     "unexpected error",
			want:    []namedResult{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotErr := parseLog(tt.log)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("parseLog() failed: %v", gotErr)
				}
				return
			}
			for _, v := range got {
				t.Logf("%s\n", v)
			}
			if tt.wantErr {
				t.Fatal("parseLog() succeeded unexpectedly")
			}
			assert.Len(t, got, len(tt.want))
			for _, v := range tt.want {
				assert.True(t, slices.ContainsFunc(got, func(r QueryResult) bool {
					return v.value == float64(r.Result[0].Values[0].Value) && v.name == r.Name
				}))
			}
		})
	}
}
