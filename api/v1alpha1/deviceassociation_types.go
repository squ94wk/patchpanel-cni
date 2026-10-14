package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec is immutable"
type DeviceAssociationSpec struct {
	Topology string `json:"topology"`
	Device   string `json:"device"`
	PodUID   string `json:"podUID"`
}

type DeviceAssociationStatus struct {
	// NodeName is empty as long as no node state exists for the association.
	// +optional
	NodeName string `json:"nodeName,omitempty"`
	// +optional
	SandboxID string `json:"sandboxID,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Topology",type=string,JSONPath=`.spec.topology`
// +kubebuilder:printcolumn:name="Device",type=string,JSONPath=`.spec.device`
// +kubebuilder:printcolumn:name="Node",type=string,JSONPath=`.status.nodeName`

type DeviceAssociation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DeviceAssociationSpec   `json:"spec"`
	Status DeviceAssociationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

type DeviceAssociationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DeviceAssociation `json:"items"`
}
