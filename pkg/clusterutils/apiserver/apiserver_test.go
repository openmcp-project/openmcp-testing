package apiserver

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"
)

func Test_addHostAlias(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		pod      *v1.Pod
		hostName string
		ip       string
	}{
		{
			name:     "add host alias to empty pod spec",
			pod:      &v1.Pod{},
			hostName: "test.example.com",
			ip:       "192.168.0.1",
		},
		{
			name: "add host alias to existing host alias spec",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					HostAliases: []v1.HostAlias{
						{
							IP:        "127.0.0.1",
							Hostnames: []string{"localhost"},
						},
					},
				},
			},
			hostName: "test.example.com",
			ip:       "192.168.0.1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalLength := len(tt.pod.Spec.HostAliases)
			addHostAlias(tt.pod, tt.hostName, tt.ip)
			assert.NotNil(t, tt.pod.Spec.HostAliases)
			assert.Len(t, tt.pod.Spec.HostAliases, originalLength+1)
			assert.Contains(t, tt.pod.Spec.HostAliases, v1.HostAlias{
				IP:        tt.ip,
				Hostnames: []string{tt.hostName},
			})
		})
	}
}

func Test_addHost(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		podManifest []byte
		hostname    string
		ip          string
		want        string
		wantErr     bool
	}{
		{
			name:        "add host alias to static kube-apiserver pod manifest of kind node",
			podManifest: []byte(staticKubeAPIServerPodManifest),
			hostname:    "test.example.com",
			ip:          "192.168.0.1",
			wantErr:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := bytes.Split(tt.podManifest, []byte("\n"))
			for i, line := range lines {
				fmt.Printf("%4d: %s\n", i+1, line)
			}
			got, gotErr := addHost(tt.podManifest, tt.hostname, tt.ip)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("addHost() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("addHost() succeeded unexpectedly")
			}
			filename := filepath.Base(got)
			assert.Equal(t, "kube-apiserver.yaml", filename)
			pod := &v1.Pod{}
			data, err := os.ReadFile(got)
			require.NoError(t, err)
			require.NoError(t, yaml.Unmarshal(data, pod))
			hostAliases := pod.Spec.HostAliases
			assert.Len(t, hostAliases, 1)
			assert.Equal(t, hostAliases[0].IP, tt.ip)
			assert.Equal(t, hostAliases[0].Hostnames[0], tt.hostname)
		})
	}
}

func Test_addNameserver(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		podManifest []byte
		ip          string
		want        string
		wantErr     bool
	}{
		{
			name:        "the positive test",
			podManifest: []byte(staticKubeAPIServerPodManifest),
			ip:          "192.168.0.1",
			wantErr:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := bytes.Split(tt.podManifest, []byte("\n"))
			for i, line := range lines {
				fmt.Printf("%4d: %s\n", i+1, line)
			}
			got, gotErr := addNameserver(tt.podManifest, tt.ip)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("addHost() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("addHost() succeeded unexpectedly")
			}
			filename := filepath.Base(got)
			assert.Equal(t, "kube-apiserver.yaml", filename)
			pod := &v1.Pod{}
			data, err := os.ReadFile(got)
			require.NoError(t, err)
			require.NoError(t, yaml.Unmarshal(data, pod))
			assert.Equal(t, pod.Spec.DNSPolicy, v1.DNSNone)
			assert.Len(t, pod.Spec.DNSConfig.Nameservers, 1)
			assert.Equal(t, pod.Spec.DNSConfig.Nameservers[0], tt.ip)
		})
	}
}

