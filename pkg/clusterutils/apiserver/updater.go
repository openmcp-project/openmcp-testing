package apiserver

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	kindcluster "sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/yaml"
)

type Updater struct {
	dockerAlias          string
	kindContainer        string
	apiServerPodManifest string
	timeout              time.Duration
}

type Option func(*Updater)

// NewUpdater returns a helper to adjust the static pod manifest of the kube-apiserver in a kind control plane container.
// Targets the onboarding cluster container if no kind target container has been provided and the onboarding default
func NewUpdater(opts ...Option) (*Updater, error) {
	updater := &Updater{
		dockerAlias:          "docker",
		apiServerPodManifest: "/etc/kubernetes/manifests/kube-apiserver.yaml",
		timeout:              time.Minute * 3,
	}
	for _, option := range opts {
		option(updater)
	}
	if updater.kindContainer == "" {
		onboardingClusterContainer, err := onboardingClusterContainer()
		if err != nil {
			return nil, fmt.Errorf("failed to determine onboarding container name: %v", err)
		}
		updater.kindContainer = onboardingClusterContainer
	}
	return updater, nil
}

func WithDockerAlias(alias string) Option {
	return func(c *Updater) {
		c.dockerAlias = alias
	}
}

func WithKindContainer(name string) Option {
	return func(c *Updater) {
		c.kindContainer = name
	}
}

func WithTimeout(timeout time.Duration) Option {
	return func(c *Updater) {
		c.timeout = timeout
	}
}

// addHostToKubeAPIServer adds the given hostname -> ip to the host aliases of the kube-apiserver static pod
// running inside the given kind container, then writes the updated manifest back so kubelet restarts the pod.
func (u *Updater) AddHostAlias(hostname, ip string) error {
	klog.Infof("add host %s with ip %s to /etc/hosts of the (%s) kube-apiserver", hostname, ip, u.kindContainer)
	raw, err := u.getStaticPod()
	if err != nil {
		return err
	}
	tmpFile, err := addHost([]byte(raw), hostname, ip)
	if err != nil {
		return err
	}
	if err := u.copyTmpFileToContainer(tmpFile); err != nil {
		return err
	}
	if err := u.waitForRestart(); err != nil {
		return fmt.Errorf("kube-apiserver didn't restart properly: %v", err)
	}
	return nil
}

// AddHostToKubeAPIServer adds the nameserver ip to dns config of the kube-apiserver static pod
// running inside the given kind container, then writes the updated manifest back so kubelet restarts the pod.
func (u *Updater) AddNameserver(ip string) error {
	klog.Infof("add nameserver with ip %s (coredns) to dns config of (%s) kube-apiserver", ip, u.kindContainer)
	raw, err := u.getStaticPod()
	if err != nil {
		return err
	}
	tmpFile, err := addNameserver([]byte(raw), ip)
	if err != nil {
		return err
	}
	if err := u.copyTmpFileToContainer(tmpFile); err != nil {
		return err
	}
	if err := u.waitForRestart(); err != nil {
		return fmt.Errorf("kube-apiserver didn't restart properly: %v", err)
	}
	return nil
}

func (u *Updater) copyTmpFileToContainer(tmpFile string) error {
	var stderr bytes.Buffer
	cmd := exec.Command(u.dockerAlias, "cp", tmpFile, u.kindContainer+":"+u.apiServerPodManifest)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to copy updated manifest to %s: %w: %s", u.kindContainer, err, stderr.String())
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

// retrieve the kube-apiserver manifest from the kind container filesystem.
func (u *Updater) getStaticPod() (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(u.dockerAlias, "exec", u.kindContainer, "cat", u.apiServerPodManifest)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to read %s from %s: %w: %s", u.apiServerPodManifest, u.kindContainer, err, stderr.String())
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

// waitForRestart polls the kube-apiserver /livez endpoint inside the kind container.
// It first waits for the server to go down, then waits for it to come back healthy.
func (u *Updater) waitForRestart() error {
	timeout := time.Now().Add(u.timeout)
	klog.Infof("wait for (%s) kube-apiserver restart...", u.kindContainer)
	// wait for the server to become unavailable
	for time.Now().Before(timeout) {
		if !u.apiServerAvailable() {
			klog.Infof("(%s) kube-apiserver unavailable", u.kindContainer)
			break
		}
		klog.Infof("wait for (%s) kube-apiserver to become unavailable...", u.kindContainer)
		time.Sleep(2 * time.Second)
	}
	if !time.Now().Before(timeout) {
		return fmt.Errorf("kube-apiserver in %s did not go down within %s", u.kindContainer, u.timeout)
	}
	// wait for the server to become healthy again
	for time.Now().Before(timeout) {
		if u.apiServerAvailable() {
			klog.Infof("(%s) kube-apiserver available", u.kindContainer)
			return nil
		}
		klog.Infof("wait for (%s) kube-apiserver to become available...", u.kindContainer)
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("kube-apiserver in %s did not become healthy within %s", u.kindContainer, u.timeout)
}

func (u *Updater) apiServerAvailable() bool {
	return exec.Command(u.dockerAlias, "exec", u.kindContainer, "curl", "--silent", "--fail", "--insecure", "https://localhost:6443/livez").Run() == nil
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
