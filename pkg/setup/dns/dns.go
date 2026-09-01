package dns

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	kindcluster "sigs.k8s.io/kind/pkg/cluster"

	"github.com/openmcp-project/openmcp-testing/internal"
	"github.com/openmcp-project/openmcp-testing/pkg/clusterutils"
	"github.com/openmcp-project/openmcp-testing/pkg/conditions"
	"github.com/openmcp-project/openmcp-testing/pkg/platformservices"
	"github.com/openmcp-project/openmcp-testing/pkg/resources"
)

const (
	defaultDNSClusterName    = "dns"
	defaultDNSClusterPurpose = "dns"
	defaultNamespace         = "openmcp-system"
	defaultDNSZone           = "open-control-plane.dev"
)

//go:embed config/*
var configFS embed.FS

type ClusterConfig struct {
	// ClusterName defines the name of the cluster that will be created (default: dns).
	ClusterName string
	// Namespace defines the namespace where the core components like the openmcp-operator is deployed (default: openmcp-system).
	Namespace string
	// ClusterPurpose defines the purpose that will be used to create the cluster for the dedicated dns deployment (default: dns).
	ClusterPurpose string
	// TargetContainer defines which kube-apiserver will be patched as part of the the initial setup (default: onboarding cluster container).
	TargetContainer string
	// ExternalDNSChartVersion defines the chart version of external-dns to use with platform-servce-dns.
	ExternalDNSChartVersion string
	// PlatformServiceDNSVersion defines the version of platform-service-dns to use.
	PlatformServiceDNSVersion string
	// EtcdVersion defines the version of etcd to use with the external-dns coredns provider.
	EtcdVersion string
	// CoreDNSChartVersion defines the the coreDNS Helm chart version to use to deploy the dedicated DNS service.
	CoreDNSChartVersion string
	// DNSZone defines the zone to pass to the coredns etcd plugin (https://coredns.io/plugins/etcd/).
	// In a OpenControlPlane testing context this should typically match the platform service gateway base domain configuration. (default: open-control-plane.dev).
	DNSZone string
}

// CreateExternalService deploys CoreDNS + etcd to a dedicated "dns" cluster together with platform service DNS to resemble a production like external DNS service.
func CreateExternalService(config ClusterConfig) features.Func {
	return func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
		gatewayv1.Install(c.Client().Resources().GetScheme())
		gatewayv1alpha2.Install(c.Client().Resources().GetScheme())
		// apply defaults
		if config.Namespace == "" {
			config.Namespace = defaultNamespace
		}
		if config.ClusterName == "" {
			config.ClusterName = defaultDNSClusterName
		}
		if config.ClusterPurpose == "" {
			config.ClusterPurpose = defaultDNSClusterPurpose
		}
		if config.DNSZone == "" {
			config.DNSZone = defaultDNSZone
		}
		if config.TargetContainer == "" {
			onboardingClusterContainer, err := onboardingClusterContainer()
			if err != nil {
				t.Fatalf("failed to determine onboarding container name: %v", err)
			}
			config.TargetContainer = onboardingClusterContainer
		}
		// create dns cluster
		if err := createCluster(ctx, c, ClusterRequest{
			Name:      config.ClusterName,
			Namespace: config.Namespace,
			Purpose:   config.ClusterPurpose,
		}); err != nil {
			t.Fatalf("failed to create dns cluster: %v", err)
		}
		dnsClusterConfig, err := clusterutils.ConfigByPrefix(config.ClusterName, "default")
		if err != nil {
			t.Fatalf("failed to retrieve dns cluster config: %v", err)
		}
		// deploy etcd and coredns to dns cluster
		etcdTemplate := internal.MustTmpFileFromEmbedFS(configFS, "config/etcd.yaml.tmpl")
		if _, err := resources.CreateObjectsFromTemplateFile(ctx, dnsClusterConfig, etcdTemplate, config); err != nil {
			t.Fatalf("failed to deploy etcd for dns: %v", err)
		}
		klog.Info("successfully deployed etcd to dns cluster")
		coreDNSTemplate := internal.MustTmpFileFromEmbedFS(configFS, "config/coredns.yaml.tmpl")
		if _, err := resources.CreateObjectsFromTemplateFile(ctx, c, coreDNSTemplate, config); err != nil {
			t.Fatalf("failed to deploy coredns: %v", err)
		}
		klog.Info("successfully deployed coredns to dns cluster")
		// install platform-service-dns and configure external-dns to use the dedicated dns cluster etcd
		etcdIP := getLoadBalancerIP(ctx, t, dnsClusterConfig, "etcd-external", "default")
		psDNSConfig := platformServiceDNSConfig{
			Version:                 config.PlatformServiceDNSVersion,
			EtcdIP:                  etcdIP,
			ExternalDNSChartVersion: config.ExternalDNSChartVersion,
			DNSZone:                 config.DNSZone,
		}
		if err := createPlatformServiceDNS(ctx, t, c, psDNSConfig); err != nil {
			t.Fatalf("failed to create platform service dns config: %v", err)
		}
		// inject additional nameserver into kube-apiserver
		nameserverIP := getLoadBalancerIP(ctx, t, dnsClusterConfig, "coredns", "default")
		klog.Infof("add nameserver with ip %s (coredns) to dns config of (%s) kube-apiserver", nameserverIP, config.TargetContainer)
		if err := addNameserverToKubeAPIServer(config.TargetContainer, nameserverIP); err != nil {
			t.Fatalf("failed to add host to kube-apiserver: %v", err)
		}
		if err := waitForKubeAPIServerRestart(config.TargetContainer, 3*time.Minute); err != nil {
			t.Fatalf("kube-apiserver didn't restart properly: %v", err)
		}
		return ctx
	}
}

