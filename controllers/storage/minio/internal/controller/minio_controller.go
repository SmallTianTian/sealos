/*
Copyright 2023.

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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/go-logr/logr"
	"github.com/jaevor/go-nanoid"
	miniov1 "github.com/labring/sealos/controllers/storage/minio/api/v1"
)

const (
	FinalizerName       = "minio.storage.sealos.io/finalizer"
	userKeyLen          = 20
	rootUserKey         = "MINIO_ROOT_USER"
	passwordKeySegments = 5
	eachPasswordKeyLen  = 6
	passwordSplitKey    = "-"
	rootPasswordKey     = "MINIO_ROOT_PASSWORD"
	LetterBytes         = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

// MinioReconciler reconciles a Minio object
type MinioReconciler struct {
	client.Client
	recorder record.EventRecorder
	Scheme   *runtime.Scheme
}

//+kubebuilder:rbac:groups=minio.storage.sealos.io.sealos.io,resources=minios,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=minio.storage.sealos.io.sealos.io,resources=minios/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=minio.storage.sealos.io.sealos.io,resources=minios/finalizers,verbs=update

func (r *MinioReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	logger := log.FromContext(ctx, "minio", req.NamespacedName)
	minio := &miniov1.Minio{}
	if err := r.Get(ctx, req.NamespacedName, minio); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if minio.ObjectMeta.DeletionTimestamp.IsZero() {
		if controllerutil.AddFinalizer(minio, FinalizerName) {
			if err := r.Update(ctx, minio); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		logger.V(5).Info("minio will remove")
		return r.handleRemoveMinio(ctx, logger, minio)
	}

	if err := r.syncSecret(ctx, minio); err != nil {
		logger.Error(err, "create secret failed")
		r.recorder.Eventf(minio, corev1.EventTypeWarning, "Create secret failed", "%v", err)
		return ctrl.Result{}, err
	}
	logger.V(5).Info("create secret succeeded")

	var hostname string
	if err := r.syncStatefulSet(ctx, minio, &hostname); err != nil {
		logger.Error(err, "create statefulset failed")
		r.recorder.Eventf(minio, corev1.EventTypeWarning, "Create statefulset failed", "%v", err)
		return ctrl.Result{}, err
	}

	if err := r.syncService(ctx, minio); err != nil {
		logger.Error(err, "create service failed")
		r.recorder.Eventf(minio, corev1.EventTypeWarning, "Create service failed", "%v", err)
		return ctrl.Result{}, err
	}

	if err := r.syncIngress(ctx, minio, hostname); err != nil {
		logger.Error(err, "create ingress failed")
		r.recorder.Eventf(minio, corev1.EventTypeWarning, "Create ingress failed", "%v", err)
		return ctrl.Result{}, err
	}

	r.recorder.Eventf(minio, corev1.EventTypeNormal, "Created", "create minio success: %s/%s", minio.Namespace, minio.Name)
	return ctrl.Result{}, nil
}

func (r *MinioReconciler) handleRemoveMinio(ctx context.Context, logger logr.Logger, minio *miniov1.Minio) (ctrl.Result, error) {
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: minio.Namespace, Name: minio.Name}}
	if err := r.Delete(ctx, secret); err != nil && !apierrors.IsNotFound(err) {
		logger.Error(err, "delete secret")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *MinioReconciler) syncSecret(ctx context.Context, minio *miniov1.Minio) error {
	existSecret := corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: minio.Namespace, Name: minio.Name}, &existSecret); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	// exist, not create again
	if existSecret.Data != nil {
		return nil
	}

	userKeyF, err := nanoid.CustomASCII(LetterBytes, userKeyLen)
	if err != nil {
		return err
	}
	passwordKeyF, err := nanoid.CustomASCII(LetterBytes, eachPasswordKeyLen)
	if err != nil {
		return err
	}

	passwordKeyArray := make([]string, 0, passwordKeySegments)
	for i := 0; i < passwordKeySegments; i++ {
		passwordKeyArray = append(passwordKeyArray, passwordKeyF())
	}

	notAllowdChangeData := true
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: minio.Namespace,
			Name:      minio.Name,
		},
		Immutable: &notAllowdChangeData,
		StringData: map[string]string{
			rootUserKey:     userKeyF(),
			rootPasswordKey: strings.Join(passwordKeyArray, passwordSplitKey),
		},
		Type: corev1.SecretTypeOpaque,
	}

	if err := client.IgnoreAlreadyExists(r.Create(ctx, secret)); err != nil {
		return err
	}

	minio.Status.SecretName = secret.Name
	return r.Status().Update(ctx, minio)
}

func (r *MinioReconciler) syncStatefulSet(ctx context.Context, minio *miniov1.Minio, hostName *string) error {
	labelsMap := buildLabelsMap(minio)
	var (
		objectMeta           metav1.ObjectMeta
		selector             *metav1.LabelSelector
		templateObjMeta      metav1.ObjectMeta
		ports                []corev1.ContainerPort
		envFrom              []corev1.EnvFromSource
		containers           []corev1.Container
		volumeMounts         []corev1.VolumeMount
		volumeClaimTemplates []corev1.PersistentVolumeClaim
	)

	objectMeta = metav1.ObjectMeta{
		Name:      minio.Name,
		Namespace: minio.Namespace,
	}
	selector = &metav1.LabelSelector{
		MatchLabels: labelsMap,
	}
	templateObjMeta = metav1.ObjectMeta{
		Labels: labelsMap,
	}
	ports = []corev1.ContainerPort{
		{
			Name:          "s3",
			Protocol:      corev1.ProtocolTCP,
			ContainerPort: 9000,
		},
		{
			Name:          "console",
			Protocol:      corev1.ProtocolTCP,
			ContainerPort: 9001,
		},
	}
	envFrom = []corev1.EnvFromSource{
		{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: minio.Name}}},
	}
	for i := int64(0); i < minio.Spec.PVCNum; i++ {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      fmt.Sprintf("disk-%d", i),
			MountPath: fmt.Sprintf("/data%d", i),
		})

		volumeClaimTemplates = append(volumeClaimTemplates, corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "disk"},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.ResourceRequirements{
					Requests: minio.Spec.Resource.Requests,
				},
			},
		})
	}

	containers = []corev1.Container{
		{
			Name:    "minio",
			Image:   minio.Spec.ClusterVersionRef,
			Ports:   ports,
			Command: []string{"server"},
			Args:    []string{fmt.Sprintf("http://minio-{0...%d}.minio.default.svc.cluster.local/data{0...%d}", minio.Spec.Replicas, minio.Spec.PVCNum)},
			EnvFrom: envFrom,
			Resources: corev1.ResourceRequirements{
				Requests: minio.Spec.Resource.Requests,
				Limits:   minio.Spec.Resource.Limits,
			},
			VolumeMounts: volumeMounts,
		},
	}

	replicas := int32(minio.Spec.Replicas)
	expectStatefulSet := &appsv1.StatefulSet{
		ObjectMeta: objectMeta,
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: selector,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: templateObjMeta,
				Spec: corev1.PodSpec{
					Containers: containers,
				},
			},
			VolumeClaimTemplates: volumeClaimTemplates,
			PersistentVolumeClaimRetentionPolicy: &appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy{
				WhenDeleted: appsv1.DeletePersistentVolumeClaimRetentionPolicyType,
				WhenScaled:  appsv1.DeletePersistentVolumeClaimRetentionPolicyType,
			},
		},
	}

	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: objectMeta,
	}

	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, statefulSet, func() error {
		// only update some specific fields
		statefulSet.Spec.Replicas = expectStatefulSet.Spec.Replicas
		statefulSet.Spec.Selector = expectStatefulSet.Spec.Selector
		statefulSet.Spec.Template.ObjectMeta.Labels = expectStatefulSet.Spec.Template.Labels
		statefulSet.Spec.VolumeClaimTemplates = expectStatefulSet.Spec.VolumeClaimTemplates
		statefulSet.Spec.PersistentVolumeClaimRetentionPolicy = expectStatefulSet.Spec.PersistentVolumeClaimRetentionPolicy
		if len(statefulSet.Spec.Template.Spec.Containers) == 0 {
			statefulSet.Spec.Template.Spec.Containers = containers
		} else {
			statefulSet.Spec.Template.Spec.Containers[0].Name = containers[0].Name
			statefulSet.Spec.Template.Spec.Containers[0].Image = containers[0].Image
			statefulSet.Spec.Template.Spec.Containers[0].Ports = containers[0].Ports
			statefulSet.Spec.Template.Spec.Containers[0].Command = containers[0].Command
			statefulSet.Spec.Template.Spec.Containers[0].Args = containers[0].Args
			statefulSet.Spec.Template.Spec.Containers[0].EnvFrom = containers[0].EnvFrom
			statefulSet.Spec.Template.Spec.Containers[0].Resources = containers[0].Resources
		}

		return controllerutil.SetControllerReference(minio, statefulSet, r.Scheme)
	}); err != nil {
		return err
	}

	minio.Status.AvailableReplicas = minio.Spec.Replicas
	minio.Status.CurrentVersionRef = minio.Spec.ClusterVersionRef
	return r.Status().Update(ctx, minio)
}

// TODO
func (r *MinioReconciler) syncService(ctx context.Context, minio *miniov1.Minio) error {
	return nil
}

// TODO
func (r *MinioReconciler) syncIngress(ctx context.Context, minio *miniov1.Minio, hostName string) error {
	return nil
}

func buildLabelsMap(minio *miniov1.Minio) map[string]string {
	labelsMap := map[string]string{
		"minio-app": minio.Name,
	}
	return labelsMap
}

// SetupWithManager sets up the controller with the Manager.
func (r *MinioReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("sealos-storage-minio-controller")
	return ctrl.NewControllerManagedBy(mgr).
		For(&miniov1.Minio{}).
		Complete(r)
}
