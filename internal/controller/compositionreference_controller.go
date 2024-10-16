/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/krateoplatformops/provider-runtime/pkg/controller"
	"github.com/krateoplatformops/provider-runtime/pkg/logging"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"

	watcher "github.com/krateoplatformops/composition-watcher/api/v1"
	httpHelper "github.com/krateoplatformops/composition-watcher/internal/helpers/http"
	informerHelper "github.com/krateoplatformops/composition-watcher/internal/helpers/informer"
	clientHelper "github.com/krateoplatformops/composition-watcher/internal/helpers/kube/client"
	statusGetter "github.com/krateoplatformops/composition-watcher/internal/helpers/kube/compositions"

	prv1 "github.com/krateoplatformops/provider-runtime/apis/common/v1"
	"github.com/krateoplatformops/provider-runtime/pkg/event"
	"github.com/krateoplatformops/provider-runtime/pkg/ratelimiter"
	"github.com/krateoplatformops/provider-runtime/pkg/reconciler"
	"github.com/krateoplatformops/provider-runtime/pkg/resource"
)

const (
	errNotCompositionReference = "managed resource is not a composition reference custom resource"
)

//+kubebuilder:rbac:groups=*,resources=*,verbs=get;list;watch
//+kubebuilder:rbac:groups=resourcetrees.krateo.io,resources=compositionreferences,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=resourcetrees.krateo.io,resources=compositionreferences/status,verbs=get;update;patch

func Setup(mgr ctrl.Manager, o controller.Options) error {
	name := reconciler.ControllerName(watcher.CompositionReferenceGroupKind)

	log := o.Logger.WithValues("controller", name)
	log.Info("controller", "name", name)

	recorder := mgr.GetEventRecorderFor(name)

	inf := &informerHelper.CompositionInformer{}
	inf.InitCompositionInformer(log)

	r := reconciler.NewReconciler(mgr,
		resource.ManagedKind(watcher.CompositionReferenceGroupVersionKind),
		reconciler.WithExternalConnecter(&connector{
			compositionInformer: inf,
			log:                 log,
			recorder:            recorder,
			pollInterval:        o.PollInterval,
		}),
		reconciler.WithPollInterval(o.PollInterval),
		reconciler.WithLogger(log),
		reconciler.WithRecorder(event.NewAPIRecorder(recorder)))

	log.Debug("polling rate", "rate", o.PollInterval)

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		WithOptions(o.ForControllerRuntime()).
		For(&watcher.CompositionReference{}).
		Complete(ratelimiter.New(name, r, o.GlobalRateLimiter))
}

type connector struct {
	compositionInformer *informerHelper.CompositionInformer
	pollInterval        time.Duration
	log                 logging.Logger
	recorder            record.EventRecorder
}

func (c *connector) Connect(ctx context.Context, mg resource.Managed) (reconciler.ExternalClient, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve rest.InClusterConfig: %w", err)
	}

	dynClient, err := clientHelper.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("unable to create dynamic client: %w", err)
	}

	return &external{
		cfg:                 cfg,
		dynClient:           dynClient,
		compositionInformer: c.compositionInformer,
		sinceLastUpdate:     make(map[string]time.Time),
		pollInterval:        c.pollInterval,
		log:                 c.log,
		rec:                 c.recorder,
	}, nil
}

type external struct {
	cfg                 *rest.Config
	compositionInformer *informerHelper.CompositionInformer
	dynClient           *dynamic.DynamicClient
	sinceLastUpdate     map[string]time.Time
	pollInterval        time.Duration
	log                 logging.Logger
	rec                 record.EventRecorder
}

func (c *external) Disconnect(_ context.Context) error {
	return nil // NOOP
}

