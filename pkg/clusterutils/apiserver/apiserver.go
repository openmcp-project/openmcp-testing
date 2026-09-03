package apiserver

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	kindcluster "sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/yaml"
)

type DNSUtil struct {
	dockerAlias          string
	kindContainer        string
	apiServerPodManifest string
	timeout              time.Duration
}

type Option func(*DNSUtil)

// NewDNSUtil returns a helper to adjust the static pod manifest of the kube-apiserver in a kind control plane container
func NewDNSUtil(t *testing.T, opts ...Option) *DNSUtil {
	cmd := &DNSUtil{
		dockerAlias:          "docker",
		apiServerPodManifest: "/etc/kubernetes/manifests/kube-apiserver.yaml",
		timeout:              time.Minute * 3,
	}
	for _, option := range opts {
		option(cmd)
	}
	if cmd.kindContainer == "" {
		onboardingClusterContainer, err := onboardingClusterContainer()
		if err != nil {
			t.Fatalf("failed to determine onboarding container name: %v", err)
		}
		cmd.kindContainer = onboardingClusterContainer
	}
	return cmd
}

// WithDockerAlias allows to replace `docker` cli calls with e.g. `podman`
func WithDockerAlias(alias string) Option {
	return func(cu *DNSUtil) {
		cu.dockerAlias = alias
	}
}

// WithTimeout overwrites the default wait timeout
func WithTimeout(timeout time.Duration) Option {
	return func(cu *DNSUtil) {
		cu.timeout = timeout
	}
}

// addHostToKubeAPIServer adds the given hostname -> ip to the host aliases of the kube-apiserver static pod
// running inside the given kind container, then writes the updated manifest back so kubelet restarts the pod.
func (cu *DNSUtil) AddHostEntry(hostname, ip string) error {
	klog.Infof("add host %s with ip %s to /etc/hosts of the (%s) kube-apiserver", hostname, ip, cu.kindContainer)
	raw, err := cu.getStaticPod()
	if err != nil {
		return err
	}
	tmpFile, err := addHost([]byte(raw), hostname, ip)
	if err != nil {
		return err
	}
	if err := cu.copyTmpFileToContainer(tmpFile); err != nil {
		return err
	}
	if err := cu.waitForKubeAPIServerRestart(); err != nil {
		return fmt.Errorf("kube-apiserver didn't restart properly: %v", err)
	}
	return nil
}

// AddHostToKubeAPIServer adds the nameserver ip to dns config of the kube-apiserver static pod
// running inside the given kind container, then writes the updated manifest back so kubelet restarts the pod.
func (cu *DNSUtil) AddNameserver(ip string) error {
	klog.Infof("add nameserver with ip %s (coredns) to dns config of (%s) kube-apiserver", ip, cu.kindContainer)
	raw, err := cu.getStaticPod()
	if err != nil {
		return err
	}
	tmpFile, err := addNameserver([]byte(raw), ip)
	if err != nil {
		return err
	}
	if err := cu.copyTmpFileToContainer(tmpFile); err != nil {
		return err
	}
	if err := cu.waitForKubeAPIServerRestart(); err != nil {
		return fmt.Errorf("kube-apiserver didn't restart properly: %v", err)
	}
	return nil
}

func (cu *DNSUtil) copyTmpFileToContainer(tmpFile string) error {
	var stderr bytes.Buffer
	cmd := exec.Command(cu.dockerAlias, "cp", tmpFile, cu.kindContainer+":"+cu.apiServerPodManifest)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to copy updated manifest to %s: %w: %s", cu.kindContainer, err, stderr.String())
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
func (cu *DNSUtil) getStaticPod() (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(cu.dockerAlias, "exec", cu.kindContainer, "cat", cu.apiServerPodManifest)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to read %s from %s: %w: %s", cu.apiServerPodManifest, cu.kindContainer, err, stderr.String())
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
func (cu *DNSUtil) waitForKubeAPIServerRestart() error {
	deadline := time.Now().Add(cu.timeout)
	klog.Infof("wait for (%s) kube-apiserver restart...", cu.kindContainer)

	// wait for the server to become unavailable
	for time.Now().Before(deadline) {
		if err := cu.curlAPIServer().Run(); err != nil {
			klog.Infof("(%s) kube-apiserver unavailable", cu.kindContainer)
			break
		}
		klog.Infof("wait for (%s) kube-apiserver to become unavailable...", cu.kindContainer)
		time.Sleep(2 * time.Second)
	}
	if !time.Now().Before(deadline) {
		return fmt.Errorf("kube-apiserver in %s did not go down within %s", cu.kindContainer, cu.timeout)
	}

	// wait for the server to become healthy again
	for time.Now().Before(deadline) {
		if err := cu.curlAPIServer().Run(); err == nil {
			klog.Infof("(%s) kube-apiserver available", cu.kindContainer)
			return nil
		}
		klog.Infof("wait for (%s) kube-apiserver to become available...", cu.kindContainer)
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("kube-apiserver in %s did not become healthy within %s", cu.kindContainer, cu.timeout)
}

func (cu *DNSUtil) curlAPIServer() *exec.Cmd {
	return exec.Command(cu.dockerAlias, "exec", cu.kindContainer, "curl", "--silent", "--fail", "--insecure", "https://localhost:6443/livez")
}

func onboardingClusterContainer() (string, error) {
	kind := kindcluster.NewProvider()
	clusters, err := kind.List()
	if err != nil {
		return "", err
	}
	for _, clusterName := range clusters {
		if strings.HasPrefix(clusterName, "onboarding") {
			nodes, err := kind.ListNodes(clusterName)
			if err != nil {
				return "", fmt.Errorf("failed to retrieve onboarding cluster nodes: %w", err)
			}
			return nodes[0].String(), nil
		}
	}
	return "", errors.New("onboarding cluster not found")
}
