package compositions

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	watcher "github.com/krateoplatformops/composition-watcher/api/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func GetCompositionResourcesStatus(dynClient *dynamic.DynamicClient, obj *unstructured.Unstructured, compositionReference watcher.Reference) ([]byte, error) {
	resourceList, found, _ := unstructured.NestedSlice(obj.Object, "managed")
	var managedResourceList []watcher.Reference
	if found {
		for _, item := range resourceList {
			managedResourceList = append(managedResourceList, item.(watcher.Reference))
		}
	} else {
		return []byte{}, fmt.Errorf("could not find 'managed' field in composition object")
	}

	var result []Resource
	for _, managedResource := range managedResourceList {
		gv, err := schema.ParseGroupVersion(managedResource.ApiVersion)
		if err != nil {
			return []byte{}, fmt.Errorf("could not parse Group/Version of managed resource: %w", err)
		}
		gvr := schema.GroupVersionResource{
			Group:    gv.Group,
			Version:  gv.Version,
			Resource: managedResource.Resource,
		}

		unstructuredRes, err := dynClient.Resource(gvr).Namespace(managedResource.Namespace).Get(context.TODO(), managedResource.Name, v1.GetOptions{})
		if err != nil {
			return []byte{}, fmt.Errorf("error fetching resource status: %w", err)
		}

		var status Status

		// Extract metadata
		status.ResourceVersion = unstructuredRes.GetResourceVersion()
		status.UID = string(unstructuredRes.GetUID())
		status.CreatedAt = unstructuredRes.GetCreationTimestamp().Time

		// Extract status if available
		if unstructuredStatus, found, _ := unstructured.NestedMap(unstructuredRes.Object, "status"); found {
			if conditions, ok := unstructuredStatus["conditions"].([]interface{}); ok && len(conditions) > 0 {
				lastCondition := conditions[len(conditions)-1].(map[string]interface{})
				status.Health = Health{}
				if value, ok := lastCondition["status"]; ok {
					status.Health.Status = value.(string)
				}
				if value, ok := lastCondition["type"]; ok {
					status.Health.Type = value.(string)
				}
				if value, ok := lastCondition["reason"]; ok {
					status.Health.Reason = value.(string)
				}
				if value, ok := lastCondition["message"]; ok {
					status.Health.Message = value.(string)
				}
			}
		}
		resource := Resource{
			APIVersion: managedResource.ApiVersion,
			Resource:   managedResource.Resource,
			Name:       managedResource.Name,
			Namespace:  managedResource.Namespace,
			Status:     status,
			ParentRefs: []watcher.Reference{
				compositionReference,
			},
		}
		result = append(result, resource)
	}

	resourceTree := ResourceTree{
		CompositionId: string(obj.GetUID()),
		Resources:     result,
	}

	jsonData, err := json.Marshal(resourceTree)
	if err != nil {
		return []byte{}, fmt.Errorf("error marshaling composition resources status: %w", err)
	}
	fmt.Println("json result:", string(jsonData))
	return jsonData, nil
}

type ResourceTree struct {
	CompositionId string     `json:"compositionId"`
	Resources     []Resource `json:"resources"`
}

type Resource struct {
	APIVersion string              `json:"apiVersion"`
	Resource   string              `json:"resource"`
	Name       string              `json:"name"`
	Namespace  string              `json:"namespace,omitempty"`
	ParentRefs []watcher.Reference `json:"parentRefs,omitempty"`
	Status     Status              `json:"status,omitempty"`
}

type Status struct {
	CreatedAt       time.Time `json:"createdAt"`
	ResourceVersion string    `json:"resourceVersion"`
	UID             string    `json:"uid"`
	Health          Health    `json:"health,omitempty"`
}

type Health struct {
	Status  string `json:"status,omitempty"`
	Type    string `json:"type,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}
