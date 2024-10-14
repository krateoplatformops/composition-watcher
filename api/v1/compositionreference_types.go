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

// +kubebuilder:object:generate=true
package v1

import (
	prv1 "github.com/krateoplatformops/provider-runtime/apis/common/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,categories={krateo}

type CompositionReference struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CompositionReferenceSpec   `json:"spec,omitempty"`
	Status CompositionReferenceStatus `json:"status,omitempty"`
}

type CompositionReferenceSpec struct {
	Filters   Filters   `json:"filters"`
	Reference Reference `json:"reference"`
}

type CompositionReferenceStatus struct {
	prv1.ConditionedStatus `json:",inline"`
}

//+kubebuilder:object:root=true

type CompositionReferenceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CompositionReference `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CompositionReference{}, &CompositionReferenceList{})
}

func (mg *CompositionReference) GetCondition(ct prv1.ConditionType) prv1.Condition {
	return mg.Status.GetCondition(ct)
}

func (mg *CompositionReference) SetConditions(c ...prv1.Condition) {
	mg.Status.SetConditions(c...)
}

type Filters struct {
	Exclude []Exclude `json:"exclude"`
}

type Exclude struct {
	ApiVersion string `json:"apiVersion"`
	// +optional
	Resource string `json:"resource"`
	// +optional
	Name string `json:"name"`
}

type Reference struct {
	ApiVersion string `json:"apiVersion"`
	Resource   string `json:"resource"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
}
