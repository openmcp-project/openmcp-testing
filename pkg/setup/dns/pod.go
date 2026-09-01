package dns

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/yaml"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

const (
	defaultStaticPodFile = "/etc/kubernetes/manifests/kube-apiserver.yaml"
)

// addHostToKubeAPIServer adds the given hostname -> ip to the host aliases of the kube-apiserver static pod
// running inside the given kind container, then writes the updated manifest back so kubelet restarts the pod.
func addHostToKubeAPIServer(kindContainer, hostname, ip string) error {
	raw, err := getStaticPod(kindContainer, "")
	if err != nil {
		return err
	}
	tmpFile, err := addHost([]byte(raw), hostname, ip)
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd := exec.Command("docker", "cp", tmpFile, kindContainer+":"+defaultStaticPodFile)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to copy updated manifest to %s: %w: %s", kindContainer, err, stderr.String())
	}
	return nil
}

// AddHostToKubeAPIServer adds the nameserver ip to dns config of the kube-apiserver static pod
// running inside the given kind container, then writes the updated manifest back so kubelet restarts the pod.
func addNameserverToKubeAPIServer(kindContainer, ip string) error {
	raw, err := getStaticPod(kindContainer, "")
	if err != nil {
		return err
	}
	tmpFile, err := addNameserver([]byte(raw), ip)
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd := exec.Command("docker", "cp", tmpFile, kindContainer+":"+defaultStaticPodFile)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to copy updated manifest to %s: %w: %s", kindContainer, err, stderr.String())
	}
	return nil
}

// addNameserver adds the given IP to the list of nameserver of the pod dns config and writes the result to a temporary file
func addNameserver(podManifest []byte, ip string) (string, error) {
	pod := &corev1.Pod{}
	if err := yaml.Unmarshal(podManifest, pod); err != nil {
		return "", fmt.Errorf("failed to unmarshal pod manifest: %w", err)
	}
	pod.Spec.DNSPolicy = corev1.DNSNone
	pod.Spec.DNSConfig = &corev1.PodDNSConfig{
		Nameservers: []string{
			ip,
		},
	}
	data, err := yaml.Marshal(pod)
	if err != nil {
		return "", fmt.Errorf("failed to marshal pod to yaml: %w", err)
	}
	tmpFile := filepath.Join(os.TempDir(), "kube-apiserver.yaml")
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}
	return tmpFile, nil
}

// retrieve kube-apiserver.yaml from kind node fs
// file defaults to /etc/kubernetes/manifests/kube-apiserver.yaml
// returns the cat file output to pass to addHost
func getStaticPod(containerName, file string) (string, error) {
	if file == "" {
		file = defaultStaticPodFile
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("docker", "exec", containerName, "cat", file)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to read %s from %s: %w: %s", file, containerName, err, stderr.String())
	}
	return stdout.String(), nil
}

// addHost adds the given hostName and IP to the list of host aliases and writes the result to a temporary file
func addHost(podManifest []byte, hostname, ip string) (string, error) {
	pod := &corev1.Pod{}
	if err := yaml.Unmarshal(podManifest, pod); err != nil {
		return "", fmt.Errorf("failed to unmarshal pod manifest: %w", err)
	}
	addHostAlias(pod, hostname, ip)
	data, err := yaml.Marshal(pod)
	if err != nil {
		return "", fmt.Errorf("failed to marshal pod to yaml: %w", err)
	}
	tmpFile := filepath.Join(os.TempDir(), "kube-apiserver.yaml")
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}
	return tmpFile, nil
}

func addHostAlias(pod *corev1.Pod, hostName, ip string) {
	pod.Spec.HostAliases = append(pod.Spec.HostAliases, corev1.HostAlias{
		IP: ip,
		Hostnames: []string{
			hostName,
		},
	})
}

