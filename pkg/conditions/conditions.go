package conditions

import (
	"context"
	"fmt"

	"github.com/christophrj/openmcp-testing/internal"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
)

// Match returns true if the conditionType of an object matches the conditionStatus.
// If an object is not found, the condition is not satisfied and no error is returned.
func Match(obj k8s.Object, cfg *envconf.Config, conditionType string, conditionStatus v1.ConditionStatus) wait.ConditionWithContextFunc {
	return func(ctx context.Context) (done bool, err error) {
		klog.Infof("%s: waiting for condition %s %s", fmtObj(obj), conditionType, conditionStatus)
		err = cfg.Client().Resources().Get(ctx, obj.GetName(), obj.GetNamespace(), obj)
		if err != nil {
			return false, internal.IgnoreNotFound(err)
		}
		return checkCondition(obj, conditionType, conditionStatus), nil
	}
}

// Status returns true if the status key of an object matches the status value.
// If an object is not found, the condition is not satisfied and no error is returned.
func Status(obj k8s.Object, cfg *envconf.Config, key string, value string) wait.ConditionWithContextFunc {
	return func(ctx context.Context) (done bool, err error) {
		klog.Infof("%s: waiting for status %s %s", fmtObj(obj), key, value)
		err = cfg.Client().Resources().Get(ctx, obj.GetName(), obj.GetNamespace(), obj)
		if err != nil {
			return false, internal.IgnoreNotFound(err)
		}
		u, err := internal.ToUnstructured(obj)
		if err != nil {
			return false, err
		}
		status, found, err := unstructured.NestedMap(u.Object, "status")
		if err != nil {
			return false, err
		}
		if !found {
			return false, nil
		}
		return status[key] == value, nil
	}
}

func checkCondition(k8sobj k8s.Object, desiredType string, desiredStatus v1.ConditionStatus) bool {
	fmtobj := fmtObj(k8sobj)
	u, err := internal.ToUnstructured(k8sobj)
	if err != nil {
		klog.Infof("%s: failed to convert object %v", fmtobj, err)
		return false
	}
	conditions, ok, err := unstructured.NestedSlice(u.UnstructuredContent(), "status", "conditions")
	if err != nil {
		klog.Infof("%s: failed to extract conditions %v", fmtobj, err)
		return false
	} else if !ok {
		klog.Infof("%s: does not have any conditions", fmtobj)
		return false
	}
	status := ""
	message := ""
	for _, condition := range conditions {
		c := condition.(map[string]interface{})
		curType := c["type"]
		if curType == desiredType {
			status = c["status"].(string)
			msg, convertible := c["message"].(string)
			if convertible {
				message = msg
			}
		}
	}
	matchedConditionStatus := status == string(desiredStatus)
	klog.Infof("%s condition %s: %s, message: %s", fmtobj, desiredType, status, message)
	return matchedConditionStatus
}

func fmtObj(obj k8s.Object) string {
	return fmt.Sprintf("Object (%s) %s/%s", obj.GetObjectKind().GroupVersionKind(), obj.GetNamespace(), obj.GetName())
}
