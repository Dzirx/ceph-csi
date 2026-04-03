/*
Copyright 2023 The Ceph-CSI Authors.

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

package kubernetes

import (
	corev1 "k8s.io/api/core/v1"
)

type ClusterInfo struct {
	// ClusterID is used for unique identification
	ClusterID string `json:"clusterID"`
	// TopologyDomainLabels maps Kubernetes topology labels to values,
	// enabling topology-aware cluster selection. When set, the CSI driver
	// can select this cluster based on the node's topology zone.
	// Example: {"topology.kubernetes.io/zone": "zone-a"}
	TopologyDomainLabels map[string]string `json:"topologyDomainLabels,omitempty"`
	// Monitors is monitor list for corresponding cluster ID
	Monitors []string `json:"monitors"`
	// CephFS contains CephFS specific options
	CephFS CephFS `json:"cephFS"`
	// RBD Contains RBD specific options
	RBD RBD `json:"rbd"`
	// NFS contains NFS specific options
	NFS NFS `json:"nfs"`
	// Read affinity map options
	ReadAffinity ReadAffinity `json:"readAffinity"`
}

type CephFS struct {
	// symlink filepath for the network namespace where we need to execute commands.
	NetNamespaceFilePath string `json:"netNamespaceFilePath"`
	// SubvolumeGroup contains the name of the SubvolumeGroup for CSI volumes
	SubvolumeGroup string `json:"subvolumeGroup"`
	// RadosNamespace is a rados namespace in the filesystem metadata pool
	RadosNamespace string `json:"radosNamespace"`
	// KernelMountOptions contains the kernel mount options for CephFS volumes
	KernelMountOptions string `json:"kernelMountOptions"`
	// FuseMountOptions contains the fuse mount options for CephFS volumes
	FuseMountOptions string `json:"fuseMountOptions"`
	// ControllerPublishSecretRef contains the secret reference for controller
	// publish operations.
	ControllerPublishSecretRef corev1.SecretReference `json:"controllerPublishSecretRef"`
	// FsName is the name of the CephFS filesystem to use for volumes provisioned
	// in this cluster. When set, overrides the fsName StorageClass parameter.
	FsName string `json:"fsName,omitempty"`
	// Pool is the optional CephFS pool used for subvolume layout.
	// When set, overrides the pool StorageClass parameter.
	Pool string `json:"pool,omitempty"`
	// ProvisionerSecretRef holds the per-cluster provisioner secret resolved
	// from the v1 clusterIDs SC format entry.
	ProvisionerSecretRef corev1.SecretReference `json:"provisionerSecretRef,omitempty"`
	// NodeStageSecretRef holds the per-cluster node-stage secret resolved
	// from the v1 clusterIDs SC format entry.
	NodeStageSecretRef corev1.SecretReference `json:"nodeStageSecretRef,omitempty"`
	// ControllerExpandSecretRef holds the per-cluster controller-expand secret
	// resolved from the v1 clusterIDs SC format entry.
	ControllerExpandSecretRef corev1.SecretReference `json:"controllerExpandSecretRef,omitempty"`
}
type RBD struct {
	// symlink filepath for the network namespace where we need to execute commands.
	NetNamespaceFilePath string `json:"netNamespaceFilePath"`
	// RadosNamespace is a rados namespace in the pool
	RadosNamespace string `json:"radosNamespace"`
	// RBD mirror daemons running in the ceph cluster.
	MirrorDaemonCount int `json:"mirrorDaemonCount"`
	// ControllerPublishSecretRef contains the secret reference for controller
	// publish operations.
	ControllerPublishSecretRef corev1.SecretReference `json:"controllerPublishSecretRef"`
}

type NFS struct {
	// symlink filepath for the network namespace where we need to execute commands.
	NetNamespaceFilePath string `json:"netNamespaceFilePath"`
}

type ReadAffinity struct {
	Enabled             bool     `json:"enabled"`
	CrushLocationLabels []string `json:"crushLocationLabels"`
}

// SCClusterEntry represents a single cluster entry in the v1 clusterIDs
// StorageClass parameter YAML/JSON format. Each entry embeds per-cluster
// secrets, filesystem options, and supported topology zones, so that the
// ConfigMap needs to contain only monitors.
type SCClusterEntry struct {
	ClusterID                       string              `json:"clusterID"`
	ProvisionerSecretName           string              `json:"csi.storage.k8s.io/provisioner-secret-name,omitempty"`
	ProvisionerSecretNamespace      string              `json:"csi.storage.k8s.io/provisioner-secret-namespace,omitempty"`
	NodeStageSecretName             string              `json:"csi.storage.k8s.io/node-stage-secret-name,omitempty"`
	NodeStageSecretNamespace        string              `json:"csi.storage.k8s.io/node-stage-secret-namespace,omitempty"`
	ControllerExpandSecretName      string              `json:"csi.storage.k8s.io/controller-expand-secret-name,omitempty"`
	ControllerExpandSecretNamespace string              `json:"csi.storage.k8s.io/controller-expand-secret-namespace,omitempty"`
	FsName                          string              `json:"fsName,omitempty"`
	Pool                            string              `json:"pool,omitempty"`
	TopologyDomainLabels            []map[string]string `json:"topologyDomainLabels,omitempty"`
}
