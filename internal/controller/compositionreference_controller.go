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
	"fmt"
	"time"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	watcher "github.com/krateoplatformops/composition-watcher/api/v1"
	httpHelper "github.com/krateoplatformops/composition-watcher/internal/helpers/http"
	informerHelper "github.com/krateoplatformops/composition-watcher/internal/helpers/informer"
	clientHelper "github.com/krateoplatformops/composition-watcher/internal/helpers/kube/client"
	statusGetter "github.com/krateoplatformops/composition-watcher/internal/helpers/kube/compositions"
)

// CompositionReferenceReconciler reconciles a CompositionRference object
type CompositionReferenceReconciler struct {
	client.Client
	Scheme              *runtime.Scheme
	CompositionInformer *informerHelper.CompositionInformer
	RequeueAfter        time.Duration
}

//+kubebuilder:rbac:groups=*,resources=*,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *CompositionReferenceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.Log.WithValues("CompositionWatcher", req.NamespacedName)
	var compositionReference watcher.CompositionReference
	var err error

	err = r.Get(ctx, req.NamespacedName, &compositionReference)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "unable to retrieve CompositionReference")
			return ctrl.Result{}, err
		}
		logger.Info("CompositionReference not found, ignoring since object must be deleted...")
		return ctrl.Result{}, nil
	}

	cfg, err := rest.InClusterConfig()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to retrieve rest.InClusterConfig: %w", err)
	}

	dynClient, err := clientHelper.New(cfg)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to create dynamic client: %w", err)
	}

	gv, err := schema.ParseGroupVersion(compositionReference.Spec.Reference.ApiVersion)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to parse GroupVersion from composition reference ApiVersion: %w", err)
	}
	gvr := schema.GroupVersionResource{
		Group:    gv.Group,
		Version:  gv.Version,
		Resource: compositionReference.Spec.Reference.Resource,
	}
	// Get structure to send to webservice
	obj, err := dynClient.Resource(gvr).Namespace(compositionReference.Spec.Reference.Namespace).Get(ctx, compositionReference.Spec.Reference.Name, v1.GetOptions{})
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to retrieve composition object: %w", err)
	}

	uid := obj.GetUID()
	if !r.CompositionInformer.DoesInformerAlreadyExist(uid) {
		r.CompositionInformer.StartCompositionInformer(compositionReference, uid, cfg)
	}

	updatedData, err := statusGetter.GetCompositionResourcesStatus(dynClient, obj, compositionReference.Spec.Reference, compositionReference.Spec.Filters.Exclude, logger)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error retrieving updated status information for resources of composition uid %s: %w", uid, err)
	}

	err = httpHelper.Request("POST", fmt.Sprintf("/compositions/%s", uid), updatedData)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error with requested http resource: %w", err)
	}

	logger.Info("End of reconcile")
	if r.RequeueAfter != time.Duration(0) {
		return ctrl.Result{RequeueAfter: r.RequeueAfter}, nil
	} else {
		return ctrl.Result{}, nil
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *CompositionReferenceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&watcher.CompositionReference{}).
		Complete(r)
}