const staticKubeAPIServerPodManifest = `
apiVersion: v1
kind: Pod
metadata:
  annotations:
    kubeadm.kubernetes.io/kube-apiserver.advertise-address.endpoint: 10.89.0.2:6443
  labels:
    component: kube-apiserver
    tier: control-plane
  name: kube-apiserver
  namespace: kube-system
spec:
  containers:
  - command:
    - kube-apiserver
    - --advertise-address=10.89.0.2
    - --allow-privileged=true
    - --authorization-mode=Node,RBAC
    - --client-ca-file=/etc/kubernetes/pki/ca.crt
    - --enable-admission-plugins=NodeRestriction
    - --enable-bootstrap-token-auth=true
    - --etcd-cafile=/etc/kubernetes/pki/etcd/ca.crt
    - --etcd-certfile=/etc/kubernetes/pki/apiserver-etcd-client.crt
    - --etcd-keyfile=/etc/kubernetes/pki/apiserver-etcd-client.key
    - --etcd-servers=https://127.0.0.1:2379
    - --kubelet-client-certificate=/etc/kubernetes/pki/apiserver-kubelet-client.crt
    - --kubelet-client-key=/etc/kubernetes/pki/apiserver-kubelet-client.key
    - --kubelet-preferred-address-types=InternalIP,ExternalIP,Hostname
    - --proxy-client-cert-file=/etc/kubernetes/pki/front-proxy-client.crt
    - --proxy-client-key-file=/etc/kubernetes/pki/front-proxy-client.key
    - --requestheader-allowed-names=front-proxy-client
    - --requestheader-client-ca-file=/etc/kubernetes/pki/front-proxy-ca.crt
    - --requestheader-extra-headers-prefix=X-Remote-Extra-
    - --requestheader-group-headers=X-Remote-Group
    - --requestheader-username-headers=X-Remote-User
    - --secure-port=6443
    - --service-account-issuer=https://kubernetes.default.svc.cluster.local
    - --service-account-key-file=/etc/kubernetes/pki/sa.pub
    - --service-account-signing-key-file=/etc/kubernetes/pki/sa.key
    - --service-cluster-ip-range=10.96.0.0/16
    - --tls-cert-file=/etc/kubernetes/pki/apiserver.crt
    - --tls-private-key-file=/etc/kubernetes/pki/apiserver.key
    - --runtime-config=
    image: registry.k8s.io/kube-apiserver:v1.36.1
    imagePullPolicy: IfNotPresent
    livenessProbe:
      failureThreshold: 8
      httpGet:
        host: 10.89.0.2
        path: /livez
        port: probe-port
        scheme: HTTPS
      initialDelaySeconds: 10
      periodSeconds: 10
      timeoutSeconds: 15
    name: kube-apiserver
    ports:
    - containerPort: 6443
      name: probe-port
      protocol: TCP
    readinessProbe:
      failureThreshold: 3
      httpGet:
        host: 10.89.0.2
        path: /readyz
        port: probe-port
        scheme: HTTPS
      periodSeconds: 1
      timeoutSeconds: 15
    resources:
      requests:
        cpu: 250m
    startupProbe:
      failureThreshold: 24
      httpGet:
        host: 10.89.0.2
        path: /livez
        port: probe-port
        scheme: HTTPS
      initialDelaySeconds: 10
      periodSeconds: 10
      timeoutSeconds: 15
    volumeMounts:
    - mountPath: /etc/ssl/certs
      name: ca-certs
      readOnly: true
    - mountPath: /etc/ca-certificates
      name: etc-ca-certificates
      readOnly: true
    - mountPath: /etc/kubernetes/pki
      name: k8s-certs
      readOnly: true
    - mountPath: /usr/local/share/ca-certificates
      name: usr-local-share-ca-certificates
      readOnly: true
    - mountPath: /usr/share/ca-certificates
      name: usr-share-ca-certificates
      readOnly: true
  hostNetwork: true
  priority: 2000001000
  priorityClassName: system-node-critical
  securityContext:
    seccompProfile:
      type: RuntimeDefault
  volumes:
  - hostPath:
      path: /etc/ssl/certs
      type: DirectoryOrCreate
    name: ca-certs
  - hostPath:
      path: /etc/ca-certificates
      type: DirectoryOrCreate
    name: etc-ca-certificates
  - hostPath:
      path: /etc/kubernetes/pki
      type: DirectoryOrCreate
    name: k8s-certs
  - hostPath:
      path: /usr/local/share/ca-certificates
      type: DirectoryOrCreate
    name: usr-local-share-ca-certificates
  - hostPath:
      path: /usr/share/ca-certificates
      type: DirectoryOrCreate
    name: usr-share-ca-certificates
status: {}
`
