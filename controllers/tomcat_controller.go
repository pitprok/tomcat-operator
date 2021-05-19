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
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	appsv1 "k8s.io/api/apps/v1"
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
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete

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
	foundReplicas := int32(-1)

	// Fetch the Tomcat instance
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

	// Check if the Service already exists, if not create a new one
	ser := r.serviceForTomcat(tomcat)
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: ser.Name, Namespace: ser.Namespace}, &corev1.Service{})
	if err != nil && errors.IsNotFound(err) {
		// Define a new Service
		reqLogger.Info("Creating a new Service.", "Service.Namespace", ser.Namespace, "Service.Name", ser.Name)
		err = r.Client.Create(context.TODO(), ser)
		if err != nil {
			reqLogger.Error(err, "Failed to create new Service.", "Service.Namespace", ser.Namespace, "Service.Name", ser.Name)
			return ctrl.Result{}, err
		}
		// Service created successfully - return and requeue
		return ctrl.Result{Requeue: true}, nil
	} else if err != nil {
		reqLogger.Error(err, "Failed to get Service.")
		return ctrl.Result{}, err
	}

	// Check if a webapp needs to be built
	if tomcat.Spec.TomcatImage.TomcatWebApp != nil {
		if tomcat.Spec.TomcatImage.TomcatWebApp.WebAppURL != "" && tomcat.Spec.TomcatImage.TomcatWebApp.WebArchiveImage != "" {

			// Check if a Persistent Volume Claim already exists, if not create a new one
			pvc := r.persistentVolumeClaimForTomcat(tomcat)
			err = r.Client.Get(context.TODO(), types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace}, pvc)
			if err != nil && errors.IsNotFound(err) {
				reqLogger.Info("Creating a new PersistentVolumeClaim.", "PersistentVolumeClaim.Namespace", pvc.Namespace, "PersistentVolumeClaim.Name", pvc.Name)
				err = r.Client.Create(context.TODO(), pvc)
				if err != nil && !errors.IsAlreadyExists(err) {
					reqLogger.Error(err, "Failed to create a new PersistentVolumeClaim.", "PersistentVolumeClaim.Namespace", pvc.Namespace, "PersistentVolumeClaim.Name", pvc.Name)
					return ctrl.Result{}, err
				}
				// Persistent Volume Claim created successfully - return and requeue
				return ctrl.Result{Requeue: true}, nil
			} else if err != nil {
				reqLogger.Error(err, "Failed to get PersistentVolumeClaim.")
				return ctrl.Result{}, err
			}

			// Check if the build pod already exists, if not create a new one
			buildPod := r.buildPodForTomcat(tomcat)
			err = r.Client.Get(context.TODO(), types.NamespacedName{Name: buildPod.Name, Namespace: buildPod.Namespace}, buildPod)
			if err != nil && errors.IsNotFound(err) {
				reqLogger.Info("Creating a new Build Pod.", "BuildPod.Namespace", buildPod.Namespace, "BuildPod.Name", buildPod.Name)
				err = r.Client.Create(context.TODO(), buildPod)
				if err != nil && !errors.IsAlreadyExists(err) {
					reqLogger.Error(err, "Failed to create a new Build Pod.", "BuildPod.Namespace", buildPod.Namespace, "BuildPod.Name", buildPod.Name)
					return ctrl.Result{}, err
				}
				// Build pod created successfully - return and requeue
				return ctrl.Result{Requeue: true}, nil
			} else if err != nil {
				reqLogger.Error(err, "Failed to get the Build Pod.")
				return ctrl.Result{}, err
			}

			if buildPod.Status.Phase != corev1.PodSucceeded {
				switch buildPod.Status.Phase {
				case corev1.PodFailed:
					reqLogger.Info("Application build failed: " + buildPod.Status.Message)
				case corev1.PodPending:
					reqLogger.Info("Application build pending")
				case corev1.PodRunning:
					reqLogger.Info("Application is still being built")
				default:
					reqLogger.Info("Unknown build pod status")
				}
				return ctrl.Result{RequeueAfter: (5 * time.Second)}, nil
			}

		}
	}

	// Check if the Deployment already exists, if not create a new one
	foundDeployment := r.deploymentForTomcat(tomcat)
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: foundDeployment.Name, Namespace: foundDeployment.Namespace}, foundDeployment)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new Deployment.", "Deployment.Namespace", foundDeployment.Namespace, "Deployment.Name", foundDeployment.Name)
		err = r.Client.Create(context.TODO(), foundDeployment)
		if err != nil && !errors.IsAlreadyExists(err) {
			reqLogger.Error(err, "Failed to create a new Deployment.", "Deployment.Namespace", foundDeployment.Namespace, "Deployment.Name", foundDeployment.Name)
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
	reqLogger.Info("Reconciliation complete")
	return ctrl.Result{}, nil
}

func (r *TomcatReconciler) serviceForTomcat(t *apachev1alpha1.Tomcat) *corev1.Service {

	service := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Service",
		},
		ObjectMeta: objectMetaForTomcat(t, t.Spec.ApplicationName),
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name:       "http",
				Port:       8080,
				TargetPort: intstr.FromInt(8080),
			}},
			Selector: map[string]string{
				"Deployment":  t.Spec.ApplicationName,
				"Tomcat":      t.Name,
				"application": t.Spec.ApplicationName,
			},
		},
	}

	// Set Tomcat instance as the owner and controller
	controllerutil.SetControllerReference(t, service, r.Scheme)

	return service
}

