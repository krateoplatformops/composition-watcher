package compositions

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/go-logr/logr"
	watcher "github.com/krateoplatformops/composition-watcher/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func GetCompositionResourcesStatus(dynClient *dynamic.DynamicClient, obj *unstructured.Unstructured, compositionReference watcher.Reference, excludes []watcher.Exclude, logger logr.Logger) ([]byte, error) {
	resourceTreeJson := ResourceTreeJson{}
	resourceTreeJson.CreationTimestamp = metav1.Now()

	resourceTreeJson.Spec.Tree = make([]ResourceNode, 0)
	resourceTreeJson.Status = make([]*ResourceNodeStatus, 0)

	status, found, err := unstructured.NestedMap(obj.Object, "status")
	if err != nil {
		return nil, fmt.Errorf("error accessing 'status' field: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("could not find 'status' field in composition object")
	}

	managed, found := status["managed"]
	if !found {
		return nil, fmt.Errorf("could not find 'managed' field in composition object")
	}

	var managedResourceList []watcher.Reference

	// Check if managed is a slice
	managedSlice, ok := managed.([]interface{})
	if !ok {
		return nil, fmt.Errorf("'managed' field is not a slice as expected")
	}

	for _, m := range managedSlice {
		if mMap, ok := m.(map[string]interface{}); ok {
			ref := watcher.Reference{
				ApiVersion: mMap["apiVersion"].(string),
				Resource:   mMap["resource"].(string),
				Name:       mMap["name"].(string),
				Namespace:  mMap["namespace"].(string),
			}
			managedResourceList = append(managedResourceList, ref)
		}
	}

	for _, managedResource := range managedResourceList {
		skip := false
		for _, exclude := range excludes {
			if shouldItSkip(exclude, managedResource) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		gv, err := schema.ParseGroupVersion(managedResource.ApiVersion)
		if err != nil {
			return nil, fmt.Errorf("could not parse Group/Version of managed resource: %w", err)
		}

		gvr := schema.GroupVersionResource{
			Group:    gv.Group,
			Version:  gv.Version,
			Resource: managedResource.Resource,
		}

		unstructuredRes, err := dynClient.Resource(gvr).Namespace(managedResource.Namespace).Get(context.TODO(), managedResource.Name, metav1.GetOptions{})
		if err != nil {
			logger.V(1).Info("error fetching resource status, trying with cluster-scoped", "error", err, "group", gvr.Group, "version", gvr.Version, "resource", gvr.Resource, "name", managedResource.Name, "namespace", managedResource.Namespace)
			unstructuredRes, err = dynClient.Resource(gvr).Get(context.TODO(), managedResource.Name, metav1.GetOptions{})
			if err != nil {
				logger.Info(fmt.Sprintf("error fetching resource status: %s", err), "group", gvr.Group, "version", gvr.Version, "resource", gvr.Resource, "name", managedResource.Name, "namespace", "")
				continue
			}

		}

		var health Health

		// Extract status if available
		if unstructuredStatus, found, _ := unstructured.NestedMap(unstructuredRes.Object, "status"); found {
			if conditions, ok := unstructuredStatus["conditions"].([]interface{}); ok && len(conditions) > 0 {
				lastCondition := conditions[len(conditions)-1].(map[string]interface{})
				if value, ok := lastCondition["status"]; ok {
					health.Status = value.(string)
				}
				if value, ok := lastCondition["type"]; ok {
					health.Type = value.(string)
				}
				if value, ok := lastCondition["reason"]; ok {
					health.Reason = value.(string)
				}
				if value, ok := lastCondition["message"]; ok {
					health.Message = value.(string)
				}
			}
		}

		resourceNodeJsonSpec := ResourceNode{}
		resourceNodeJsonSpec.APIVersion = managedResource.ApiVersion
		resourceNodeJsonSpec.Resource = managedResource.Resource
		resourceNodeJsonSpec.Name = managedResource.Name
		resourceNodeJsonSpec.Namespace = managedResource.Namespace
		resourceNodeJsonSpec.ParentRefs = []watcher.Reference{compositionReference}
		resourceTreeJson.Spec.Tree = append(resourceTreeJson.Spec.Tree, resourceNodeJsonSpec)

		resourceNodeJsonStatus := ResourceNodeStatus{}
		time := unstructuredRes.GetCreationTimestamp()
		resourceNodeJsonStatus.CreatedAt = &time
		resourceNodeJsonStatus.Kind = unstructuredRes.GetKind()
		resourceNodeJsonStatus.Name = managedResource.Name
		resourceNodeJsonStatus.Namespace = managedResource.Namespace
		resourceNodeJsonStatus.Health = &health
		uidString := string(unstructuredRes.GetUID())
		resourceNodeJsonStatus.UID = &uidString
		resourceVersionString := unstructuredRes.GetResourceVersion()
		resourceNodeJsonStatus.ResourceVersion = &resourceVersionString
		resourceNodeJsonStatus.Version = unstructuredRes.GetAPIVersion()
		resourceNodeJsonStatus.Kind = unstructuredRes.GetKind()
		resourceNodeJsonStatus.Name = managedResource.Name
		resourceNodeJsonStatus.Namespace = managedResource.Resource
		resourceNodeJsonStatus.ParentRefs = []*ResourceNodeStatus{}

		resourceTreeJson.Spec.Tree = append(resourceTreeJson.Spec.Tree, resourceNodeJsonSpec)
		resourceTreeJson.Status = append(resourceTreeJson.Status, &resourceNodeJsonStatus)
	}

	resourceTree := ResourceTree{
		CompositionId: string(obj.GetUID()),
		Resources:     resourceTreeJson,
	}

	jsonData, err := json.Marshal(resourceTree)
	if err != nil {
		return []byte{}, fmt.Errorf("error marshaling composition resources status: %w", err)
	}
	logger.V(1).Info("webservice response", "json", string(jsonData))
	return jsonData, nil
}
