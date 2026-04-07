/*
Copyright 2019 The Ceph-CSI Authors.

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

package util

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/ceph/ceph-csi/api/deploy/kubernetes"
	"github.com/container-storage-interface/spec/lib/go/csi"
	corev1 "k8s.io/api/core/v1"
	sigyaml "sigs.k8s.io/yaml"
)

const (
	// defaultCsiSubvolumeGroup defines the default name for the CephFS CSI subvolumegroup.
	// This was hardcoded once and defaults to the old value to keep backward compatibility.
	defaultCsiSubvolumeGroup = "csi"

	// defaultCsiCephFSRadosNamespace defines the default RADOS namespace used for storing
	// CSI-specific objects and keys for CephFS volumes.
	defaultCsiCephFSRadosNamespace = "csi"

	// CsiConfigFile is the location of the CSI config file.
	CsiConfigFile = "/etc/ceph-csi-config/config.json"

	// ClusterIDKey is the name of the key containing clusterID.
	ClusterIDKey = "clusterID"

	// ClusterIDsKey is the name of the key containing a comma-separated list
	// of clusterIDs for topology-aware cluster selection.
	ClusterIDsKey = "clusterIDs"
)

// Expected JSON structure in the passed in config file is,
//nolint:godot // example json content should not contain unwanted dot.
/*
[{
	"clusterID": "<cluster-id>",
	"rbd": {
		"radosNamespace": "<rados-namespace>"
		"mirrorDaemonCount": 1
	},
	"monitors": [
		"<monitor-value>",
		"<monitor-value>"
	],
	"cephFS": {
		"subvolumeGroup": "<subvolumegroup for cephfs volumes>"
	}
}]
*/
func readClusterInfo(pathToConfig, clusterID string) (*kubernetes.ClusterInfo, error) {
	var config []kubernetes.ClusterInfo

	// #nosec
	content, err := os.ReadFile(pathToConfig)
	if err != nil {
		err = fmt.Errorf("error fetching configuration for cluster ID %q: %w", clusterID, err)

		return nil, err
	}

	err = json.Unmarshal(content, &config)
	if err != nil {
		return nil, fmt.Errorf("unmarshal failed (%w), raw buffer response: %s",
			err, string(content))
	}

	for i := range config {
		if config[i].ClusterID == clusterID {
			return &config[i], nil
		}
	}

	return nil, fmt.Errorf("%w: %q", ErrConfigNotFound, clusterID)
}

// Mons returns a comma separated MON list from the csi config for the given clusterID.
func Mons(pathToConfig, clusterID string) (string, error) {
	cluster, err := readClusterInfo(pathToConfig, clusterID)
	if err != nil {
		return "", err
	}

	if len(cluster.Monitors) == 0 {
		return "", fmt.Errorf("empty monitor list for cluster ID (%s) in config", clusterID)
	}

	return strings.Join(cluster.Monitors, ","), nil
}

// GetClusterTopologyDomainLabels returns a copy of topologyDomainLabels for the given clusterID.
func GetClusterTopologyDomainLabels(pathToConfig, clusterID string) (map[string]string, error) {
	cluster, err := readClusterInfo(pathToConfig, clusterID)
	if err != nil {
		return nil, err
	}

	if len(cluster.TopologyDomainLabels) == 0 {
		return nil, nil
	}

	topology := make(map[string]string, len(cluster.TopologyDomainLabels))
	for label, value := range cluster.TopologyDomainLabels {
		topology[label] = value
	}

	return topology, nil
}

// GetRBDRadosNamespace returns the namespace for the given clusterID.
func GetRBDRadosNamespace(pathToConfig, clusterID string) (string, error) {
	cluster, err := readClusterInfo(pathToConfig, clusterID)
	if err != nil {
		return "", err
	}

	return cluster.RBD.RadosNamespace, nil
}

// GetCephFSRadosNamespace returns the namespace for the given clusterID.
// If not set, it returns the default value "csi".
func GetCephFSRadosNamespace(pathToConfig, clusterID string) (string, error) {
	cluster, err := readClusterInfo(pathToConfig, clusterID)
	if err != nil {
		return "", err
	}

	if cluster.CephFS.RadosNamespace == "" {
		return defaultCsiCephFSRadosNamespace, nil
	}

	return cluster.CephFS.RadosNamespace, nil
}

