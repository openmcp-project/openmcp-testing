package dns

import (
	"context"
	"embed"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/openmcp-project/openmcp-testing/internal"
	"github.com/openmcp-project/openmcp-testing/pkg/clusterutils"
	"github.com/openmcp-project/openmcp-testing/pkg/clusterutils/apiserver"
	"github.com/openmcp-project/openmcp-testing/pkg/conditions"
	"github.com/openmcp-project/openmcp-testing/pkg/resources"
)

const (
	defaultCoreDNSChartVersion       = "1.47.0"
	defaultDNSClusterName            = "dns"
	defaultDNSClusterPurpose         = "dns"
	defaultDNSZone                   = "open-control-plane.dev"
	defaultEtcdVersion               = "v3.5.15"
	defaultExternalDNSChartVersion   = "v0.21.0"
	defaultNamespace                 = "openmcp-system"
	defaultPlatformServiceDNSVersion = "v0.1.0"
)

//go:embed config/*
var configFS embed.FS

type Deployer struct {
	// clusterName defines the name of the cluster that will be created (default: dns).
	clusterName string
	// namespace defines the namespace where the core components like the openmcp-operator is deployed (default: openmcp-system).
	namespace string
	// clusterPurpose defines the purpose that will be used to create the cluster for the dedicated dns deployment (default: dns).
	clusterPurpose string
	// externalDNSChartVersion defines the chart version of external-dns to use with platform-servce-dns.
	externalDNSChartVersion string
	// platformServiceDNSVersion defines the version of platform-service-dns to use.
	platformServiceDNSVersion string
	// etcdVersion defines the version of etcd to use with the external-dns coredns provider.
	etcdVersion string
	// coreDNSChartVersion defines the the coreDNS Helm chart version to use to deploy the dedicated DNS service.
	coreDNSChartVersion string
	// dnsZone defines the zone to pass to the coredns etcd plugin (https://coredns.io/plugins/etcd/).
	// In a OpenControlPlane testing context this should typically match the platform service gateway base domain configuration. (default: open-control-plane.dev).
	dnsZone string
	// apiServerUpdater is used to adjust the kind api server dns settings
	apiServerUpdater *apiserver.Updater
}

type Option func(*Deployer)

func WithClusterName(name string) Option {
	return func(s *Deployer) {
		s.clusterName = name
	}
}

func WithNamespace(namespace string) Option {
	return func(s *Deployer) {
		s.namespace = namespace
	}
}

func WithClusterPurpose(purpose string) Option {
	return func(s *Deployer) {
		s.clusterPurpose = purpose
	}
}

func WithExternalDNSChartVersion(version string) Option {
	return func(s *Deployer) {
		s.externalDNSChartVersion = version
	}
}

func WithEtcdVersion(version string) Option {
	return func(s *Deployer) {
		s.etcdVersion = version
	}
}

func WithCoreDNSChartVersion(version string) Option {
	return func(s *Deployer) {
		s.coreDNSChartVersion = version
	}
}

func WithPlatformServiceDNSVersion(version string) Option {
	return func(s *Deployer) {
		s.platformServiceDNSVersion = version
	}
}

func WithAPIServerUpdater(c *apiserver.Updater) Option {
	return func(s *Deployer) {
		s.apiServerUpdater = c
	}
}

func WithDNSZone(zone string) Option {
	return func(s *Deployer) {
		s.dnsZone = zone
	}
}

func NewDeployer(opts ...Option) (*Deployer, error) {
	setup := &Deployer{
		clusterName:               defaultDNSClusterName,
		namespace:                 defaultNamespace,
		clusterPurpose:            defaultDNSClusterPurpose,
		externalDNSChartVersion:   defaultExternalDNSChartVersion,
		platformServiceDNSVersion: defaultPlatformServiceDNSVersion,
		etcdVersion:               defaultEtcdVersion,
		coreDNSChartVersion:       defaultCoreDNSChartVersion,
		dnsZone:                   defaultDNSZone,
	}
	for _, o := range opts {
		o(setup)
	}
	if setup.apiServerUpdater == nil {
		var err error
		setup.apiServerUpdater, err = apiserver.NewUpdater()
		if err != nil {
			return nil, fmt.Errorf("failed to init api-server updater: %v", err)
		}
	}
	return setup, nil
}

// CreateExternalService creates a dedicated DNS cluster and sets up platform-cluster-dns to use it as external-dns target.
// If the deployment fails, the calling test is stopped and marked as failed.
func CreateExternalService(opts ...Option) features.Func {
	return func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
		dnsDeployer, err := NewDeployer(opts...)
		if err != nil {
			t.Fatalf("failed to create DNS deployer: %v", err)
			return ctx
		}
		if err := dnsDeployer.Deploy(ctx, c); err != nil {
			t.Fatalf("failed to deploy external DNS service: %v", err)
		}
		return ctx
	}
}