func (e *external) Observe(ctx context.Context, mg resource.Managed) (reconciler.ExternalObservation, error) {
	cr, ok := mg.(*watcher.CompositionReference)
	if !ok {
		return reconciler.ExternalObservation{}, errors.New(errNotCompositionReference)
	}

	obj, err := e.getObj(ctx, cr)
	if err != nil {
		return reconciler.ExternalObservation{}, err
	}

	uid := obj.GetUID()
	if !e.compositionInformer.DoesInformerAlreadyExist(uid) {
		return reconciler.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	timeSinceLastUpdate, ok := e.sinceLastUpdate[cr.Name+cr.Namespace]
	if !ok {
		return reconciler.ExternalObservation{
			ResourceExists:   true,
			ResourceUpToDate: false,
		}, nil
	} else {
		if time.Since(timeSinceLastUpdate) > e.pollInterval {
			return reconciler.ExternalObservation{
				ResourceExists:   true,
				ResourceUpToDate: false,
			}, nil
		}
	}
	cr.SetConditions(prv1.Available())
	return reconciler.ExternalObservation{
		ResourceExists:   true,
		ResourceUpToDate: true,
	}, nil

}

func (e *external) Create(ctx context.Context, mg resource.Managed) error {
	cr, ok := mg.(*watcher.CompositionReference)
	if !ok {
		return errors.New(errNotCompositionReference)
	}

	cr.SetConditions(prv1.Creating())

	obj, err := e.getObj(ctx, cr)
	if err != nil {
		return err
	}

	uid := obj.GetUID()

	if err = e.compositionInformer.StartCompositionInformer(*cr, uid, e.cfg); err != nil {
		return err
	}

	e.rec.Eventf(cr, corev1.EventTypeNormal, "Completed create", "UID '%s'", uid)

	return nil
}

func (e *external) Update(ctx context.Context, mg resource.Managed) error {
	cr, ok := mg.(*watcher.CompositionReference)
	if !ok {
		return errors.New(errNotCompositionReference)
	}

	obj, err := e.getObj(ctx, cr)
	if err != nil {
		return err
	}

	uid := obj.GetUID()

	updatedData, err := statusGetter.GetCompositionResourcesStatus(e.dynClient, obj, cr.Spec.Reference, cr.Spec.Filters.Exclude, e.log)
	if err != nil {
		return fmt.Errorf("error retrieving updated status information for resources of composition uid %s: %w", uid, err)
	}

	err = httpHelper.Request("POST", fmt.Sprintf("/compositions/%s", uid), updatedData)
	if err != nil {
		return fmt.Errorf("error with requested http resource: %w", err)
	}

	e.rec.Eventf(cr, corev1.EventTypeNormal, "Completed updated", "UID '%s'", uid)
	e.sinceLastUpdate[cr.Name+cr.Namespace] = time.Now()
	return nil
}

func (e *external) Delete(ctx context.Context, mg resource.Managed) error {
	cr, ok := mg.(*watcher.CompositionReference)
	if !ok {
		return errors.New(errNotCompositionReference)
	}

	cr.SetConditions(prv1.Deleting())

	gv, err := schema.ParseGroupVersion(cr.Spec.Reference.ApiVersion)
	if err != nil {
		return fmt.Errorf("unable to parse GroupVersion from composition reference ApiVersion: %w", err)
	}
	gvr := schema.GroupVersionResource{
		Group:    gv.Group,
		Version:  gv.Version,
		Resource: cr.Spec.Reference.Resource,
	}
	// Get structure to send to webservice
	compositionObj, err := e.dynClient.Resource(gvr).Namespace(cr.Spec.Reference.Namespace).Get(context.Background(), cr.Spec.Reference.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("unable to retrieve composition object: %w", err)
	}

	deletedUID := compositionObj.GetUID()

	err = httpHelper.Request("DELETE", fmt.Sprintf("/compositions/%s", deletedUID), nil)
	if err != nil {
		return fmt.Errorf("error with requested http resource: %w", err)
	}

	if !e.compositionInformer.DeleteInformer(deletedUID) {
		e.log.Info("Could not delete informer for composotion", "uid", deletedUID)
	}

	delete(e.sinceLastUpdate, cr.Name+cr.Namespace)

	e.log.Debug("Deleted cache on webservice", "delete UID", deletedUID)
	e.rec.Eventf(cr, corev1.EventTypeNormal, "Deleted from cache", "UID '%s'", deletedUID)

	return nil
}

func (e *external) getObj(ctx context.Context, cr *watcher.CompositionReference) (*unstructured.Unstructured, error) {
	gv, err := schema.ParseGroupVersion(cr.Spec.Reference.ApiVersion)
	if err != nil {
		return nil, fmt.Errorf("unable to parse GroupVersion from composition reference ApiVersion: %w", err)
	}
	gvr := schema.GroupVersionResource{
		Group:    gv.Group,
		Version:  gv.Version,
		Resource: cr.Spec.Reference.Resource,
	}
	// Get structure to send to webservice
	res, err := e.dynClient.Resource(gvr).Namespace(cr.Spec.Reference.Namespace).Get(ctx, cr.Spec.Reference.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve resource %s with name %s in namespace %s, with apiVersion %s: %w", cr.Spec.Reference.Resource, cr.Spec.Reference.Name, cr.Spec.Reference.Namespace, cr.Spec.Reference.ApiVersion, err)
	}
	return res, nil
}