type HostConfig struct {
	// Namespace defines the namespace where the core components like the openmcp-operator is deployed (default: openmcp-system)
	Namespace string
	// TargetContainer defines which kube-apiserver will be patched as part of the the initial setup (default: onboarding cluster container).
	TargetContainer string
	// GatewayKey defines where to get the ip from
	GatewayKey types.NamespacedName
	// TLSRouteKey defines where to get the hostname from
	TLSRouteKey types.NamespacedName
	// Timeout defines the time to wait for the kube-apiserver to restart (default: 3 minutes)
	Timeout *time.Duration
}

// AddTLSRouteToKubeAPIServer adds the retrieves the hostname of a TLSRoute and IP of a Gatway to Pod.Spec.HostAliases of a kube-apiserver.
// The function waits for the kubelet to restart the kube-apiserver.
func AddTLSRouteToKubeAPIServer(config HostConfig) features.Func {
	return func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
		gatewayv1.Install(c.Client().Resources().GetScheme())
		gatewayv1alpha2.Install(c.Client().Resources().GetScheme())
		openmcpSystemNamespace := config.Namespace
		if openmcpSystemNamespace == "" {
			openmcpSystemNamespace = defaultNamespace
		}
		container := config.TargetContainer
		if container == "" {
			onboardingClusterContainer, err := onboardingClusterContainer()
			if err != nil {
				t.Fatalf("failed to determine onboarding container name: %v", err)
				return ctx
			}
			container = onboardingClusterContainer
		}
		// inject host into kube-apiserver
		gwIP := getGatewayIP(ctx, t, c, "default", openmcpSystemNamespace)
		wbHostname := getHostname(ctx, t, c, config.TLSRouteKey.Name, config.TLSRouteKey.Namespace)
		klog.Infof("add host %s with ip %s to /etc/hosts of the (%s) kube-apiserver", wbHostname, gwIP, container)
		if err := addHostToKubeAPIServer(container, wbHostname, gwIP); err != nil {
			t.Fatalf("failed to add host to kube-apiserver: %v", err)
			return ctx
		}
		timeout := time.Minute * 3
		if config.Timeout != nil {
			timeout = *config.Timeout
		}
		if err := waitForKubeAPIServerRestart(container, timeout); err != nil {
			t.Fatalf("kube-apiserver didn't restart properly: %v", err)
		}
		return ctx
	}
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

type platformServiceDNSConfig struct {
	Version                 string
	EtcdIP                  string
	ExternalDNSChartVersion string
	DNSZone                 string
}

func createPlatformServiceDNS(ctx context.Context, t *testing.T, config *envconf.Config, dnsConfig platformServiceDNSConfig) error {
	t.Helper()
	klog.Info("create platform service dns...")
	err := platformservices.InstallPlatformService(ctx, config, platformservices.PlatformServiceSetup{
		Name:  "dns",
		Image: fmt.Sprintf("ghcr.io/openmcp-project/images/platform-service-dns:%s", dnsConfig.Version),
	})
	if err != nil {
		return err
	}
	klog.Infof("create external-dns config for provider coredns backed by etcd (%s)", dnsConfig.EtcdIP)
	// Import platform service configs with retry logic since discovery api might take some time to pick the new ps-dns config type
	err = wait.For(func(ctx context.Context) (done bool, err error) {
		psDNSConfigTemplate := internal.MustTmpFileFromEmbedFS(configFS, "config/psconfig.yaml.tmpl")
		if _, err = resources.CreateObjectsFromTemplateFile(ctx, config, psDNSConfigTemplate, dnsConfig); err != nil {
			klog.Infof("failed to import platform service dns config, will retry: %v", err)
			// Return false to retry, but don't return error to allow retries
			return false, nil
		}
		klog.Info("successfully imported platform service dns")
		return true, nil
	})
	if err != nil {
		return err
	}
	psDNS := &unstructured.Unstructured{}
	psDNS.SetName("dns")
	psDNS.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "openmcp.cloud",
		Version: "v1alpha1",
		Kind:    "PlatformService",
	})
	return wait.For(conditions.Match(psDNS, config, "Ready", corev1.ConditionTrue))
}
