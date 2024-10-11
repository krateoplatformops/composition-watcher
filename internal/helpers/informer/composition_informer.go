package watcher

import (
	"fmt"
	"sync"

	watcher "github.com/krateoplatformops/composition-watcher/api/v1"
	httpHelper "github.com/krateoplatformops/composition-watcher/internal/helpers/http"
	statusGetter "github.com/krateoplatformops/composition-watcher/internal/helpers/kube/compositions"
	"github.com/krateoplatformops/provider-runtime/pkg/logging"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

type CompositionInformer struct {
	informerList map[types.UID]*cache.SharedIndexInformer
	stopChans    map[types.UID]chan struct{}
	mu           sync.Mutex
	logger       logging.Logger
}

func (r *CompositionInformer) InitCompositionInformer(log logging.Logger) {
	r.informerList = make(map[types.UID]*cache.SharedIndexInformer)
	r.stopChans = make(map[types.UID]chan struct{})
	r.logger = log
}

func (r *CompositionInformer) StartCompositionInformer(compositionReference watcher.CompositionReference, uid types.UID, config *rest.Config) error {
	gv, err := schema.ParseGroupVersion(compositionReference.Spec.Reference.ApiVersion)
	if err != nil {
		return fmt.Errorf("unable to parse GroupVersion from composition reference ApiVersion: %w", err)
	}
	gvr := schema.GroupVersionResource{
		Group:    gv.Group,
		Version:  gv.Version,
		Resource: compositionReference.Spec.Reference.Resource,
	}

	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("watcher error: %w", err)
	}

	fac := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynClient, 0, compositionReference.Spec.Reference.Namespace, nil)
	informer := fac.ForResource(gvr).Informer()

	r.mu.Lock()
	if _, ok := r.informerList[uid]; !ok {
		r.informerList[uid] = &informer
		stopChan := make(chan struct{})
		r.stopChans[uid] = stopChan
	}
	r.mu.Unlock()

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: func(obj interface{}) {
			item := obj.(*unstructured.Unstructured)
			deletedUID := item.GetUID()

			// Check if the event we receive is related to an object we are watching, otherwise do nothing
			if r.DoesInformerAlreadyExist(deletedUID) {

				r.mu.Lock()
				defer r.mu.Unlock()

				if stopChan, ok := r.stopChans[deletedUID]; ok {
					close(stopChan)
					delete(r.stopChans, deletedUID)
				}

				delete(r.informerList, deletedUID)
				r.logger.Info("Informer for has been stopped and removed from the map", "UID", deletedUID)

				err = httpHelper.Request("DELETE", fmt.Sprintf("/compositions/%s", deletedUID), nil)
				if err != nil {
					r.logger.Info(fmt.Sprintf("error with requested http resource: %s", err))
				}
				r.logger.Info("Deleted cache on webservice", "delete UID", deletedUID)

			}
		},
		UpdateFunc: func(oldObj interface{}, newObj interface{}) {
			item := newObj.(*unstructured.Unstructured)
			updatedUID := item.GetUID()

			// Check if the event we receive is related to an object we are watching, otherwise do nothing
			if r.DoesInformerAlreadyExist(updatedUID) {
				r.logger.Info("Informer has received an update for object in list", "UID", updatedUID)
			}

			updatedData, err := statusGetter.GetCompositionResourcesStatus(dynClient, item, compositionReference.Spec.Reference, compositionReference.Spec.Filters.Exclude, r.logger)
			if err != nil {
				r.logger.Info(fmt.Sprintf("error retrieving updated status information for resources of composition uid %s: %s", updatedUID, err))
			}

			err = httpHelper.Request("POST", fmt.Sprintf("/compositions/%s", updatedUID), updatedData)
			if err != nil {
				r.logger.Info(fmt.Sprintf("error with requested http resource: %s", err))
			}

		},
	})

	go informer.Run(r.stopChans[uid])
	return nil
}

func (r *CompositionInformer) DoesInformerAlreadyExist(uid types.UID) bool {
	_, ok := r.informerList[uid]
	return ok
}

func (r *CompositionInformer) DeleteInformer(uid types.UID) bool {
	if r.DoesInformerAlreadyExist(uid) {
		delete(r.informerList, uid)
		close(r.stopChans[uid])
		delete(r.stopChans, uid)
		return true
	}
	return false
}
