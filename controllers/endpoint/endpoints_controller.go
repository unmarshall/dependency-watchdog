// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package endpoint

import (
	"context"
	"time"

	wapi "github.com/gardener/dependency-watchdog/api/weeder"
	"github.com/gardener/dependency-watchdog/internal/weeder"
	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const controllerName = "endpoint"

// Reconciler EndpointReconciler reconciles an Endpoints object
type Reconciler struct {
	Client                  client.Client
	SeedClient              kubernetes.Interface
	WeederConfig            *wapi.Config
	WeederMgr               weeder.Manager
	MaxConcurrentReconciles int
}

// +kubebuilder:rbac:resources=endpoints,verbs=get;list;watch
// +kubebuilder:rbac:resources=pods,verbs=get;list;watch;delete

// Reconcile listens to create/update events for `Endpoints` resources and manages weeder which shoot the dependent pods of the configured services, if necessary
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	//Get the endpoint object
	var ep v1.Endpoints
	err := r.Client.Get(ctx, req.NamespacedName, &ep)
	if err != nil {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}
	log.Info("Starting a new weeder for endpoint, replacing old weeder, if any exists", "namespace", req.Namespace, "endpoint", ep.Name)
	r.startWeeder(ctx, log, req.Namespace, &ep)
	return ctrl.Result{}, nil
}

// startWeeder starts a new weeder for the endpoint
func (r *Reconciler) startWeeder(ctx context.Context, logger logr.Logger, namespace string, ep *v1.Endpoints) {
	w := weeder.NewWeeder(ctx, namespace, r.WeederConfig, r.Client, r.SeedClient, ep, logger)
	// Register the weeder
	r.WeederMgr.Register(*w)
	go w.Run()
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	c, err := controller.New(
		controllerName,
		mgr,
		controller.Options{
			MaxConcurrentReconciles: r.MaxConcurrentReconciles,
			Reconciler:              r},
	)
	if err != nil {
		return err
	}
	return c.Watch(
		source.Kind[client.Object](mgr.GetCache(), &v1.Endpoints{},
			&handler.EnqueueRequestForObject{},
			predicate.And[client.Object](
				predicate.ResourceVersionChangedPredicate{},
				MatchingEndpoints(r.WeederConfig.ServicesAndDependantSelectors),
				ReadyEndpoints(c.GetLogger()),
			),
		),
	)
}