func (r *TomcatReconciler) persistentVolumeClaimForTomcat(t *apachev1alpha1.Tomcat) *corev1.PersistentVolumeClaim {
	pvc := &corev1.PersistentVolumeClaim{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "k8s.io/api/apps/v1",
			Kind:       "PersistentVolumeClaimVolumeSource",
		},
		ObjectMeta: objectMetaForTomcat(t, t.Spec.ApplicationName),
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				"ReadWriteMany",
			},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					"storage": resource.MustParse("1Gi"),
				},
			},
		},
	}
	return pvc
}

func (r *TomcatReconciler) buildPodForTomcat(t *apachev1alpha1.Tomcat) *corev1.Pod {
	name := t.Spec.ApplicationName + "-build"
	objectMeta := objectMetaForTomcat(t, name)
	objectMeta.Labels["Tomcat"] = t.Name
	terminationGracePeriodSeconds := int64(60)
	pod := &corev1.Pod{
		ObjectMeta: objectMeta,
		Spec: corev1.PodSpec{
			TerminationGracePeriodSeconds: &terminationGracePeriodSeconds,
			RestartPolicy:                 "OnFailure",
			Volumes: []corev1.Volume{{
				Name: "app-volume",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: t.Spec.ApplicationName},
				},
			}},
			Containers: []corev1.Container{{
				Name:  "war",
				Image: t.Spec.TomcatImage.TomcatWebApp.WebArchiveImage,
				Command: []string{
					"/bin/sh",
					"-c",
				},
				Args: []string{
					generateWebAppBuildScript(t.Spec.TomcatImage.TomcatWebApp.WebAppURL, "/mnt/ROOT.war"),
				},
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      "app-volume",
						MountPath: "/mnt",
					},
				},
			}},
		},
	}
	// Set Tomcat instance as the owner and controller
	controllerutil.SetControllerReference(t, pod, r.Scheme)
	return pod
}

func generateWebAppBuildScript(webAppURL string, mountPath string) string {
	return fmt.Sprintf(`
	GITURL=%s
	ROOTWAR=%s
	if [ -z ${GITURL} ]; then
		echo "Need an URL like https://github.com/jfclere/demo-webapp.git"
		exit 1
	fi
	if [ -z ${ROOTWAR} ]; then
		# The /mnt is mounted by the first InitContainers of the operator,
		ROOTWAR=/mnt/ROOT.war
	fi
	git clone ${GITURL}
	if [ $? -ne 0 ]; then
		echo "Can't clone ${GITURL}"
		exit 1
	fi
	DIR=$(echo ${GITURL##*/})
	DIR=$(echo ${DIR%%.*})
	cd ${DIR}
	rm -r ~/.m2/repository
	mvn clean install
	if [ $? -ne 0 ]; then
		echo "mvn install failed please check the pom.xml in ${GITURL}"
		exit 1
	fi
	cp target/*.war $ROOTWAR
	ls -l ${ROOTWAR}`,
		webAppURL,
		mountPath,
	)
}

func (r *TomcatReconciler) deploymentForTomcat(t *apachev1alpha1.Tomcat) *appsv1.Deployment {

	replicas := int32(1)

	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "k8s.io/api/apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: objectMetaForTomcat(t, t.Spec.ApplicationName),
		Spec: appsv1.DeploymentSpec{
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType,
			},
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"Deployment":  t.Spec.ApplicationName,
					"Tomcat":      t.Name,
					"application": t.Spec.ApplicationName,
				},
			},
			Template: podTemplateSpecForTomcat(t, t.Spec.TomcatImage.ApplicationImage),
		},
	}

	// Set Tomcat instance as the owner and controller
	controllerutil.SetControllerReference(t, deployment, r.Scheme)
	return deployment
}

func podTemplateSpecForTomcat(t *apachev1alpha1.Tomcat, image string) corev1.PodTemplateSpec {
	objectMeta := objectMetaForTomcat(t, t.Spec.ApplicationName)
	objectMeta.Labels["Deployment"] = t.Spec.ApplicationName
	objectMeta.Labels["Tomcat"] = t.Name
	// TODO comment in when a webapp is added
	// 	health := t.Spec.TomcatImage.TomcatHealthCheck
	terminationGracePeriodSeconds := int64(60)
	return corev1.PodTemplateSpec{
		ObjectMeta: objectMeta,
		Spec: corev1.PodSpec{
			TerminationGracePeriodSeconds: &terminationGracePeriodSeconds,
			Volumes: []corev1.Volume{{
				Name: "app-volume",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: t.Spec.ApplicationName,
						ReadOnly:  true,
					},
				},
			}},
			Containers: []corev1.Container{{
				Name:            t.Spec.ApplicationName,
				Image:           image,
				ImagePullPolicy: "Always",
				Env: []corev1.EnvVar{
					{
						Name:  "KUBERNETES_NAMESPACE",
						Value: t.Namespace,
					},
				},
				//TODO comment in when a webapp is added
				// ReadinessProbe:  createReadinessProbe(t, health),
				// LivenessProbe:   createLivenessProbe(t, health),
				Ports: []corev1.ContainerPort{{
					Name:          "http",
					ContainerPort: 8080,
					Protocol:      corev1.ProtocolTCP,
				},
				},
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      "app-volume",
						MountPath: "/usr/local/tomcat/webapps/ROOT.war",
						SubPath:   "ROOT.war",
					},
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
