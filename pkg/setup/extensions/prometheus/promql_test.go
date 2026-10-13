package prometheus

import (
	"testing"
)

var testout = `
average create time to ready (seconds)
[]
average update time to ready (seconds)
[]
average container cpu usage (cores)
[]
average container memory usage (MiB)
[{"metric":{"beta_kubernetes_io_arch":"arm64","beta_kubernetes_io_os":"linux","container":"sp-externalsecrets","instance":"platform-733ea21-control-plane","job":"platform-kubelet-metrics","kubernetes_io_arch":"arm64","kubernetes_io_hostname":"platform-733ea21-control-plane","kubernetes_io_os":"linux","namespace":"openmcp-system","pod":"sp-externalsecrets-7bbb545c6c-jzjfh"},"values":[[1794419384,"13.53515625"]]}]
average reconcile time (seconds)
[]
max workqueue depth
[]
`

func Test_marshal(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		promtoolresult string
		want           QueryResult
		wantErr        bool
	}{
		{
			name:           "simple metric",
			promtoolresult: `[{"metric":{"interface":"eth0"},"values":[[1791906056.369,"594.555720976818"]]}]`,
			want:           QueryResult{},
			wantErr:        false,
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
			// TODO: update the condition below to compare got with tt.want.
			if true {
				t.Errorf("marshal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_parseLog(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		log     string
		want    []QueryResult
		wantErr bool
	}{
		{
			name:    "test",
			log:     testout,
			want:    []QueryResult{},
			wantErr: false,
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
			// TODO: update the condition below to compare got with tt.want.
			if true {
				t.Errorf("parseLog() = %v, want %v", got, tt.want)
			}
		})
	}
}
