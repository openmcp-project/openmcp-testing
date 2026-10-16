package prometheus

import (
	"context"
	"embed"
	"encoding/base64"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/pkg/envconf"

	"github.com/openmcp-project/openmcp-testing/internal"
	"github.com/openmcp-project/openmcp-testing/pkg/resources"
	"github.com/openmcp-project/openmcp-testing/pkg/setup/extensions"
)

const (
	defaultNamespace = "monitoring"
	defaultVersion   = "v3.4.1"
	defaultImage     = "prom/prometheus"
)

//go:embed config/*
var configFS embed.FS

var _ extensions.Extension = Prometheus{}

// RemoteCluster describes an additional cluster to scrape.
type RemoteCluster struct {
	// Name is a unique (prometheus) label applied to metrics from this cluster (cluster="<Name>").
	Name string
	// KubeconfigSecret is the name of the Secret (in SecretNamespace) whose "kubeconfig" key holds
	// a valid kubeconfig for the remote cluster.
	KubeconfigSecret string
	// SecretNamespace is the namespace of KubeconfigSecret.
	// If empty, defaults to the Prometheus installation namespace.
	SecretNamespace string
}

type Prometheus struct {
	// Version defines the Prometheus version to install.
	// If empty, defaults to "v3.4.1".
	Version string
	// Namespace is the Kubernetes namespace where Prometheus will be installed.
	// If empty, defaults to "monitoring".
	Namespace string
	// ScrapeServiceProviders restricts scraping to pods belonging to the named service providers.
	// When empty, all annotated pods across all namespaces are scraped.
	ScrapeServiceProviders []string
	// RemoteClusters lists additional clusters whose pods Prometheus should scrape.
	RemoteClusters []RemoteCluster
}

// Name implements [extensions.Extension].
func (p Prometheus) Name() string {
	return "prometheus"
}

// Install implements [extensions.Extension].
func (p Prometheus) Install(ctx context.Context, cfg *envconf.Config) error {
	klog.Info("installing prometheus...")
	namespace := p.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}
	version := p.Version
	if version == "" {
		version = defaultVersion
	}
	if err := createIfNotExists(ctx, cfg, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}); err != nil {
		return err
	}
	if err := rbac(ctx, cfg, namespace); err != nil {
		return err
	}
	for _, rc := range p.RemoteClusters {
		if err := copyKubeconfigSecret(ctx, cfg, rc, namespace); err != nil {
			return err
		}
	}
	if err := configMap(ctx, cfg, namespace, p.ScrapeServiceProviders, p.RemoteClusters); err != nil {
		return err
	}
	if err := deployment(ctx, cfg, namespace, version, p.RemoteClusters); err != nil {
		return err
	}
	klog.Infof("prometheus installed successfully in namespace %s", namespace)
	return nil
}

// RegisterSchemes implements [extensions.Extension].
func (p Prometheus) RegisterSchemes(_ context.Context, _ *runtime.Scheme) error {
	return nil
}

func createIfNotExists(ctx context.Context, cfg *envconf.Config, obj k8s.Object) error {
	if err := cfg.Client().Resources().Create(ctx, obj); err != nil {
		if !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("failed to create %T %s: %w", obj, obj.GetName(), err)
		}
	}
	return nil
}

