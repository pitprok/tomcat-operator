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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// PodStateActive represents PodStatus.State when pod is active to serve requests
	// it's connected in the Service load balancer
	PodStateActive = "ACTIVE"
	// PodStatePending represents PodStatus.State when pod is pending
	PodStatePending = "PENDING"
	// PodStateFailed represents PodStatus.State when pod has failed
	PodStateFailed = "FAILED"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// TomcatSpec defines the desired state of Tomcat
type TomcatSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// The base for the names of the deployed application resources
	// +kubebuilder:validation:Pattern=^[a-z]([-a-z0-9]*[a-z0-9])?$
	ApplicationName string `json:"applicationName"`
	// The desired number of replicas for the application
	// +kubebuilder:validation:Minimum=0
	Replicas int32 `json:"replicas"`
	// (Deployment method 1) Application image
	Image *ImageSpec `json:"image,omitempty"`
}

// ImageSpec contains the Tomcat application image information
type ImageSpec struct {
	// The name of the application image to be deployed
	ApplicationImage string `json:"applicationImage"`

	// The source code for a webapp to be built and deployed
	WebApp *WebAppSpec `json:"webApp,omitempty"`

	// Pod health checks information
	HealthCheck *HealthCheckSpec `json:"healthCheck,omitempty"`
}

// WebAppSpec contains all the information required to build a web application before deploying the server
type WebAppSpec struct {
	// Name of the web application (default: ROOT)
	Name string `json:"name,omitempty"`
	// URL for the repository of the application sources
	SourceRepositoryURL string `json:"sourceRepositoryURL"`
	// Branch in the source repository
	SourceRepositoryRef string `json:"sourceRepositoryRef,omitempty"`
	// Subdirectory in the source repository
	SourceRepositoryContextDir string `json:"contextDir,omitempty"`
	// The path on which the application war will be mounted (default:/usr/local/tomcat/webapps/)
	DeployPath string `json:"deployPath,omitempty"`
	// The size that the PersistentVolumeClaim needs to be in order to contain the application war (default 1Gi)
	ApplicationSize string `json:"applicationSize,omitempty"`
	// Image of the container where the web application will be built
	BuilderImage string `json:"builderImage"`
	// The script that the BuilderImage will use to build the application war and move it to /mnt
	BuildScript string `json:"buildScript,omitempty"`
}

// HealthCheckSpec has the liveness and readiness scripts for the server
type HealthCheckSpec struct {
	// String for the pod readiness health check logic
	ServerReadinessScript string `json:"serverReadinessScript"`
	// String for the pod liveness health check logic
	ServerLivenessScript string `json:"serverLivenessScript,omitempty"`
}

// TomcatStatus defines the observed state of Tomcat
//+kubebuilder:subresource:status
type TomcatStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// Replicas is the actual number of replicas for the application
	Replicas int32 `json:"replicas"`
	// +listType=atomic
	Pods []PodStatus `json:"pods,omitempty"`
	// +listType=set
	Hosts []string `json:"hosts,omitempty"`
	// Represents the number of pods which are in scaledown process
	// what particular pod is scaling down can be verified by PodStatus
	//
	// Read-only.
	ScalingdownPods int32 `json:"scalingdownPods"`
}

// PodStatus defines the observed state of pods running the Tomcat application
// +k8s:openapi-gen=true
type PodStatus struct {
	Name  string `json:"name"`
	PodIP string `json:"podIP"`
	// Represent the state of the Pod, it is used especially during scale down.
	// +kubebuilder:validation:Enum=ACTIVE;PENDING;FAILED
	State string `json:"state"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Tomcat is the Schema for the tomcats API
type Tomcat struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TomcatSpec   `json:"spec,omitempty"`
	Status TomcatStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// TomcatList contains a list of Tomcat
type TomcatList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Tomcat `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Tomcat{}, &TomcatList{})
}
