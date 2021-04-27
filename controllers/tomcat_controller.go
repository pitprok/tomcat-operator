/*
Copyright 2021.

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

package controllers

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	kbappsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apachev1alpha1 "github.com/pitprok/tomcat-operator/api/v1alpha1"
)

// TomcatReconciler reconciles a Tomcat object
type TomcatReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// SetupWithManager sets up the controller with the Manager.
func (r *TomcatReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apachev1alpha1.Tomcat{}).
		Complete(r)
}

//+kubebuilder:rbac:groups=apache.org,resources=tomcats,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apache.org,resources=tomcats/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apache.org,resources=tomcats/finalizers,verbs=update

//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Tomcat object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.2/pkg/reconcile
func (r *TomcatReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	reqLogger := r.Log.WithValues("tomcat", request.NamespacedName)
	reqLogger.Info("Starting reconciliation")
	updateDeployment := false
	foundReplicas := int32(-1) // we need the foundDeployment.Spec.Replicas which is &appsv1.DeploymentConfig{} or &kbappsv1.Deployment{}
	tomcat := &apachev1alpha1.Tomcat{}
	err := r.Client.Get(context.TODO(), request.NamespacedName, tomcat)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			reqLogger.Info("Tomcat resource not found. Ignoring since object must have been deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		reqLogger.Error(err, "Failed to get Tomcat resource")
		return ctrl.Result{}, err
	}

	// Check if the Deployment already exists, if not create a new one
	foundDeployment := &kbappsv1.Deployment{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: tomcat.Spec.ApplicationName, Namespace: tomcat.Namespace}, foundDeployment)
	if err != nil && errors.IsNotFound(err) {
		// Define a new Deployment
		dep := r.deploymentForTomcat(tomcat)
		reqLogger.Info("Creating a new Deployment.", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
		err = r.Client.Create(context.TODO(), dep)
		if err != nil && !errors.IsAlreadyExists(err) {
			reqLogger.Error(err, "Failed to create a new Deployment.", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
			return ctrl.Result{}, err
		}
		// Deployment created successfully - return and requeue
		return ctrl.Result{Requeue: true}, nil
	} else if err != nil {
		reqLogger.Error(err, "Failed to get Deployment.")
		return ctrl.Result{}, err
	}

	tomcatImage := tomcat.Spec.TomcatImage
	applicationImage := ""
	if tomcatImage != nil {
		applicationImage = tomcatImage.ApplicationImage
	}

	foundImage := foundDeployment.Spec.Template.Spec.Containers[0].Image
	if foundImage != applicationImage {
		reqLogger.Info("Tomcat application image change detected. Deployment update scheduled")
		foundDeployment.Spec.Template.Spec.Containers[0].Image = applicationImage
		updateDeployment = true
	}

	// Handle Scaling
	foundReplicas = *foundDeployment.Spec.Replicas
	replicas := tomcat.Spec.Replicas
	if foundReplicas != replicas {
		reqLogger.Info("Deployment replicas number does not match the Tomcat specification")
		foundDeployment.Spec.Replicas = &replicas
		updateDeployment = true
	}

	if updateDeployment {
		err = r.Client.Update(context.TODO(), foundDeployment)
		if err != nil {
			reqLogger.Error(err, "Failed to update Deployment.", "Deployment.Namespace", foundDeployment.Namespace, "Deployment.Name", foundDeployment.Name)
			return ctrl.Result{}, err
		}
		// Spec updated - return and requeue
		return ctrl.Result{Requeue: true}, nil
	}

	return ctrl.Result{}, nil
}

func (r *TomcatReconciler) deploymentForTomcat(t *apachev1alpha1.Tomcat) *kbappsv1.Deployment {

	replicas := int32(1)
	podTemplateSpec := podTemplateSpecForTomcat(t, t.Spec.TomcatImage.ApplicationImage)
	deployment := &kbappsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "k8s.io/api/apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: objectMetaForTomcat(t, t.Spec.ApplicationName),
		Spec: kbappsv1.DeploymentSpec{
			Strategy: kbappsv1.DeploymentStrategy{
				Type: kbappsv1.RecreateDeploymentStrategyType,
			},
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"deploymentConfig": t.Spec.ApplicationName,
					"Tomcat":           t.Name,
				},
			},
			Template: podTemplateSpec,
		},
	}

	controllerutil.SetControllerReference(t, deployment, r.Scheme)
	return deployment
}

func podTemplateSpecForTomcat(t *apachev1alpha1.Tomcat, image string) corev1.PodTemplateSpec {
	objectMeta := objectMetaForTomcat(t, t.Spec.ApplicationName)
	objectMeta.Labels["deploymentConfig"] = t.Spec.ApplicationName
	objectMeta.Labels["Tomcat"] = t.Name
	// TODO comment in when a webapp is added
	// 	health := t.Spec.TomcatImage.TomcatHealthCheck
	terminationGracePeriodSeconds := int64(60)
	return corev1.PodTemplateSpec{
		ObjectMeta: objectMeta,
		Spec: corev1.PodSpec{
			TerminationGracePeriodSeconds: &terminationGracePeriodSeconds,
			Containers: []corev1.Container{{
				Name:            t.Spec.ApplicationName,
				Image:           image,
				ImagePullPolicy: "Always",
				//TODO comment in when a webapp is added
				// ReadinessProbe:  createReadinessProbe(t, health),
				// LivenessProbe:   createLivenessProbe(t, health),
				Ports: []corev1.ContainerPort{{
					Name:          "http",
					ContainerPort: 8080,
					Protocol:      corev1.ProtocolTCP,
				},
				// TODO check
				//{
				// Name:          "jolokia",
				// ContainerPort: 8778,
				// Protocol:      corev1.ProtocolTCP,
				// },
				},
			}},
		},
	}
}

func objectMetaForTomcat(t *apachev1alpha1.Tomcat, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      name,
		Namespace: t.Namespace,
		Labels: map[string]string{
			"application": t.Spec.ApplicationName,
		},
	}
}