// copyKubeconfigSecret reads the cluster kubeconfigs from a source namespace and creates
// a copy in a destination namespace so the Prometheus pod can mount it.
// A secret copy additional contains "ca.crt" and "token" keys based on the kubeconfig to use in the prometheus config.
//
// TODO: Instead of copying fixed secrets the idea is to expose a function for devs to add a workload or control plane cluster scrape config on demand
// by passing the cluster name which results in creating an access requests with the set of required permissions (see prometheus rbac), calling
// calling this secret transformation function, updating the config and reloading prometheus (e.g. using reloader).
func copyKubeconfigSecret(ctx context.Context, cfg *envconf.Config, rc RemoteCluster, destNamespace string) error {
	srcNamespace := rc.SecretNamespace
	if srcNamespace == "" {
		srcNamespace = destNamespace
	}
	if srcNamespace == destNamespace {
		return nil
	}
	src := &corev1.Secret{}
	if err := cfg.Client().Resources(srcNamespace).Get(ctx, rc.KubeconfigSecret, srcNamespace, src); err != nil {
		return fmt.Errorf("failed to get kubeconfig secret %s/%s: %w", srcNamespace, rc.KubeconfigSecret, err)
	}

	kubeconfigBytes, ok := src.Data["kubeconfig"]
	if !ok {
		return fmt.Errorf("kubeconfig secret %s/%s has no \"kubeconfig\" key", srcNamespace, rc.KubeconfigSecret)
	}
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		return fmt.Errorf("failed to parse kubeconfig from secret %s/%s: %w", srcNamespace, rc.KubeconfigSecret, err)
	}

	var caData []byte
	switch {
	case len(restConfig.CAData) > 0:
		caData = restConfig.CAData
	case restConfig.CAFile != "":
		return fmt.Errorf("kubeconfig secret %s/%s references a CA file path which is not supported in-cluster", srcNamespace, rc.KubeconfigSecret)
	}

	tokenBytes := []byte(restConfig.BearerToken)
	if len(tokenBytes) == 0 && restConfig.BearerTokenFile != "" {
		return fmt.Errorf("kubeconfig secret %s/%s references a bearer token file path which is not supported in-cluster", srcNamespace, rc.KubeconfigSecret)
	}
	if len(tokenBytes) == 0 {
		// Try base64-decoding in case the token is stored encoded (some generators do this)
		if raw, err := base64.StdEncoding.DecodeString(string(src.Data["token"])); err == nil {
			tokenBytes = raw
		} else {
			tokenBytes = src.Data["token"]
		}
	}

	data := map[string][]byte{
		"kubeconfig": kubeconfigBytes,
	}
	if len(caData) > 0 {
		data["ca.crt"] = caData
	}
	if len(tokenBytes) > 0 {
		data["token"] = tokenBytes
	}

	copy := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rc.KubeconfigSecret,
			Namespace: destNamespace,
		},
		Data: data,
		Type: src.Type,
	}
	return createIfNotExists(ctx, cfg, copy)
}

func rbac(ctx context.Context, c *envconf.Config, namespace string) error {
	rbacTemplate := internal.MustTmpFileFromEmbedFS(configFS, "config/rbac.yaml.tmpl")
	templateData := struct {
		Namespace string
	}{
		Namespace: namespace,
	}
	if _, err := resources.CreateObjectsFromTemplateFile(ctx, c.WithNamespace(namespace), rbacTemplate, templateData); err != nil {
		return fmt.Errorf("failed to create prometheus rbac: %w", err)
	}
	return nil
}

func configMap(ctx context.Context, c *envconf.Config, namespace string, scrapeServiceProviders []string, remoteClusters []RemoteCluster) error {
	parts := make([]string, len(scrapeServiceProviders))
	for i, ns := range scrapeServiceProviders {
		parts[i] = "sp-" + ns + "-.*"
	}
	configTemplate := internal.MustTmpFileFromEmbedFS(configFS, "config/config.yaml.tmpl")
	templateData := struct {
		Regex          string
		RemoteClusters []RemoteCluster
	}{
		Regex:          strings.Join(parts, "|"),
		RemoteClusters: remoteClusters,
	}
	if _, err := resources.CreateObjectsFromTemplateFile(ctx, c.WithNamespace(namespace), configTemplate, templateData); err != nil {
		return fmt.Errorf("failed create prometheus config: %w", err)
	}
	return nil
}

func deployment(ctx context.Context, c *envconf.Config, namespace, version string, remoteClusters []RemoteCluster) error {
	deployTemplate := internal.MustTmpFileFromEmbedFS(configFS, "config/deployment.yaml.tmpl")
	templateData := struct {
		Namespace      string
		Image          string
		RemoteClusters []RemoteCluster
	}{
		Namespace:      namespace,
		Image:          defaultImage + ":" + version,
		RemoteClusters: remoteClusters,
	}
	if _, err := resources.CreateObjectsFromTemplateFile(ctx, c.WithNamespace(namespace), deployTemplate, templateData); err != nil {
		return fmt.Errorf("failed to create prometheus deployment: %w", err)
	}
	return nil
}
