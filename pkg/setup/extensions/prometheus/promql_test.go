package prometheus

import (
	"testing"
)

var testout = `
average create time to ready
[{"metric":{"instance":"10.244.0.19:8080","job":"controller-runtime","namespace":"openmcp-system","pod":"sp-externalsecretsoperator-5bc9d9d579-2zb4m"},"value":[1791906056.048,"126.317578994"]}]
average update time to ready
[{"metric":{"instance":"10.244.0.19:8080","job":"controller-runtime","namespace":"openmcp-system","pod":"sp-externalsecretsoperator-5bc9d9d579-2zb4m"},"value":[1791906056.068,"NaN"]}]
average pod cpu usage (cores)
[{"metric":{"beta_kubernetes_io_arch":"arm64","beta_kubernetes_io_os":"linux","instance":"platform-6cafea6-control-plane","job":"kubelet-resource","kubernetes_io_arch":"arm64","kubernetes_io_hostname":"platform-6cafea6-control-plane","kubernetes_io_os":"linux","namespace":"openmcp-system","pod":"sp-externalsecretsoperator-5bc9d9d579-2zb4m"},"value":[1791906056.086,"0.0007415611469388148"]}]
average pod memory usage (MiB)
[{"metric":{"beta_kubernetes_io_arch":"arm64","beta_kubernetes_io_os":"linux","instance":"platform-6cafea6-control-plane","job":"kubelet-resource","kubernetes_io_arch":"arm64","kubernetes_io_hostname":"platform-6cafea6-control-plane","kubernetes_io_os":"linux","namespace":"openmcp-system","pod":"sp-externalsecretsoperator-5bc9d9d579-2zb4m"},"value":[1791906056.104,"26.95160590277778"]}]
average reconcile time (seconds)
[{"metric":{"controller":"externalsecretsoperator","instance":"10.244.0.19:8080","job":"controller-runtime","namespace":"openmcp-system","pod":"sp-externalsecretsoperator-5bc9d9d579-2zb4m"},"value":[1791906056.123,"0.02316181569230772"]}]
total reconcile errors
[{"metric":{},"value":[1791906056.14,"0"]}]
reconcile rate (ops/sec)
[{"metric":{"controller":"externalsecretsoperator"},"value":[1791906056.158,"0.024074074074074074"]}]
reconcile error rate (%)
[{"metric":{"controller":"externalsecretsoperator"},"value":[1791906056.177,"0"]}]
reconcile time p99 (seconds)
[{"metric":{"controller":"externalsecretsoperator"},"value":[1791906056.195,"0.04891666666666667"]}]
workqueue depth
[{"metric":{"controller":"externalsecretsoperator","instance":"10.244.0.19:8080","job":"controller-runtime","name":"externalsecretsoperator","namespace":"openmcp-system","pod":"sp-externalsecretsoperator-5bc9d9d579-2zb4m","priority":"0"},"value":[1791906056.214,"0"]}]
workqueue queue latency p99 (seconds)
[{"metric":{"name":"externalsecretsoperator"},"value":[1791906056.231,"0.00009414999999999998"]}]
workqueue process latency p99 (seconds)
[{"metric":{"name":"externalsecretsoperator"},"value":[1791906056.248,"0.09910000000000002"]}]
total workqueue retries
[{"metric":{"name":"externalsecretsoperator"},"value":[1791906056.266,"13.04814814814815"]}]
worker utilization (%)
[{"metric":{"controller":"externalsecretsoperator","instance":"10.244.0.19:8080","job":"controller-runtime","namespace":"openmcp-system","pod":"sp-externalsecretsoperator-5bc9d9d579-2zb4m"},"value":[1791906056.284,"0"]}]
api server request rate (req/sec)
[{"metric":{"code":"200","method":"GET"},"value":[1791906056.302,"0.3648148148148148"]},{"metric":{"code":"201","method":"POST"},"value":[1791906056.302,"0"]},{"metric":{"code":"404","method":"GET"},"value":[1791906056.302,"0"]},{"metric":{"code":"200","method":"PATCH"},"value":[1791906056.302,"0.001851851851851852"]},{"metric":{"code":"200","method":"PUT"},"value":[1791906056.302,"0.024074074074074074"]}]
cpu throttle rate (%)
[]
memory limit utilization (%)
[{"metric":{"beta_kubernetes_io_arch":"arm64","beta_kubernetes_io_os":"linux","container":"sp-externalsecretsoperator","id":"/kubelet.slice/kubelet-kubepods.slice/kubelet-kubepods-besteffort.slice/kubelet-kubepods-besteffort-poddf24b703_5458_40ce_9c78_e2e26ef1ff6a.slice/cri-containerd-b0b39019b4e8131e6ec908d3ce084e09f5750cd2cf3674952669e96d3b4d2c48.scope","image":"ghcr.io/openmcp-project/images/service-provider-external-secrets:v1.2.3-dev-60e3b50dd5d380d28f75caedcb900bf602eadec3","instance":"platform-6cafea6-control-plane","job":"kubelet-cadvisor","kubernetes_io_arch":"arm64","kubernetes_io_hostname":"platform-6cafea6-control-plane","kubernetes_io_os":"linux","name":"b0b39019b4e8131e6ec908d3ce084e09f5750cd2cf3674952669e96d3b4d2c48","namespace":"openmcp-system","pod":"sp-externalsecretsoperator-5bc9d9d579-2zb4m"},"value":[1791906056.337,"+Inf"]}]
network receive rate (bytes/sec)
[{"metric":{"interface":"eth0"},"value":[1791906056.354,"1724.1795633784498"]}]
network transmit rate (bytes/sec)
[{"metric":{"interface":"eth0"},"value":[1791906056.369,"594.555720976818"]}]
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
			promtoolresult: `[{"metric":{"interface":"eth0"},"value":[1791906056.369,"594.555720976818"]}]`,
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