// GetRBDMirrorDaemonCount returns the number of mirror daemon count for the
// given clusterID.
func GetRBDMirrorDaemonCount(pathToConfig, clusterID string) (int, error) {
	cluster, err := readClusterInfo(pathToConfig, clusterID)
	if err != nil {
		return 0, err
	}

	// if it is empty, set the default to 1 which is most common in a cluster.
	if cluster.RBD.MirrorDaemonCount == 0 {
		return 1, nil
	}

	return cluster.RBD.MirrorDaemonCount, nil
}

// CephFSSubvolumeGroup returns the subvolumeGroup for CephFS volumes. If not set, it returns the default value "csi".
func CephFSSubvolumeGroup(pathToConfig, clusterID string) (string, error) {
	cluster, err := readClusterInfo(pathToConfig, clusterID)
	if err != nil {
		return "", err
	}

	if cluster.CephFS.SubvolumeGroup == "" {
		return defaultCsiSubvolumeGroup, nil
	}

	return cluster.CephFS.SubvolumeGroup, nil
}

// GetMonsAndClusterID returns monitors and clusterID information read from
// configfile.
func GetMonsAndClusterID(ctx context.Context, clusterID string, checkClusterIDMapping bool) (string, string, error) {
	if checkClusterIDMapping {
		monitors, mappedClusterID, err := FetchMappedClusterIDAndMons(ctx, clusterID)
		if err != nil {
			return "", "", err
		}

		return monitors, mappedClusterID, nil
	}

	monitors, err := Mons(CsiConfigFile, clusterID)
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch monitor list using clusterID (%s): %w", clusterID, err)
	}

	return monitors, clusterID, nil
}

// GetClusterID fetches clusterID from given options map.
func GetClusterID(options map[string]string) (string, error) {
	clusterID, ok := options[ClusterIDKey]
	if !ok {
		return "", ErrClusterIDNotSet
	}

	return clusterID, nil
}

func GetRBDNetNamespaceFilePath(pathToConfig, clusterID string) (string, error) {
	cluster, err := readClusterInfo(pathToConfig, clusterID)
	if err != nil {
		return "", err
	}

	return cluster.RBD.NetNamespaceFilePath, nil
}

// GetCephFSNetNamespaceFilePath returns the netNamespaceFilePath for CephFS volumes.
func GetCephFSNetNamespaceFilePath(pathToConfig, clusterID string) (string, error) {
	cluster, err := readClusterInfo(pathToConfig, clusterID)
	if err != nil {
		return "", err
	}

	return cluster.CephFS.NetNamespaceFilePath, nil
}

// GetNFSNetNamespaceFilePath returns the netNamespaceFilePath for NFS volumes.
func GetNFSNetNamespaceFilePath(pathToConfig, clusterID string) (string, error) {
	cluster, err := readClusterInfo(pathToConfig, clusterID)
	if err != nil {
		return "", err
	}

	return cluster.NFS.NetNamespaceFilePath, nil
}

// GetCrushLocationLabels returns the `readAffinity.enabled` and `readAffinity.crushLocationLabels`
// values from the CSI config for the given `clusterID`. If `readAffinity.enabled` is set to true
// it returns `true` and `crushLocationLabels`, else returns `false` and an empty string.
func GetCrushLocationLabels(pathToConfig, clusterID string) (bool, string, error) {
	cluster, err := readClusterInfo(pathToConfig, clusterID)
	if err != nil {
		return false, "", err
	}

	if !cluster.ReadAffinity.Enabled {
		return false, "", nil
	}

	crushLocationLabels := strings.Join(cluster.ReadAffinity.CrushLocationLabels, ",")

	return true, crushLocationLabels, nil
}

// GetCephFSMountOptions returns the `kernelMountOptions` and `fuseMountOptions` for CephFS volumes.
func GetCephFSMountOptions(pathToConfig, clusterID string) (string, string, error) {
	cluster, err := readClusterInfo(pathToConfig, clusterID)
	if err != nil {
		return "", "", err
	}

	return cluster.CephFS.KernelMountOptions, cluster.CephFS.FuseMountOptions, nil
}

