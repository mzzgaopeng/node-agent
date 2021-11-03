package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type HLease struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              HLeaseSpec `json:"spec"`
}

type HLeaseSpec struct {
	//NodeStatus        bool        `json:"nodeStatus"`
	LastHeartbeatTime metav1.Time `json:"lastHeartbeatTime,omitempty"`
	//HeartbeatFlag     int         `json:"heartbeatFlag,omitempty"`
	Switch           string `json:"switch,omitempty"`
	Reason           string `json:"reason,omitempty"`
	Source           string `json:"source,omitempty"`
	ProcessingStatus string `json:"processingStatus,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type HLeaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []HLease `json:"items"`
}

