package apiserver

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	kindcluster "sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/yaml"
)

// Updater is a helper to adjust the static pod manifest of the kube-apiserver in a kind control plane container.
type Updater struct {
	// The CLI to interact with the kind container. Defaults to docker, can be replaced with docker compatible CLIs like podman.
	dockerCLI string
	// The kind control plane to update. Defaults to the onboarcing cluster container.
	kindContainer string
	// The path to the api-server manifest in the kind container.
	apiServerManifestPath string
	// The timeout for the API server restart.
	timeout time.Duration
}

type Option func(*Updater)

// NewUpdater returns a new Updater. The onboarding cluster container is the default cluster target.
func NewUpdater(opts ...Option) (*Updater, error) {
	updater := &Updater{
		dockerCLI:             "docker",
		apiServerManifestPath: "/etc/kubernetes/manifests/kube-apiserver.yaml",
		timeout:               time.Minute * 3,
	}
	for _, o := range opts {
		o(updater)
	}
	if updater.kindContainer == "" {
		onboardingClusterContainer, err := onboardingClusterContainer()
		if err != nil {
			return nil, err
		}
		updater.kindContainer = onboardingClusterContainer
	}
	return updater, nil
}

func WithAPIServerManifestPath(path string) Option {
	return func(c *Updater) {
		c.apiServerManifestPath = path
	}
}

func WithDockerCLI(cli string) Option {
	return func(c *Updater) {
		c.dockerCLI = cli
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

// AddHostAlias adds the given hostname -> ip mapping as host alias to the kube-apiserver (static pod) manifest
// inside a kind container and waits for the kubelet to restart the API server.
func (u *Updater) AddHostAlias(hostname, ip string) error {
	klog.Infof("add host %s with ip %s to /etc/hosts of the (%s) kube-apiserver", hostname, ip, u.kindContainer)
	pod, err := u.getStaticPod()
	if err != nil {
		return err
	}
	pod.Spec.HostAliases = append(pod.Spec.HostAliases, corev1.HostAlias{
		IP: ip,
		Hostnames: []string{
			hostname,
		},
	})
	if err := u.writeToContainerFS(pod); err != nil {
		return err
	}
	if err := u.waitForRestart(); err != nil {
		return err
	}
	return nil
}

// AddNameserver adds the nameserver ip to the DNS config of the kube-apiserver (static pod) manifest
// inside a kind container and waits for the kubelet to restart the API server.
func (u *Updater) AddNameserver(ip string) error {
	klog.Infof("add nameserver with ip %s (coredns) to dns config of (%s) kube-apiserver", ip, u.kindContainer)
	pod, err := u.getStaticPod()
	if err != nil {
		return err
	}
	pod.Spec.DNSPolicy = corev1.DNSNone
	pod.Spec.DNSConfig = &corev1.PodDNSConfig{
		Nameservers: []string{
			ip,
		},
	}
	if err := u.writeToContainerFS(pod); err != nil {
		return err
	}
	if err := u.waitForRestart(); err != nil {
		return err
	}
	return nil
}

func (u *Updater) writeToContainerFS(pod *corev1.Pod) error {
	tmpFile, err := os.CreateTemp("", "kube-apiserver.yaml")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
	}()
	data, err := yaml.Marshal(pod)
	if err != nil {
		return fmt.Errorf("failed to marshal pod to yaml: %w", err)
	}
	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	var stderr bytes.Buffer
	cmd := exec.Command(u.dockerCLI, "cp", tmpFile.Name(), u.kindContainer+":"+u.apiServerManifestPath)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to copy updated manifest to %s: %w: %s", u.kindContainer, err, stderr.String())
	}
	return nil
}

// retrieve the kube-apiserver manifest from the kind container filesystem.
func (u *Updater) getStaticPod() (*corev1.Pod, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(u.dockerCLI, "exec", u.kindContainer, "cat", u.apiServerManifestPath)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to read %s from %s: %w: %s", u.apiServerManifestPath, u.kindContainer, err, stderr.String())
	}
	podManifest := stdout.String()
	pod := &corev1.Pod{}
	if err := yaml.Unmarshal([]byte(podManifest), pod); err != nil {
		return nil, fmt.Errorf("failed to unmarshal pod manifest: %w", err)
	}
	return pod, nil
}

// waitForRestart polls the kube-apiserver /livez endpoint inside the kind container.
// It waits for the API server to go down and come back healthy.
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
	return exec.Command(u.dockerCLI, "exec", u.kindContainer, "curl", "--silent", "--fail", "--insecure", "https://localhost:6443/livez").Run() == nil
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
