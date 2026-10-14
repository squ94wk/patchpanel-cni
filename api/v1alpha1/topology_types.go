package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type TopologySpec struct {
	// +optional
	Devices []Device `json:"devices,omitempty"`
	// +optional
	Links []Link `json:"links,omitempty"`
}

type Device struct {
	Name string `json:"name"`
	// +optional
	Group string `json:"group,omitempty"`
}

// Link is a cable between two device interfaces.
type Link struct {
	A LinkEnd `json:"a"`
	B LinkEnd `json:"b"`
}

type LinkEnd struct {
	Device string `json:"device"`
	// +kubebuilder:validation:MaxLength=15
	Interface string `json:"interface"`
}

// +kubebuilder:object:root=true

type Topology struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec TopologySpec `json:"spec"`
}

// +kubebuilder:object:root=true

type TopologyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Topology `json:"items"`
}
