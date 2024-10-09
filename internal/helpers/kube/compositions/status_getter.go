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
	"github.com/krateoplatformops/provider-runtime/pkg/logging"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func GetCompositionResourcesStatus(dynClient *dynamic.DynamicClient, obj *unstructured.Unstructured, compositionReference watcher.Reference, excludes []watcher.Exclude, logger logging.Logger) ([]byte, error) {
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

	var result []Resource
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

		unstructuredRes, err := dynClient.Resource(gvr).Namespace(managedResource.Namespace).Get(context.TODO(), managedResource.Name, v1.GetOptions{})
		if err != nil {
			logger.Debug("error fetching resource status, trying with cluster-scoped", "error", err, "group", gvr.Group, "version", gvr.Version, "resource", gvr.Resource, "name", managedResource.Name, "namespace", managedResource.Namespace)
			unstructuredRes, err = dynClient.Resource(gvr).Get(context.TODO(), managedResource.Name, v1.GetOptions{})
			if err != nil {
				logger.Info(fmt.Sprintf("error fetching resource status: %s", err), "group", gvr.Group, "version", gvr.Version, "resource", gvr.Resource, "name", managedResource.Name, "namespace", "")
				continue
			}

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
	logger.Debug("webservice response", "json", string(jsonData))
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
