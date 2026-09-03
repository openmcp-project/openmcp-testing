package dns

import (
	"context"
	"embed"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
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

type Setup struct {
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
	// apiServerConfig is used to adjust the kind api server dns settings
	apiServerConfig apiserver.Configurator
}

type Option func(*Setup)

func WithClusterName(name string) Option {
	return func(s *Setup) {
		s.clusterName = name
	}
}

func WithNamespace(namespace string) Option {
	return func(s *Setup) {
		s.namespace = namespace
	}
}

func WithClusterPurpose(purpose string) Option {
	return func(s *Setup) {
		s.clusterPurpose = purpose
	}
}

func WithExternalDNSChartVersion(version string) Option {
	return func(s *Setup) {
		s.externalDNSChartVersion = version
	}
}

func WithEtcdVersion(version string) Option {
	return func(s *Setup) {
		s.etcdVersion = version
	}
}

func WithCoreDNSChartVersion(version string) Option {
	return func(s *Setup) {
		s.coreDNSChartVersion = version
	}
}

func WithPlatformServiceDNSVersion(version string) Option {
	return func(s *Setup) {
		s.platformServiceDNSVersion = version
	}
}

func WithConfigurator(c apiserver.Configurator) Option {
	return func(s *Setup) {
		s.apiServerConfig = c
	}
}

func WithDNSZone(zone string) Option {
	return func(s *Setup) {
		s.dnsZone = zone
	}
}

func NewSetup(t *testing.T, opts ...Option) *Setup {
	cfg := &Setup{
		clusterName:               defaultDNSClusterName,
		namespace:                 defaultNamespace,
		clusterPurpose:            defaultDNSClusterPurpose,
		externalDNSChartVersion:   defaultExternalDNSChartVersion,
		platformServiceDNSVersion: defaultPlatformServiceDNSVersion,
		etcdVersion:               defaultEtcdVersion,
		coreDNSChartVersion:       defaultCoreDNSChartVersion,
		dnsZone:                   defaultDNSZone,
		apiServerConfig:           *apiserver.NewConfigurator(t),
	}
	for _, o := range opts {
		o(cfg)
	}
	return cfg
}

// CreateExternalService deploys CoreDNS + etcd to a dedicated "dns" cluster together with platform service DNS to resemble a production like external DNS service.
func (s *Setup) CreateExternalService() features.Func {
	return func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
		runtime.Must(gatewayv1.Install(c.Client().Resources().GetScheme()))
		runtime.Must(gatewayv1alpha2.Install(c.Client().Resources().GetScheme()))
		// create dns cluster
		if err := createCluster(ctx, c, ClusterRequest{
			Name:      s.clusterName,
			Namespace: s.namespace,
			Purpose:   s.clusterPurpose,
		}); err != nil {
			t.Fatalf("failed to create dns cluster: %v", err)
		}
		dnsClusterConfig, err := clusterutils.ConfigByPrefix(s.clusterName, "default")
		if err != nil {
			t.Fatalf("failed to retrieve dns cluster config: %v", err)
		}
		// deploy etcd and coredns to dns cluster
		etcdTemplate := internal.MustTmpFileFromEmbedFS(configFS, "config/etcd.yaml.tmpl")
		etcdData := struct {
			EtcdVersion string
		}{
			EtcdVersion: s.etcdVersion,
		}
		if _, err := resources.CreateObjectsFromTemplateFile(ctx, dnsClusterConfig, etcdTemplate, etcdData); err != nil {
			t.Fatalf("failed to deploy etcd for dns: %v", err)
		}
		klog.Info("successfully deployed etcd to dns cluster")
		coreDNSTemplate := internal.MustTmpFileFromEmbedFS(configFS, "config/coredns.yaml.tmpl")
		coreDNSData := struct {
			Namespace           string
			ClusterName         string
			CoreDNSChartVersion string
			DNSZone             string
		}{
			Namespace:           s.namespace,
			ClusterName:         s.clusterName,
			CoreDNSChartVersion: s.coreDNSChartVersion,
			DNSZone:             s.dnsZone,
		}
		if _, err := resources.CreateObjectsFromTemplateFile(ctx, c, coreDNSTemplate, coreDNSData); err != nil {
			t.Fatalf("failed to deploy coredns: %v", err)
		}
		klog.Info("successfully deployed coredns to dns cluster")
		// install platform-service-dns and configure external-dns to use the dedicated dns cluster etcd
		etcdIP := getLoadBalancerIP(ctx, t, dnsClusterConfig, "etcd-external", "default")
		psDNSConfig := platformServiceDNSConfig{
			Version:                 s.platformServiceDNSVersion,
			EtcdIP:                  etcdIP,
			ExternalDNSChartVersion: s.externalDNSChartVersion,
			DNSZone:                 s.dnsZone,
		}
		if err := createPlatformServiceDNS(ctx, t, c, psDNSConfig); err != nil {
			t.Fatalf("failed to create platform service dns config: %v", err)
		}
		// inject additional nameserver into kube-apiserver
		nameserverIP := getLoadBalancerIP(ctx, t, dnsClusterConfig, "coredns", "default")
		if err := s.apiServerConfig.AddNameserver(nameserverIP); err != nil {
			t.Fatalf("failed to add host to kube-apiserver: %v", err)
		}
		return ctx
	}
}

// InjectTLSRouteHostAlias adds the retrieves the hostname of a TLSRoute and IP of a Gatway to Pod.Spec.HostAliases of a kube-apiserver.
// The function waits for the kubelet to restart the kube-apiserver.
func (s *Setup) InjectTLSRouteHostAlias(gateway, tlsRoute types.NamespacedName) features.Func {
	return func(ctx context.Context, t *testing.T, c *envconf.Config) context.Context {
		runtime.Must(gatewayv1.Install(c.Client().Resources().GetScheme()))
		runtime.Must(gatewayv1alpha2.Install(c.Client().Resources().GetScheme()))
		// inject host into kube-apiserver
		gwIP := getGatewayIP(ctx, t, c, gateway.Name, gateway.Namespace)
		wbHostname := getHostname(ctx, t, c, tlsRoute.Name, tlsRoute.Namespace)
		if err := s.apiServerConfig.AddHostAlias(wbHostname, gwIP); err != nil {
			t.Fatalf("failed to add host to kube-apiserver: %v", err)
		}
		return ctx
	}
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
	psDNSTemplate := internal.MustTmpFileFromEmbedFS(configFS, "config/ps.yaml.tmpl")
	if _, err := resources.CreateObjectsFromTemplateFile(ctx, config, psDNSTemplate, dnsConfig); err != nil {
		return err
	}
	klog.Infof("create external-dns config for provider coredns backed by etcd (%s)", dnsConfig.EtcdIP)
	// Import platform service configs with retry logic since discovery api might take some time to pick the new ps-dns config type
	err := wait.For(func(ctx context.Context) (done bool, err error) {
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