// Deploy deploys CoreDNS + etcd to a dedicated "dns" cluster together with platform service DNS to resemble a production like external DNS service.
func (d *Deployer) Deploy(ctx context.Context, c *envconf.Config) error {
	runtime.Must(gatewayv1.Install(c.Client().Resources().GetScheme()))
	runtime.Must(gatewayv1alpha2.Install(c.Client().Resources().GetScheme()))
	// create dns cluster
	if err := createCluster(ctx, c, ClusterRequest{
		Name:      d.clusterName,
		Namespace: d.namespace,
		Purpose:   d.clusterPurpose,
	}); err != nil {
		return fmt.Errorf("failed to create dns cluster: %v", err)
	}
	dnsClusterConfig, err := clusterutils.ConfigByPrefix(d.clusterName, "default")
	if err != nil {
		return fmt.Errorf("failed to retrieve dns cluster config: %v", err)
	}
	// deploy etcd and coredns to dns cluster
	etcdTemplate := internal.MustTmpFileFromEmbedFS(configFS, "config/etcd.yaml.tmpl")
	etcdData := struct {
		EtcdVersion string
	}{
		EtcdVersion: d.etcdVersion,
	}
	if _, err := resources.CreateObjectsFromTemplateFile(ctx, dnsClusterConfig, etcdTemplate, etcdData); err != nil {
		return fmt.Errorf("failed to deploy etcd for dns: %v", err)
	}
	klog.Info("successfully deployed etcd to dns cluster")
	coreDNSTemplate := internal.MustTmpFileFromEmbedFS(configFS, "config/coredns.yaml.tmpl")
	coreDNSData := struct {
		Namespace           string
		ClusterName         string
		CoreDNSChartVersion string
		DNSZone             string
	}{
		Namespace:           d.namespace,
		ClusterName:         d.clusterName,
		CoreDNSChartVersion: d.coreDNSChartVersion,
		DNSZone:             d.dnsZone,
	}
	if _, err := resources.CreateObjectsFromTemplateFile(ctx, c, coreDNSTemplate, coreDNSData); err != nil {
		return fmt.Errorf("failed to deploy coredns: %v", err)
	}
	klog.Info("successfully deployed coredns to dns cluster")
	// install platform-service-dns and configure external-dns to use the dedicated dns cluster etcd
	etcdIP, err := getLoadBalancerIP(ctx, dnsClusterConfig, "etcd-external", "default")
	if err != nil {
		return fmt.Errorf("failed to retrieve etcd IP: %v", err)
	}

	if err := d.createPlatformServiceDNS(ctx, c, etcdIP); err != nil {
		return fmt.Errorf("failed to create platform service dns config: %v", err)
	}
	// inject additional nameserver into kube-apiserver
	nameserverIP, err := getLoadBalancerIP(ctx, dnsClusterConfig, "coredns", "default")
	if err != nil {
		return fmt.Errorf("failed to retrieve core dns IP: %v", err)
	}
	if err := d.apiServerUpdater.AddNameserver(nameserverIP); err != nil {
		return fmt.Errorf("failed to add host to kube-apiserver: %v", err)
	}
	return nil
}

func (d *Deployer) createPlatformServiceDNS(ctx context.Context, config *envconf.Config, etcdIP string) error {
	klog.Info("create platform service dns...")
	psDNSTemplate := internal.MustTmpFileFromEmbedFS(configFS, "config/ps.yaml.tmpl")
	psDNSData := struct {
		Version string
	}{
		Version: d.platformServiceDNSVersion,
	}
	if _, err := resources.CreateObjectsFromTemplateFile(ctx, config, psDNSTemplate, psDNSData); err != nil {
		return err
	}
	klog.Infof("create external-dns config for provider coredns backed by etcd (%s)", etcdIP)
	// Import platform service configs with retry logic since discovery api might take some time to pick the new ps-dns config type
	err := wait.For(func(ctx context.Context) (done bool, err error) {
		psDNSConfigTemplate := internal.MustTmpFileFromEmbedFS(configFS, "config/psconfig.yaml.tmpl")
		psDNSConfigData := struct {
			EtcdIP                  string
			ExternalDNSChartVersion string
			DNSZone                 string
		}{
			EtcdIP:                  etcdIP,
			ExternalDNSChartVersion: d.externalDNSChartVersion,
			DNSZone:                 d.dnsZone,
		}
		if _, err = resources.CreateObjectsFromTemplateFile(ctx, config, psDNSConfigTemplate, psDNSConfigData); err != nil {
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

// getLoadBalancerIP retrieves the first IP address of the service with key name/namespace
func getLoadBalancerIP(ctx context.Context, config *envconf.Config, name, namespace string) (string, error) {
	service := &corev1.Service{}
	service.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Service",
	})
	service.SetName(name)
	service.SetNamespace(namespace)
	if err := config.Client().Resources().Get(ctx, name, namespace, service); err != nil {
		return "", fmt.Errorf("failed to get Service '%s/%s': %v", namespace, name, err)
	}
	for _, ingress := range service.Status.LoadBalancer.Ingress {
		if ingress.IP != "" {
			return ingress.IP, nil
		}
	}
	return "", fmt.Errorf("service '%s/%s' does not have any IP addresses exposed", namespace, name)
}
