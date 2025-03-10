/*
Copyright 2021 The Karmada Authors.

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

package objectwatcher

import (
	"context"
	"fmt"
	"sync"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1alpha1 "github.com/karmada-io/karmada/pkg/apis/config/v1alpha1"
	workv1alpha2 "github.com/karmada-io/karmada/pkg/apis/work/v1alpha2"
	"github.com/karmada-io/karmada/pkg/resourceinterpreter"
	"github.com/karmada-io/karmada/pkg/util"
	"github.com/karmada-io/karmada/pkg/util/fedinformer/genericmanager"
	"github.com/karmada-io/karmada/pkg/util/fedinformer/keys"
	"github.com/karmada-io/karmada/pkg/util/helper"
	"github.com/karmada-io/karmada/pkg/util/lifted"
	"github.com/karmada-io/karmada/pkg/util/restmapper"
)

// OperationResult is the action result of an Update call.
type OperationResult string

const (
	// OperationResultNone means that the update operation was not performed and the resource has not been changed.
	OperationResultNone OperationResult = "none"
	// OperationResultUnchanged means that the update operation was performed but the resource did not change.
	OperationResultUnchanged OperationResult = "unchanged"
	// OperationResultUpdated means that an existing resource is updated.
	OperationResultUpdated OperationResult = "updated"
)

// ObjectWatcher manages operations for object dispatched to member clusters.
type ObjectWatcher interface {
	Create(ctx context.Context, clusterName string, desireObj *unstructured.Unstructured) error
	Update(ctx context.Context, clusterName string, desireObj, clusterObj *unstructured.Unstructured) (operationResult OperationResult, err error)
	Delete(ctx context.Context, clusterName string, desireObj *unstructured.Unstructured) error
	NeedsUpdate(clusterName string, desiredObj, clusterObj *unstructured.Unstructured) (bool, error)
}

// ClientSetFunc is used to generate client set of member cluster
type ClientSetFunc func(c string, client client.Client) (*util.DynamicClusterClient, error)

type objectWatcherImpl struct {
	Lock                 sync.RWMutex
	RESTMapper           meta.RESTMapper
	KubeClientSet        client.Client
	VersionRecord        map[string]map[string]string
	ClusterClientSetFunc ClientSetFunc
	resourceInterpreter  resourceinterpreter.ResourceInterpreter
	InformerManager      genericmanager.MultiClusterInformerManager
}

// NewObjectWatcher returns an instance of ObjectWatcher
func NewObjectWatcher(kubeClientSet client.Client, restMapper meta.RESTMapper, clusterClientSetFunc ClientSetFunc, interpreter resourceinterpreter.ResourceInterpreter) ObjectWatcher {
	return &objectWatcherImpl{
		KubeClientSet:        kubeClientSet,
		VersionRecord:        make(map[string]map[string]string),
		RESTMapper:           restMapper,
		ClusterClientSetFunc: clusterClientSetFunc,
		resourceInterpreter:  interpreter,
		InformerManager:      genericmanager.GetInstance(),
	}
}

func (o *objectWatcherImpl) Create(ctx context.Context, clusterName string, desireObj *unstructured.Unstructured) error {
	dynamicClusterClient, err := o.ClusterClientSetFunc(clusterName, o.KubeClientSet)
	if err != nil {
		klog.Errorf("Failed to build dynamic cluster client for cluster %s, err: %v.", clusterName, err)
		return err
	}

	gvr, err := restmapper.GetGroupVersionResource(o.RESTMapper, desireObj.GroupVersionKind())
	if err != nil {
		klog.Errorf("Failed to create resource(kind=%s, %s/%s) in cluster %s as mapping GVK to GVR failed: %v", desireObj.GetKind(), desireObj.GetNamespace(), desireObj.GetName(), clusterName, err)
		return err
	}

	fedKey, err := keys.FederatedKeyFunc(clusterName, desireObj)
	if err != nil {
		klog.Errorf("Failed to get FederatedKey %s, error: %v.", desireObj.GetName(), err)
		return err
	}
	_, err = helper.GetObjectFromCache(o.RESTMapper, o.InformerManager, fedKey)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("Failed to get resource(kind=%s, %s/%s) from member cluster, err is %v.", desireObj.GetKind(), desireObj.GetNamespace(), desireObj.GetName(), err)
			return err
		}

		clusterObj, err := dynamicClusterClient.DynamicClientSet.Resource(gvr).Namespace(desireObj.GetNamespace()).Create(ctx, desireObj, metav1.CreateOptions{})
		if err != nil {
			klog.Errorf("Failed to create resource(kind=%s, %s/%s) in cluster %s, err is %v.", desireObj.GetKind(), desireObj.GetNamespace(), desireObj.GetName(), clusterName, err)
			return err
		}

		klog.Infof("Created the resource(kind=%s, %s/%s) on cluster(%s).", desireObj.GetKind(), desireObj.GetNamespace(), desireObj.GetName(), clusterName)
		// record version
		o.recordVersion(clusterObj, dynamicClusterClient.ClusterName)
	}

	return nil
}

func (o *objectWatcherImpl) retainClusterFields(desired, observed *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	// Pass the same ResourceVersion as in the cluster object for update operation, otherwise operation will fail.
	desired.SetResourceVersion(observed.GetResourceVersion())

	// Retain finalizers since they will typically be set by
	// controllers in a member cluster.  It is still possible to set the fields
	// via overrides.
	desired.SetFinalizers(observed.GetFinalizers())

	// Retain ownerReferences since they will typically be set by controllers in a member cluster.
	desired.SetOwnerReferences(observed.GetOwnerReferences())

	// Retain annotations since they will typically be set by controllers in a member cluster
	// and be set by user in karmada-controller-plane.
	util.RetainAnnotations(desired, observed)

	// Retain labels since they will typically be set by controllers in a member cluster
	// and be set by user in karmada-controller-plane.
	util.RetainLabels(desired, observed)

	if o.resourceInterpreter.HookEnabled(desired.GroupVersionKind(), configv1alpha1.InterpreterOperationRetain) {
		return o.resourceInterpreter.Retain(desired, observed)
	}

	return desired, nil
}

func (o *objectWatcherImpl) Update(ctx context.Context, clusterName string, desireObj, clusterObj *unstructured.Unstructured) (OperationResult, error) {
	updateAllowed := o.allowUpdate(clusterName, desireObj, clusterObj)
	if !updateAllowed {
		// The existing resource is not managed by Karmada, and no conflict resolution found, avoid updating the existing resource by default.
		return OperationResultNone, fmt.Errorf("resource(kind=%s, %s/%s) already exists in the cluster %v and the %s strategy value is empty, Karmada will not manage this resource",
			desireObj.GetKind(), desireObj.GetNamespace(), desireObj.GetName(), clusterName, workv1alpha2.ResourceConflictResolutionAnnotation)
	}

	dynamicClusterClient, err := o.ClusterClientSetFunc(clusterName, o.KubeClientSet)
	if err != nil {
		klog.Errorf("Failed to build dynamic cluster client for cluster %s, err: %v.", clusterName, err)
		return OperationResultNone, err
	}

	gvr, err := restmapper.GetGroupVersionResource(o.RESTMapper, desireObj.GroupVersionKind())
	if err != nil {
		klog.Errorf("Failed to update the resource(kind=%s, %s/%s) in the cluster %s as mapping GVK to GVR failed: %v", desireObj.GetKind(), desireObj.GetNamespace(), desireObj.GetName(), clusterName, err)
		return OperationResultNone, err
	}

	desireObj, err = o.retainClusterFields(desireObj, clusterObj)
	if err != nil {
		klog.Errorf("Failed to retain fields for resource(kind=%s, %s/%s) in cluster %s: %v", clusterObj.GetKind(), clusterObj.GetNamespace(), clusterObj.GetName(), clusterName, err)
		return OperationResultNone, err
	}

	resource, err := dynamicClusterClient.DynamicClientSet.Resource(gvr).Namespace(desireObj.GetNamespace()).Update(ctx, desireObj, metav1.UpdateOptions{})
	if err != nil {
		klog.Errorf("Failed to update resource(kind=%s, %s/%s) in cluster %s, err: %v.", desireObj.GetKind(), desireObj.GetNamespace(), desireObj.GetName(), clusterName, err)
		return OperationResultNone, err
	}

	// record version
	o.recordVersion(resource, clusterName)

	if clusterObj.GetResourceVersion() == resource.GetResourceVersion() {
		klog.Infof("Updated the resource(kind=%s, %s/%s) on cluster(%s) but the cluster object was not changed.", desireObj.GetKind(), desireObj.GetNamespace(), desireObj.GetName(), clusterName)
		return OperationResultUnchanged, nil
	}

	klog.Infof("Updated the resource(kind=%s, %s/%s) on cluster(%s).", desireObj.GetKind(), desireObj.GetNamespace(), desireObj.GetName(), clusterName)
	return OperationResultUpdated, nil
}

func (o *objectWatcherImpl) Delete(ctx context.Context, clusterName string, desireObj *unstructured.Unstructured) error {
	fedKey, err := keys.FederatedKeyFunc(clusterName, desireObj)
	if err != nil {
		klog.Errorf("Failed to get FederatedKey %s, error: %v", desireObj.GetName(), err)
		return err
	}

	clusterObj, err := helper.GetObjectFromCache(o.RESTMapper, o.InformerManager, fedKey)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		klog.Errorf("Failed to get the resource(kind=%s, %s/%s) from the member cluster %s, err is %v", desireObj.GetKind(), desireObj.GetNamespace(), desireObj.GetName(), clusterName, err)
		return err
	}

	// Avoid deleting resources that are not managed by Karmada.
	if !o.isManagedResource(clusterObj) {
		klog.Infof("Abort deleting the resource(kind=%s, %s/%s) which exists in the member cluster %s but is not managed by Karmada.", clusterObj.GetKind(), clusterObj.GetNamespace(), clusterObj.GetName(), clusterName)
		return nil
	}

	dynamicClusterClient, err := o.ClusterClientSetFunc(clusterName, o.KubeClientSet)
	if err != nil {
		klog.Errorf("Failed to build dynamic cluster client for cluster %s, err: %v.", clusterName, err)
		return err
	}

	gvr, err := restmapper.GetGroupVersionResource(o.RESTMapper, desireObj.GroupVersionKind())
	if err != nil {
		klog.Errorf("Failed to delete the resource(kind=%s, %s/%s) in the cluster %s as mapping GVK to GVR failed: %v", desireObj.GetKind(), desireObj.GetNamespace(), desireObj.GetName(), clusterName, err)
		return err
	}

	// Set deletion strategy to background explicitly even though it's the default strategy for most of the resources.
	// The reason for this is to fix the exception case that Kubernetes does on Job(batch/v1).
	// In kubernetes, the Job's default deletion strategy is "Orphan", that will cause the "Pods" created by "Job"
	// still exist after "Job" has been deleted.
	// Refer to https://github.com/karmada-io/karmada/issues/969 for more details.
	deleteBackground := metav1.DeletePropagationBackground
	deleteOption := metav1.DeleteOptions{
		PropagationPolicy: &deleteBackground,
	}

	err = dynamicClusterClient.DynamicClientSet.Resource(gvr).Namespace(desireObj.GetNamespace()).Delete(ctx, desireObj.GetName(), deleteOption)
	if err != nil && !apierrors.IsNotFound(err) {
		klog.Errorf("Failed to delete the resource(kind=%s, %s/%s) in the cluster %s, err: %v.", desireObj.GetKind(), desireObj.GetNamespace(), desireObj.GetName(), clusterName, err)
		return err
	}
	klog.Infof("Deleted the resource(kind=%s, %s/%s) on cluster(%s).", desireObj.GetKind(), desireObj.GetNamespace(), desireObj.GetName(), clusterName)

	objectKey := o.genObjectKey(desireObj)
	o.deleteVersionRecord(dynamicClusterClient.ClusterName, objectKey)

	return nil
}

func (o *objectWatcherImpl) genObjectKey(obj *unstructured.Unstructured) string {
	return obj.GroupVersionKind().String() + "/" + obj.GetNamespace() + "/" + obj.GetName()
}

// recordVersion will add or update resource version records
func (o *objectWatcherImpl) recordVersion(clusterObj *unstructured.Unstructured, clusterName string) {
	objVersion := lifted.ObjectVersion(clusterObj)
	objectKey := o.genObjectKey(clusterObj)

	o.Lock.Lock()
	defer o.Lock.Unlock()
	if o.VersionRecord[clusterName] == nil {
		o.VersionRecord[clusterName] = make(map[string]string)
	}
	o.VersionRecord[clusterName][objectKey] = objVersion
}

// getVersionRecord will return the recorded version of given resource(if exist)
func (o *objectWatcherImpl) getVersionRecord(clusterName, resourceName string) (string, bool) {
	o.Lock.RLock()
	defer o.Lock.RUnlock()

	version, exist := o.VersionRecord[clusterName][resourceName]
	return version, exist
}

// deleteVersionRecord will delete the recorded version of given resource
func (o *objectWatcherImpl) deleteVersionRecord(clusterName, resourceName string) {
	o.Lock.Lock()
	defer o.Lock.Unlock()
	delete(o.VersionRecord[clusterName], resourceName)
}

func (o *objectWatcherImpl) NeedsUpdate(clusterName string, desiredObj, clusterObj *unstructured.Unstructured) (bool, error) {
	// get resource version
	version, exist := o.getVersionRecord(clusterName, desiredObj.GroupVersionKind().String()+"/"+desiredObj.GetNamespace()+"/"+desiredObj.GetName())
	if !exist {
		klog.Errorf("Failed to update the resource(kind=%s, %s/%s) in the cluster %s because the version record does not exist.", desiredObj.GetKind(), desiredObj.GetNamespace(), desiredObj.GetName(), clusterName)
		return false, fmt.Errorf("failed to update resource(kind=%s, %s/%s) in cluster %s for the version record does not exist", desiredObj.GetKind(), desiredObj.GetNamespace(), desiredObj.GetName(), clusterName)
	}

	return lifted.ObjectNeedsUpdate(desiredObj, clusterObj, version), nil
}

func (o *objectWatcherImpl) isManagedResource(clusterObj *unstructured.Unstructured) bool {
	return util.GetLabelValue(clusterObj.GetLabels(), util.ManagedByKarmadaLabel) == util.ManagedByKarmadaLabelValue
}

func (o *objectWatcherImpl) allowUpdate(clusterName string, desiredObj, clusterObj *unstructured.Unstructured) bool {
	// If the existing resource is managed by Karmada, then the updating is allowed.
	if o.isManagedResource(clusterObj) {
		return true
	}
	klog.Warningf("The existing resource(kind=%s, %s/%s) in the cluster(%s) is not managed by Karmada.",
		clusterObj.GetKind(), clusterObj.GetNamespace(), clusterObj.GetName(), clusterName)

	// This happens when promoting workload to the Karmada control plane.
	conflictResolution := util.GetAnnotationValue(desiredObj.GetAnnotations(), workv1alpha2.ResourceConflictResolutionAnnotation)
	if conflictResolution == workv1alpha2.ResourceConflictResolutionOverwrite {
		klog.Infof("Force overwriting of the resource(kind=%s, %s/%s) in the cluster(%s).",
			desiredObj.GetKind(), desiredObj.GetNamespace(), desiredObj.GetName(), clusterName)
		return true
	}

	return false
}