// GetRBDControllerPublishSecretRef returns the secret name and namespace used for
// controller publish operations for RBD volumes.
func GetRBDControllerPublishSecretRef(pathToConfig, clusterID string) (string, string, error) {
	cluster, err := readClusterInfo(pathToConfig, clusterID)
	if err != nil {
		return "", "", err
	}

	secretRef := cluster.RBD.ControllerPublishSecretRef

	return secretRef.Name, secretRef.Namespace, nil
}

// GetCephFSControllerPublishSecretRef returns the secret name and namespace used for
// controller publish operations for CephFS volumes.
func GetCephFSControllerPublishSecretRef(pathToConfig, clusterID string) (string, string, error) {
	cluster, err := readClusterInfo(pathToConfig, clusterID)
	if err != nil {
		return "", "", err
	}

	secretRef := cluster.CephFS.ControllerPublishSecretRef

	return secretRef.Name, secretRef.Namespace, nil
}

// parseV1ClusterIDs tries to parse clusterIDsStr as a YAML/JSON list (v1 format).
// Detection: trimmed string starts with '-' (YAML list) or '[' (JSON array).
// Returns (entries, true, nil) on success, (nil, false, nil) if the string
// does not look like a list (legacy comma-separated fallback), or
// (nil, false, err) on malformed YAML/JSON.
func parseV1ClusterIDs(s string) ([]kubernetes.SCClusterEntry, bool, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "-") && !strings.HasPrefix(s, "[") {
		return nil, false, nil
	}
	var entries []kubernetes.SCClusterEntry
	if err := sigyaml.Unmarshal([]byte(s), &entries); err != nil {
		return nil, false, fmt.Errorf("clusterIDs looks like a list but failed to parse as v1 format: %w", err)
	}
	return entries, true, nil
}

// buildClusterInfoFromSCEntry constructs a ClusterInfo from an SCClusterEntry
// and a single topology zone map. Secret refs, fsName and pool are
// pre-populated so downstream code needs no additional ConfigMap lookups.
func buildClusterInfoFromSCEntry(entry kubernetes.SCClusterEntry, zone map[string]string) kubernetes.ClusterInfo {
	ci := kubernetes.ClusterInfo{
		ClusterID:            entry.ClusterID,
		TopologyDomainLabels: zone,
		CephFS: kubernetes.CephFS{
			FsName: entry.FsName,
			Pool:   entry.Pool,
		},
	}
	if entry.ProvisionerSecretName != "" {
		ci.CephFS.ProvisionerSecretRef = corev1.SecretReference{
			Name:      entry.ProvisionerSecretName,
			Namespace: entry.ProvisionerSecretNamespace,
		}
	}
	if entry.NodeStageSecretName != "" {
		ci.CephFS.NodeStageSecretRef = corev1.SecretReference{
			Name:      entry.NodeStageSecretName,
			Namespace: entry.NodeStageSecretNamespace,
		}
	}
	if entry.ControllerExpandSecretName != "" {
		ci.CephFS.ControllerExpandSecretRef = corev1.SecretReference{
			Name:      entry.ControllerExpandSecretName,
			Namespace: entry.ControllerExpandSecretNamespace,
		}
	}
	return ci
}

// expandSCEntriesToClusterInfos converts []SCClusterEntry to []ClusterInfo.
// Each entry's topologyDomainLabels list is expanded into separate ClusterInfo
// entries (one per zone), because matchClusterTopology operates on a single
// map[string]string. "Cluster X serves zones B,C,S" → three candidate entries.
func expandSCEntriesToClusterInfos(entries []kubernetes.SCClusterEntry) []kubernetes.ClusterInfo {
	var result []kubernetes.ClusterInfo
	for _, entry := range entries {
		if len(entry.TopologyDomainLabels) == 0 {
			result = append(result, buildClusterInfoFromSCEntry(entry, nil))
		} else {
			for _, zone := range entry.TopologyDomainLabels {
				result = append(result, buildClusterInfoFromSCEntry(entry, zone))
			}
		}
	}
	return result
}

