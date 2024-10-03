package watcher

import (
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

var (
	informerList map[types.UID]*cache.SharedIndexInformer
	stopChans    map[types.UID]chan struct{}
	mu           sync.Mutex
)

func InitWatcher() {
	informerList = make(map[types.UID]*cache.SharedIndexInformer)
	stopChans = make(map[types.UID]chan struct{})
}

func StartWatcher(gvr schema.GroupVersionResource, namespace string, uid types.UID, config *rest.Config) error {
	clientSet, err := dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("watcher error: %w", err)
	}

	fac := dynamicinformer.NewFilteredDynamicSharedInformerFactory(clientSet, 0, namespace, nil)
	informer := fac.ForResource(gvr).Informer()

	mu.Lock()
	if _, ok := informerList[uid]; !ok {
		informerList[uid] = &informer
		stopChan := make(chan struct{})
		stopChans[uid] = stopChan
	}
	mu.Unlock()

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: func(obj interface{}) {
			item := obj.(*unstructured.Unstructured)
			deletedUID := item.GetUID()

			// Check if the event we receive is related to an object we are watching, otherwise do nothing
			if DoesWatcherAlreadyExist(deletedUID) {

				mu.Lock()
				defer mu.Unlock()

				if stopChan, ok := stopChans[deletedUID]; ok {
					close(stopChan)
					delete(stopChans, deletedUID)
				}

				delete(informerList, deletedUID)
				fmt.Printf("Informer for UID %s has been stopped and removed from the map\n", deletedUID)
			}
		},
		UpdateFunc: func(oldObj interface{}, newObj interface{}) {
			item := newObj.(*unstructured.Unstructured)
			updatedUID := item.GetUID()

			// Check if the event we receive is related to an object we are watching, otherwise do nothing
			if DoesWatcherAlreadyExist(updatedUID) {
				fmt.Printf("Informer for UID %s has received an update for object in list\n", updatedUID)
			}
		},
	})

	go informer.Run(stopChans[uid])
	return nil
}

func DoesWatcherAlreadyExist(uid types.UID) bool {
	_, ok := informerList[uid]
	return ok
}