// waitForKubeAPIServerRestart polls the kube-apiserver /livez endpoint inside the kind
// container via docker exec. It first waits for the server to go down (confirming kubelet
// has torn down the old pod), then waits for it to come back healthy.
func waitForKubeAPIServerRestart(kindContainer string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	klog.Infof("wait for (%s) kube-apiserver restart...", kindContainer)

	// wait for the server to become unavailable
	for time.Now().Before(deadline) {
		if err := curlAPIServer(kindContainer).Run(); err != nil {
			klog.Infof("(%s) kube-apiserver unavailable", kindContainer)
			break
		}
		klog.Infof("wait for (%s) kube-apiserver to become unavailable...", kindContainer)
		time.Sleep(2 * time.Second)
	}
	if !time.Now().Before(deadline) {
		return fmt.Errorf("kube-apiserver in %s did not go down within %s", kindContainer, timeout)
	}

	// wait for the server to become healthy again
	for time.Now().Before(deadline) {
		if err := curlAPIServer(kindContainer).Run(); err == nil {
			klog.Infof("(%s) kube-apiserver available", kindContainer)
			return nil
		}
		klog.Infof("wait for (%s) kube-apiserver to become available...", kindContainer)
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("kube-apiserver in %s did not become healthy within %s", kindContainer, timeout)
}

func curlAPIServer(kindContainer string) *exec.Cmd {
	return exec.Command("docker", "exec", kindContainer, "curl", "--silent", "--fail", "--insecure", "https://localhost:6443/livez")
}

// getHostname retrieves the first hostname defined in the TLSRoute with the given name and namespace.
func getHostname(ctx context.Context, t *testing.T, config *envconf.Config, name, namespace string) string {
	t.Helper()
	tlsRoute := &gatewayv1alpha2.TLSRoute{}
	tlsRoute.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "gateway.networking.k8s.io",
		Version: "v1alpha2",
		Kind:    "TLSRoute",
	})
	tlsRoute.SetName(name)
	tlsRoute.SetNamespace(namespace)
	if err := config.Client().Resources().Get(ctx, name, namespace, tlsRoute); err != nil {
		t.Fatalf("failed to get TLSRoute '%s/%s': %v", namespace, name, err)
	}
	if len(tlsRoute.Spec.Hostnames) == 0 {
		t.Fatalf("TLSRoute '%s/%s' does not have any hostnames defined", namespace, name)
	}
	return string(tlsRoute.Spec.Hostnames[0])
}

// getGatewayIP retrieves the first IP address of the Gateway with the given name and namespace.
func getGatewayIP(ctx context.Context, t *testing.T, config *envconf.Config, name, namespace string) string {
	t.Helper()

	gateway := &gatewayv1.Gateway{}
	gateway.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "gateway.networking.k8s.io",
		Version: "v1",
		Kind:    "Gateway",
	})
	gateway.SetName(name)
	gateway.SetNamespace(namespace)
	if err := config.Client().Resources().Get(ctx, name, namespace, gateway); err != nil {
		t.Fatalf("failed to get Gateway '%s/%s': %v", namespace, name, err)
	}
	for _, addr := range gateway.Status.Addresses {
		if addr.Type != nil && *addr.Type == gatewayv1.IPAddressType {
			return addr.Value
		}
	}
	t.Fatalf("Gateway '%s/%s' does not have any IP addresses exposed", namespace, name)
	return ""
}

// getLoadBalancerIP retrieves the first IP address of the service with key name/namespace
func getLoadBalancerIP(ctx context.Context, t *testing.T, config *envconf.Config, name, namespace string) string {
	t.Helper()
	service := &corev1.Service{}
	service.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Service",
	})
	service.SetName(name)
	service.SetNamespace(namespace)
	if err := config.Client().Resources().Get(ctx, name, namespace, service); err != nil {
		t.Fatalf("failed to get Service '%s/%s': %v", namespace, name, err)
	}
	for _, ingress := range service.Status.LoadBalancer.Ingress {
		if ingress.IP != "" {
			return ingress.IP
		}
	}
	t.Fatalf("Service '%s/%s' does not have any IP addresses exposed", namespace, name)
	return ""
}