// GetClusterInfoByTopologyV1 checks if the clusterIDs option is in v1 YAML/JSON
// format and if so resolves the matching ClusterInfo directly from the SC entry.
// Returns (nil, false, nil) when clusterIDs is not v1 format — caller falls back
// to the legacy comma-separated path. Returns (nil, false, err) on any error.
func GetClusterInfoByTopologyV1(
	options map[string]string,
	topologyReq *csi.TopologyRequirement,
) (*kubernetes.ClusterInfo, bool, error) {
	clusterIDsStr, ok := options[ClusterIDsKey]
	if !ok || clusterIDsStr == "" {
		return nil, false, nil
	}

	entries, isV1, err := parseV1ClusterIDs(clusterIDsStr)
	if err != nil {
		return nil, false, err
	}
	if !isV1 {
		return nil, false, nil
	}

	if topologyReq == nil {
		return nil, false, fmt.Errorf("topology requirements are nil, cannot select cluster from v1 clusterIDs")
	}

	candidates := expandSCEntriesToClusterInfos(entries)

	copyClusterInfo := func(ci kubernetes.ClusterInfo) *kubernetes.ClusterInfo {
		out := ci
		if len(ci.TopologyDomainLabels) > 0 {
			out.TopologyDomainLabels = make(map[string]string, len(ci.TopologyDomainLabels))
			for k, v := range ci.TopologyDomainLabels {
				out.TopologyDomainLabels[k] = v
			}
		}
		return &out
	}

	for _, topology := range topologyReq.GetPreferred() {
		for i := range candidates {
			if matchClusterTopology(&candidates[i], topology.GetSegments()) {
				return copyClusterInfo(candidates[i]), true, nil
			}
		}
	}
	for _, topology := range topologyReq.GetRequisite() {
		for i := range candidates {
			if matchClusterTopology(&candidates[i], topology.GetSegments()) {
				return copyClusterInfo(candidates[i]), true, nil
			}
		}
	}

	return nil, false, fmt.Errorf(
		"no cluster from v1 clusterIDs matches the topology requirements (preferred: %v, requisite: %v)",
		topologyReq.GetPreferred(), topologyReq.GetRequisite())
}

// GetProvisionerSecretRefForCluster returns the ProvisionerSecretRef for the
// given clusterID from the v1 clusterIDs format in the options map.
// Returns (nil, false, nil) when clusterIDs is absent or not v1 format — caller
// falls back to the standard Kubernetes-provided secrets.
// Returns (nil, false, err) on malformed v1 format.
func GetProvisionerSecretRefForCluster(options map[string]string, clusterID string) (*corev1.SecretReference, bool, error) {
	clusterIDsStr, ok := options[ClusterIDsKey]
	if !ok || clusterIDsStr == "" {
		return nil, false, nil
	}
	entries, isV1, err := parseV1ClusterIDs(clusterIDsStr)
	if err != nil {
		return nil, false, err
	}
	if !isV1 {
		return nil, false, nil
	}
	for _, entry := range entries {
		if entry.ClusterID == clusterID && entry.ProvisionerSecretName != "" {
			return &corev1.SecretReference{
				Name:      entry.ProvisionerSecretName,
				Namespace: entry.ProvisionerSecretNamespace,
			}, true, nil
		}
	}
	return nil, false, nil
}

// GetNodeStageSecretRefForCluster returns the NodeStageSecretRef for the given
// clusterID from the v1 clusterIDs format in the options map.
// Returns (nil, false, nil) when clusterIDs is absent or not v1 format — caller
// falls back to the standard Kubernetes-provided secrets.
// Returns (nil, false, err) on malformed v1 format.
func GetNodeStageSecretRefForCluster(options map[string]string, clusterID string) (*corev1.SecretReference, bool, error) {
	clusterIDsStr, ok := options[ClusterIDsKey]
	if !ok || clusterIDsStr == "" {
		return nil, false, nil
	}
	entries, isV1, err := parseV1ClusterIDs(clusterIDsStr)
	if err != nil {
		return nil, false, err
	}
	if !isV1 {
		return nil, false, nil
	}
	for _, entry := range entries {
		if entry.ClusterID == clusterID && entry.NodeStageSecretName != "" {
			return &corev1.SecretReference{
				Name:      entry.NodeStageSecretName,
				Namespace: entry.NodeStageSecretNamespace,
			}, true, nil
		}
	}
	return nil, false, nil
}

