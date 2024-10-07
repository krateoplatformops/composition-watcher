package watcher

import (
	"context"
	"fmt"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/log"

	watcher "github.com/krateoplatformops/composition-watcher/api/v1"
	httpHelper "github.com/krateoplatformops/composition-watcher/internal/helpers/http"
)

func StartCompositionReferenceInformer() {
	logger := log.Log.WithValues("CompositionReferenceInformer", "cluster-wide")
	inClusterConfig, _ := rest.InClusterConfig()

	inClusterConfig.APIPath = "/apis"
	inClusterConfig.GroupVersion = &watcher.GroupVersion

	dynClient, err := dynamic.NewForConfig(inClusterConfig)
	if err != nil {
		panic(err)
	}

	fac := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynClient, 0, "", nil)
	informer := fac.ForResource(schema.GroupVersionResource{
		Group:    watcher.GroupVersion.Group,
		Version:  watcher.GroupVersion.Version,
		Resource: "compositionreferences",
	}).Informer()

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: func(obj interface{}) {
			item := obj.(*unstructured.Unstructured)

			reference, found, err := unstructured.NestedMap(item.Object, "spec")
			if err != nil {
				logger.Error(err, "could not obtain composition reference field \"spec\" from deleted CompositionReference")
				return
			}
			if !found {
				logger.Info("could not find composition reference field \"spec\" from deleted CompositionReference")
				return
			}

			var referenceStruct watcher.Reference
			if referenceStructUntyped, ok := reference["reference"]; ok {
				referenceStruct = watcher.Reference{
					ApiVersion: referenceStructUntyped.(map[string]interface{})["apiVersion"].(string),
					Resource:   referenceStructUntyped.(map[string]interface{})["resource"].(string),
					Name:       referenceStructUntyped.(map[string]interface{})["name"].(string),
					Namespace:  referenceStructUntyped.(map[string]interface{})["namespace"].(string),
				}
			} else {
				logger.Info("could not find composition reference field \"spec.reference\" from deleted CompositionReference")
				return
			}

			gv, err := schema.ParseGroupVersion(referenceStruct.ApiVersion)
			if err != nil {
				logger.Error(err, "unable to parse GroupVersion from composition reference ApiVersion")
				return
			}
			gvr := schema.GroupVersionResource{
				Group:    gv.Group,
				Version:  gv.Version,
				Resource: referenceStruct.Resource,
			}
			// Get structure to send to webservice
			compositionObj, err := dynClient.Resource(gvr).Namespace(referenceStruct.Namespace).Get(context.Background(), referenceStruct.Name, v1.GetOptions{})
			if err != nil {
				logger.Error(err, "unable to retrieve composition object")
				return
			}

			deletedUID := compositionObj.GetUID()

			err = httpHelper.Request("DELETE", fmt.Sprintf("/compositions/%s", deletedUID), nil)
			if err != nil {
				logger.Error(err, "error with requested http resource")
			}
			logger.Info("Deleted cache on webservice", "delete UID", deletedUID)
		},
	})

	go informer.Run(make(<-chan struct{}))
}
