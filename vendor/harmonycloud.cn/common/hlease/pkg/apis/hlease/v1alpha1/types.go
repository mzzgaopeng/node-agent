/*
Copyright 2017 The Kubernetes Authors.

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
	HeartbeatFlag     int         `json:"heartbeatFlag,omitempty"`
	Switch            string      `json:"switch,omitempty"`
	Reason            string      `json:"reason,omitempty"`
	Source            string      `json:"source,omitempty"`
	ProcessingStatus  string      `json:"processingStatus,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type HLeaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []HLease `json:"items"`
}