// readAllClusterInfos reads and returns all cluster entries from the config file.
func readAllClusterInfos(pathToConfig string) ([]kubernetes.ClusterInfo, error) {
	var config []kubernetes.ClusterInfo

	// #nosec
	content, err := os.ReadFile(pathToConfig)
	if err != nil {
		return nil, fmt.Errorf("error reading CSI config file %q: %w", pathToConfig, err)
	}

	err = json.Unmarshal(content, &config)
	if err != nil {
		return nil, fmt.Errorf("unmarshal failed (%w), raw buffer response: %s",
			err, string(content))
	}

	return config, nil
}

// matchClusterTopology checks if a cluster's TopologyDomainLabels match
// the given topology segments. All labels defined in the cluster config
// must be present and match in the topology segments.
func matchClusterTopology(cluster *kubernetes.ClusterInfo, segments map[string]string) bool {
	if len(cluster.TopologyDomainLabels) == 0 {
		return false
	}

	for label, value := range cluster.TopologyDomainLabels {
		segValue, ok := segments[label]
		if !ok || segValue != value {
			return false
		}
	}

	return true
}

// FindClusterByTopology selects a cluster from the config file based on
// topology requirements. It filters clusters by the given clusterIDs list
// and matches their TopologyDomainLabels against the AccessibilityRequirements.
// Preferred topologies are checked first, then requisite.
func FindClusterByTopology(
	pathToConfig string,
	clusterIDs []string,
	topologyReq *csi.TopologyRequirement,
) (string, error) {
	if topologyReq == nil {
		return "", fmt.Errorf("topology requirements are nil, cannot select cluster")
	}

	allClusters, err := readAllClusterInfos(pathToConfig)
	if err != nil {
		return "", err
	}

	// build a set of allowed clusterIDs for fast lookup
	allowed := make(map[string]bool, len(clusterIDs))
	for _, id := range clusterIDs {
		allowed[strings.TrimSpace(id)] = true
	}

	// filter clusters to only those in the allowed list
	var candidates []kubernetes.ClusterInfo
	for i := range allClusters {
		if allowed[allClusters[i].ClusterID] {
			candidates = append(candidates, allClusters[i])
		}
	}

	if len(candidates) == 0 {
		return "", fmt.Errorf("none of the cluster IDs %v found in CSI config %q", clusterIDs, pathToConfig)
	}

	// check preferred topologies first
	for _, topology := range topologyReq.GetPreferred() {
		for i := range candidates {
			if matchClusterTopology(&candidates[i], topology.GetSegments()) {
				return candidates[i].ClusterID, nil
			}
		}
	}

	// fall back to requisite topologies
	for _, topology := range topologyReq.GetRequisite() {
		for i := range candidates {
			if matchClusterTopology(&candidates[i], topology.GetSegments()) {
				return candidates[i].ClusterID, nil
			}
		}
	}

	return "", fmt.Errorf(
		"no cluster from %v matches the topology requirements (preferred: %v, requisite: %v)",
		clusterIDs, topologyReq.GetPreferred(), topologyReq.GetRequisite())
}

// GetClusterIDByTopology checks if the options contain a "clusterIDs" parameter
// and resolves the appropriate clusterID based on topology requirements.
// Returns ErrClusterIDNotSet if "clusterIDs" is not present in the options.
func GetClusterIDByTopology(
	options map[string]string,
	pathToConfig string,
	topologyReq *csi.TopologyRequirement,
) (string, error) {
	clusterIDsStr, ok := options[ClusterIDsKey]
	if !ok || clusterIDsStr == "" {
		return "", ErrClusterIDNotSet
	}

	clusterIDs := strings.Split(clusterIDsStr, ",")

	return FindClusterByTopology(pathToConfig, clusterIDs, topologyReq)
}
