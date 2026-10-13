package resources

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/v2"
	"sigs.k8s.io/e2e-framework/klient/decoder"
	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/envconf"

	"github.com/openmcp-project/openmcp-testing/internal"
)

// CreateNObjectsFromTemplateFile creates n number of objects based on a given template file.
// Object names will be automatically iterated based on the provided count.
// Additional adjustments can be realized by passing any number of decoder options.
// Providing multiple resources in one file (separated by '---') will result in creating n objects of each resource.
func CreateNObjectsFromTemplateFile(ctx context.Context, c *envconf.Config, filePath string, count int, data any, opts ...decoder.DecodeOption) error {
	for i := 0; i < count; i++ {
		decoderOpts := slices.Clone(opts)
		decoderOpts = append(decoderOpts, MutateName(fmt.Sprintf("-%d", i)))
		_, err := CreateObjectsFromTemplateFile(ctx, c, filePath, data, decoderOpts...)
		if err != nil {
			return err
		}
	}
	return nil
}

// MutateNamespace is an optional parameter to decoding functions that will patch objects with the given namespace name
func MutateName(suffix string) decoder.DecodeOption {
	return decoder.MutateOption(func(obj k8s.Object) error {
		obj.SetName(fmt.Sprintf("%s%s", obj.GetName(), suffix))
		return nil
	})
}

// DeleteObject deletes the passed in object if it exists
func DeleteObject(ctx context.Context, c *envconf.Config, obj k8s.Object, options ...wait.Option) error {
	err := c.Client().Resources().Get(ctx, obj.GetName(), obj.GetNamespace(), obj)
	if err != nil {
		return internal.IgnoreNotFound(err)
	}
	if err = c.Client().Resources().Delete(ctx, obj); err != nil {
		return internal.IgnoreNotFound(err)
	}
	if options != nil {
		return wait.For(conditions.New(c.Client().Resources()).ResourceDeleted(obj), options...)
	}
	return nil
}

// CreateObjectsFromTemplateFile creates objects by first applying the passed data to a template file on the file system
func CreateObjectsFromTemplateFile(ctx context.Context, cfg *envconf.Config, filePath string, data interface{}, options ...decoder.DecodeOption) (*unstructured.UnstructuredList, error) {
	manifest, err := internal.ExecTemplateFile(filePath, data)
	if err != nil {
		return nil, err
	}
	return createObjectsFromManifest(ctx, cfg, manifest, options...)
}

// UpdateObjectsFromTemplateFile updates objects by first applying the passed data to a template file on the file system
func UpdateObjectsFromTemplateFile(ctx context.Context, cfg *envconf.Config, filePath string, data interface{}) error {
	manifest, err := internal.ExecTemplateFile(filePath, data)
	if err != nil {
		return err
	}
	r := strings.NewReader(manifest)
	return decoder.DecodeEach(ctx, r, decoder.UpdateHandler(cfg.Client().Resources()), decoder.MutateNamespace(cfg.Namespace()))
}

// CreateObjectFromTemplate creates a single object by first applying the passed in data to a template
func CreateObjectFromTemplate(ctx context.Context, cfg *envconf.Config, template string, data interface{}) (*unstructured.Unstructured, error) {
	manifest, err := internal.ExecTemplate(template, data)
	if err != nil {
		return nil, err
	}
	obj := &unstructured.Unstructured{}
	err = decoder.DecodeString(manifest, obj, decoder.MutateNamespace(cfg.Namespace()))
	if err != nil {
		return nil, err
	}
	if err = cfg.Client().Resources().Create(ctx, obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func createObjectsFromManifest(ctx context.Context, cfg *envconf.Config, manifest string, options ...decoder.DecodeOption) (*unstructured.UnstructuredList, error) {
	opts := []decoder.DecodeOption{
		decoder.MutateNamespace(cfg.Namespace()),
	}
	if options != nil {
		opts = options
	}
	r := strings.NewReader(manifest)
	list := &unstructured.UnstructuredList{}
	err := decoder.DecodeEach(ctx, r,
		func(ctx context.Context, obj k8s.Object) error {
			return createAndPopulateList(ctx, obj, list, cfg)
		}, opts...)
	return list, err
}

// CreateObjectsFromDir creates objects specified by a file on the file system
func CreateObjectsFromDir(ctx context.Context, cfg *envconf.Config, dir string) (*unstructured.UnstructuredList, error) {
	list := &unstructured.UnstructuredList{}
	err := decoder.DecodeEachFile(ctx, os.DirFS(dir), "*",
		func(ctx context.Context, obj k8s.Object) error {
			return createAndPopulateList(ctx, obj, list, cfg)
		}, decoder.MutateNamespace(cfg.Namespace()))
	return list, err
}

func createAndPopulateList(ctx context.Context, obj k8s.Object, list *unstructured.UnstructuredList, cfg *envconf.Config) error {
	u, err := internal.ToUnstructured(obj)
	if err != nil {
		return err
	}
	list.Items = append(list.Items, *u)
	klog.Infof("creating object (%s) %s/%s", obj.GetObjectKind().GroupVersionKind(), obj.GetNamespace(), obj.GetName())
	if os.Getenv("RESOURCES_DRY_RUN") != "true" {
		return decoder.CreateIgnoreAlreadyExists(cfg.Client().Resources())(ctx, obj)
	}
	return nil
}
