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

package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:validation:Enum=nginx;apisix
type IngressType string

const (
	Nginx  IngressType = "nginx"
	Apisix IngressType = "apisix"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// MinioSpec defines the desired state of Minio
type MinioSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	//+kubebuilder:validation:Required
	//+kubebuilder:validation:Maximum=32
	//+kubebuilder:default=1
	Replicas uint8 `json:"replicas"`
	//+kubebuilder:validation:Required
	//+kubebuilder:validation:Pattern:=`^[a-z0-9]([a-z0-9\.\-]*[a-z0-9])?$`
	ClusterVersionRef string `json:"clusterVersionRef"`
	//+kubebuilder:validation:Optional
	//+kubebuilder:default=nginx
	IngressType IngressType `json:"ingressType"`
	//+kubebuilder:validation:Optional
	//+kubebuilder:default=false
	ConsolePublic bool `json:"consolePublic"`
	//+kubebuilder:validation:Required
	//+kubebuilder:default=false
	S3Public bool `json:"s3Public"`
	//+kubebuilder:validation:Required
	//+kubebuilder:default=1
	PVCNum int64 `json:"pvcNum"`

	//+kubebuilder:validation:Required
	Resource corev1.ResourceRequirements `json:"resource"`
}

// MinioStatus defines the observed state of Minio
type MinioStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	AvailableReplicas   int32  `json:"availableReplicas"`
	CurrentVersionRef   string `json:"currentVersionRef"`
	PublicS3Domain      string `json:"publicS3Domain"`
	PublicConsoleDomain string `json:"publicConsoleDomain"`
	SecretName          string `json:"secretName"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Version",type=string,JSONPath=".spec.ClusterVersionRef"
//+kubebuilder:printcolumn:name="Available",type=string,JSONPath=".status.availableReplicas"
//+kubebuilder:printcolumn:name="Domain",type=string,JSONPath=".status.domain"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Minio is the Schema for the minios API
type Minio struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MinioSpec   `json:"spec,omitempty"`
	Status MinioStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// MinioList contains a list of Minio
type MinioList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Minio `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Minio{}, &MinioList{})
}
