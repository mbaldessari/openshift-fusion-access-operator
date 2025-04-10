/*
Copyright 2025.

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
	"strings"

	mfc "github.com/manifestival/controller-runtime-client"
	"github.com/manifestival/manifestival"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/openshift-storage-scale/openshift-fusion-access-operator/api/v1alpha1"
	fusionv1alpha "github.com/openshift-storage-scale/openshift-fusion-access-operator/api/v1alpha1"
	"github.com/openshift-storage-scale/openshift-fusion-access-operator/internal/controller/console"
	"github.com/openshift-storage-scale/openshift-fusion-access-operator/internal/controller/localvolumediscovery"
	"github.com/openshift-storage-scale/openshift-fusion-access-operator/internal/controller/machineconfig"
	"github.com/openshift-storage-scale/openshift-fusion-access-operator/internal/utils"
)

type CanPullImageFunc func(ctx context.Context, client kubernetes.Interface, ns, image string) (bool, error)

// FusionAccessReconciler reconciles a FusionAccess object
type FusionAccessReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	config        *rest.Config
	dynamicClient dynamic.Interface
	fullClient    kubernetes.Interface
	// Need this so I can mock it when needed
	CanPullImage CanPullImageFunc
}

func NewFusionAccessReconciler(client client.Client, scheme *runtime.Scheme) *FusionAccessReconciler {
	return &FusionAccessReconciler{
		Client:       client,
		Scheme:       scheme,
		CanPullImage: utils.CanPullImage,
	}
}

// const storageScaleFinalizer = "fusion.storage.openshift.io/finalizer"

// Basic Operator RBACs
//+kubebuilder:rbac:groups=fusion.storage.openshift.io,resources=fusionaccesses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=fusion.storage.openshift.io,resources=fusionaccesses/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=fusion.storage.openshift.io,resources=fusionaccesses/finalizers,verbs=update

// Operator needs to create some machine configs
//+kubebuilder:rbac:groups=machineconfiguration.openshift.io,resources=machineconfigs,verbs=get;list;watch;create;update;patch;delete

// Below rules are inserted via `make rbac-generate` automatically
// IBM_RBAC_MARKER_START
//+kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations,verbs=list;watch;delete;update;get;create;patch
//+kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingwebhookconfigurations,verbs=list;watch;delete;update;get;create;patch
//+kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=list;watch;delete;update;get;create;patch
//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=create;delete;get;list;update;watch
//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=create;get;list;patch;watch
//+kubebuilder:rbac:groups=apps,resources=deployments/finalizers,verbs=get;update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=create;delete;get;list;update;watch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=list;watch;delete;update;get;create;patch
//+kubebuilder:rbac:groups=apps,resources=replicasets,verbs=create;delete;get;list;update;watch
//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=create;delete;get;list;patch
//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=create;delete;get;list;update;watch
//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list
//+kubebuilder:rbac:groups=config.openshift.io,resources=clusterversions,verbs=get;list;watch
//+kubebuilder:rbac:groups=config.openshift.io,resources=dnses,verbs=get;list;watch
//+kubebuilder:rbac:groups=config.openshift.io,resources=infrastructures,verbs=get;list;watch
//+kubebuilder:rbac:groups=config.openshift.io,resources=networks,verbs=get;list;watch
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=create;delete;get;list;update;watch
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=csi.ibm.com,resources=csiscaleoperators/finalizers,verbs=update
//+kubebuilder:rbac:groups=csi.ibm.com,resources=csiscaleoperators/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=csi.ibm.com,resources=csiscaleoperators,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=csi.ibm.com,resources=*,verbs=*
//+kubebuilder:rbac:groups=discovery.k8s.io,resources=endpointslices,verbs=get;list;watch
//+kubebuilder:rbac:groups=machineconfiguration.openshift.io,resources=machineconfigpools,verbs=get;list;watch
//+kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=create;get
//+kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies/finalizers,verbs=update
//+kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=oauth.openshift.io,resources=oauthclients,verbs=create;get;list;patch;watch
//+kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=create;delete;get;list;patch;watch
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=*
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=list;watch;delete;update;get;create;patch
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=*
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=list;watch;delete;update;get;create;patch
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=list;watch;delete;update;get;create;patch
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=list;watch;delete;update;get;create;patch
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=*
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=list;watch;delete;update;get;create;patch
//+kubebuilder:rbac:groups="",resources=configmap,verbs=create;get;list;patch;update;watch
//+kubebuilder:rbac:groups="",resources=endpoints/restricted,verbs=create;get;list;patch;update;watch
//+kubebuilder:rbac:groups="",resources=endpoints,verbs=*
//+kubebuilder:rbac:groups="",resources=endpoints,verbs=create;get;list;patch;update;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=*
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch
//+kubebuilder:rbac:groups="",resources=leases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;patch;watch
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=list;watch;delete;update;get;create;patch
//+kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;patch;watch
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims/status,verbs=get
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=*
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups="",resources=persistentvolumes,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups="",resources=PersistentVolume,verbs=get;list
//+kubebuilder:rbac:groups="",resources=pods/eviction,verbs=create
//+kubebuilder:rbac:groups="",resources=pods/exec,verbs=create
//+kubebuilder:rbac:groups="",resources=pods/status,verbs=get;list;patch;update;watch
//+kubebuilder:rbac:groups="",resources=pods,verbs=*
//+kubebuilder:rbac:groups="",resources=pods,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list
//+kubebuilder:rbac:groups="",resources=secrets/status,verbs=*
//+kubebuilder:rbac:groups="",resources=secrets,verbs=*
//+kubebuilder:rbac:groups="",resources=secrets,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=*
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=create;delete;patch
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=list;watch;delete;update;get;create;patch
//+kubebuilder:rbac:groups="",resources=services/finalizers,verbs=*
//+kubebuilder:rbac:groups="",resources=services/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=services,verbs=*
//+kubebuilder:rbac:groups="",resources=services,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups="",resources=services,verbs=create;delete;patch
//+kubebuilder:rbac:groups="",resources=services,verbs=get;list
//+kubebuilder:rbac:groups="",resources=services,verbs=list;watch;delete;update;get;create;patch
//+kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=create;get;list;patch;update;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=approvalrequests/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=approvalrequests,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=asyncreplications/finalizers,verbs=update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=asyncreplications/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=asyncreplications,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=asyncreplications,verbs=get;list;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=callhomes/finalizers,verbs=update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=callhomes/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=callhomes,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=cloudcsidisks/finalizers,verbs=update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=cloudcsidisks/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=cloudcsidisks,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=clusterinterconnects,verbs=get;list;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=clusters/finalizers,verbs=update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=clusters/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=clusters,verbs=create
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=clusters,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=clusters,verbs=get
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=compressionjobs/finalizers,verbs=update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=compressionjobs/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=compressionjobs,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=consistencygroups/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=consistencygroups,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=consistencygroups,verbs=get;list;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=daemons/finalizers,verbs=update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=daemons/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=daemons,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=diskjobs/finalizers,verbs=update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=diskjobs/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=diskjobs,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=dnss/finalizers,verbs=update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=dnss/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=dnss,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=encryptionconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=encryptionconfigs/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=encryptionconfigs,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=filesystems/finalizers,verbs=update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=filesystems/status,verbs=get;list;patch;update;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=filesystems,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=*/finalizers,verbs=update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=grafanabridges/finalizers,verbs=update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=grafanabridges/status,verbs=delete;get;patch;update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=grafanabridges,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=guis/finalizers,verbs=update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=guis/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=guis,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=localdisks/finalizers,verbs=update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=localdisks/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=localdisks,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=pmcollectors/finalizers,verbs=update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=pmcollectors/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=pmcollectors,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=recoverygroups/finalizers,verbs=update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=recoverygroups/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=recoverygroups,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=regionaldrexports/finalizers,verbs=update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=regionaldrexports/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=regionaldrexports,verbs=create;get;list;patch;update;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=regionaldrs/finalizers,verbs=get;patch;update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=regionaldrs/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=regionaldrs,verbs=get;list;patch;update;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=regionaldrs,verbs=get;list;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=remoteclusters/finalizers,verbs=update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=remoteclusters/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=remoteclusters,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=restripefsjobs/finalizers,verbs=update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=restripefsjobs/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=restripefsjobs,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=*/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=stretchclusterinitnodes/finalizers,verbs=update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=stretchclusterinitnodes/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=stretchclusterinitnodes,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=stretchclusters/finalizers,verbs=update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=stretchclusters/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=stretchclusters,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=stretchclustertiebreaker/finalizers,verbs=update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=stretchclustertiebreaker/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=stretchclustertiebreaker,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=upgradeapprovals/status,verbs=get;patch;update
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=upgradeapprovals,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=scale.spectrum.ibm.com,resources=*,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,verbs=*
//+kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,verbs=list;watch;delete;update;get;create;patch
//+kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,verbs=use
//+kubebuilder:rbac:groups=storage.k8s.io,resources=csidrivers,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=storage.k8s.io,resources=csidrivers,verbs=get;list;watch
//+kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=storage.k8s.io,resources=volumeattachments,verbs=create;delete;get;list;patch;update;watch
// IBM_RBAC_MARKER_END

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the FusionAccess object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.3/pkg/reconcile
func (r *FusionAccessReconciler) Reconcile(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	// TODO(user): your logic here
	fusionaccess := &fusionv1alpha.FusionAccess{}
	err := r.Get(ctx, req.NamespacedName, fusionaccess)
	if err != nil {
		if kerrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// // Check if the FusionAccess instance is marked to be deleted, which is
	// // indicated by the deletion timestamp being set.
	// isFusionAccessMarkedToBeDeleted := fusionaccess.GetDeletionTimestamp() != nil
	// if isFusionAccessMarkedToBeDeleted {
	// 	if controllerutil.ContainsFinalizer(fusionaccess, storageScaleFinalizer) {
	// 		// Run finalization logic for storageScaleFinalizer. If the
	// 		// finalization logic fails, don't remove the finalizer so
	// 		// that we can retry during the next reconciliation.
	// 		if err := r.finalizeFusionAccess(log.Log, fusionaccess); err != nil {
	// 			return ctrl.Result{}, err
	// 		}

	// 		// Remove memcachedFinalizer. Once all finalizers have been
	// 		// removed, the object will be deleted.
	// 		controllerutil.RemoveFinalizer(fusionaccess, storageScaleFinalizer)
	// 		err := r.Update(ctx, fusionaccess)
	// 		if err != nil {
	// 			return ctrl.Result{}, err
	// 		}
	// 	}
	// 	return ctrl.Result{}, nil
	// }
	// // Add finalizer for this CR
	// if !controllerutil.ContainsFinalizer(fusionaccess, storageScaleFinalizer) {
	// 	controllerutil.AddFinalizer(fusionaccess, storageScaleFinalizer)
	// 	err = r.Update(ctx, fusionaccess)
	// 	if err != nil {
	// 		return ctrl.Result{}, err
	// 	}
	// }

	currentNamespace, err := utils.GetDeploymentNamespace()
	if err != nil {
		return ctrl.Result{}, err
	}

	// Check if can pull the image if we have not already
	if fusionaccess.Status.ExternalImagePullStatus == v1alpha1.CheckNotRun {
		testImage, err := utils.GetExternalTestImage(string(fusionaccess.Spec.IbmCnsaVersion))
		if err != nil {
			return ctrl.Result{}, err
		}
		ok, err := r.CanPullImage(ctx, r.fullClient, currentNamespace, testImage)
		if ok {
			fusionaccess.Status.ExternalImagePullStatus = v1alpha1.CheckSuccess
		} else {
			fusionaccess.Status.ExternalImagePullStatus = v1alpha1.CheckFailed
			fusionaccess.Status.ExternalImagePullError = err.Error()
		}
		err = r.Status().Update(ctx, fusionaccess)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	// Create machineconfig to enable kernel modules if needed
	if fusionaccess.Spec.MachineConfig.Create {
		mc := machineconfig.NewMachineConfig(fusionaccess.Spec.MachineConfig.Labels)
		err = machineconfig.CreateOrUpdateMachineConfig(ctx, mc, r.Client)
		if err != nil {
			return ctrl.Result{}, err
		}
		err = machineconfig.WaitForMachineConfigPoolUpdated(ctx, r.dynamicClient, "worker")
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	// Load and install manifests from ibm
	install_path, err := utils.GetInstallPath(string(fusionaccess.Spec.IbmCnsaVersion))
	if err != nil {
		return ctrl.Result{}, err
	}

	installManifest, err := manifestival.NewManifest(
		install_path,
		manifestival.UseClient(mfc.NewClient(r.Client)),
	)
	if err != nil {
		return ctrl.Result{}, err
	}
	log.Log.Info(fmt.Sprintf("Applying manifest from %s", install_path))

	if err := installManifest.Apply(); err != nil {
		log.Log.Error(err, "Error applying manifest")
		return reconcile.Result{}, err
	}
	log.Log.Info(fmt.Sprintf("Applied manifest from %s", install_path))

	secretstring := strings.TrimSpace(pull)
	// Create secrets in IBM namespaces to pull images from quay
	secretData := map[string][]byte{
		".dockerconfigjson": []byte(secretstring),
	}

	destSecretName := "ibm-entitlement-key" //nolint:gosec
	destNamespaces := []string{
		"ibm-spectrum-scale",
		"ibm-spectrum-scale-dns",
		"ibm-spectrum-scale-csi",
		"ibm-spectrum-scale-operator",
	}
	for _, destNamespace := range destNamespaces {
		ibmPullSecret := newSecret(
			destSecretName,
			destNamespace,
			secretData,
			"kubernetes.io/dockerconfigjson",
			nil,
		)
		_, err = r.fullClient.CoreV1().
			Secrets(destNamespace).
			Get(ctx, destSecretName, metav1.GetOptions{})
		if err != nil {
			if kerrors.IsNotFound(err) {
				// Resource does not exist, create it
				_, err := r.fullClient.CoreV1().
					Secrets(destNamespace).
					Create(context.TODO(), ibmPullSecret, metav1.CreateOptions{})
				if err != nil {
					return ctrl.Result{}, err
				}
				log.Log.Info(
					fmt.Sprintf("Created Secret %s in ns %s", destSecretName, destNamespace),
				)
				continue
			}
			return ctrl.Result{}, err
		}
		// The destination secret already exists so we upate it and return an error if they were different so the reconcile loop can restart
		_, err = r.fullClient.CoreV1().
			Secrets(destNamespace).
			Update(context.TODO(), ibmPullSecret, metav1.UpdateOptions{})
		if err == nil {
			log.Log.Info(fmt.Sprintf("Updated Secret %s in ns %s", destSecretName, destNamespace))
			continue
		}
	}
	if err := console.CreateOrUpdatePlugin(ctx, r.Client); err != nil {
		return ctrl.Result{}, err
	}
	log.Log.Info("Successfully created / updated console plugin resources")

	if err := console.EnablePlugin(ctx, r.Client); err != nil {
		return ctrl.Result{}, err
	}
	log.Log.Info("Successfully enabled console plugin")

	if fusionaccess.Spec.LocalVolumeDiscovery.Create {
		// Create Device discovery
		lvd := localvolumediscovery.NewLocalVolumeDiscovery(currentNamespace)
		if err := localvolumediscovery.CreateOrUpdateLocalVolumeDiscovery(ctx, lvd, r.Client); err != nil {
			return ctrl.Result{}, err
		}
	}
	if fusionaccess.Spec.Cluster.Create {
		// Create IBM storage cluster
		cluster := NewSpectrumCluster(fusionaccess.Spec.Cluster.Daemon_nodeSelector)
		gvr := schema.GroupVersionResource{
			Group:    "scale.spectrum.ibm.com",
			Version:  "v1beta1",
			Resource: "clusters",
		}
		log.Log.Info("Creating cluster")

		_, err = r.dynamicClient.Resource(gvr).Get(ctx, cluster.GetName(), metav1.GetOptions{})
		if err != nil {
			if kerrors.IsNotFound(err) {
				// Resource does not exist, create it
				err = r.Create(ctx, cluster)
				log.Log.Info("Created cluster")
			}
			return ctrl.Result{}, err
		}
		log.Log.Info("Cluster aleardy exists, considering immutable")
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FusionAccessReconciler) SetupWithManager(mgr ctrl.Manager) error {
	var err error
	r.config = mgr.GetConfig()
	if r.dynamicClient, err = dynamic.NewForConfig(r.config); err != nil {
		return err
	}
	if r.fullClient, err = kubernetes.NewForConfig(r.config); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&fusionv1alpha.FusionAccess{}).
		Complete(r)
}

// func (r *FusionAccessReconciler) finalizeFusionAccess(reqLogger logr.Logger, sc *v1alpha1.FusionAccess) error {
// 	// TODO(user): Add the cleanup steps that the operator
// 	// needs to do before the CR can be deleted. Examples
// 	// of finalizers include performing backups and deleting
// 	// resources that are not owned by this CR, like a PVC.
// 	reqLogger.Info("Successfully finalized FusionAccess")
// 	return nil
// }
