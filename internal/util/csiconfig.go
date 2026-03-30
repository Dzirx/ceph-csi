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
//
// Returns the matched clusterID and a copy of the matched entry's
// TopologyDomainLabels. Because the same clusterID may appear multiple times
// in the config with different TopologyDomainLabels, the topology is taken
// directly from the matched entry rather than re-looked-up by clusterID.
func FindClusterByTopology(
	pathToConfig string,
	clusterIDs []string,
	topologyReq *csi.TopologyRequirement,
) (string, map[string]string, error) {
	if topologyReq == nil {
		return "", nil, fmt.Errorf("topology requirements are nil, cannot select cluster")
	}

	allClusters, err := readAllClusterInfos(pathToConfig)
	if err != nil {
		return "", nil, err
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
		return "", nil, fmt.Errorf("none of the cluster IDs %v found in CSI config %q", clusterIDs, pathToConfig)
	}

	copyTopology := func(labels map[string]string) map[string]string {
		out := make(map[string]string, len(labels))
		for k, v := range labels {
			out[k] = v
		}
		return out
	}

	// check preferred topologies first
	for _, topology := range topologyReq.GetPreferred() {
		for i := range candidates {
			if matchClusterTopology(&candidates[i], topology.GetSegments()) {
				return candidates[i].ClusterID, copyTopology(candidates[i].TopologyDomainLabels), nil
			}
		}
	}

	// fall back to requisite topologies
	for _, topology := range topologyReq.GetRequisite() {
		for i := range candidates {
			if matchClusterTopology(&candidates[i], topology.GetSegments()) {
				return candidates[i].ClusterID, copyTopology(candidates[i].TopologyDomainLabels), nil
			}
		}
	}

	return "", nil, fmt.Errorf(
		"no cluster from %v matches the topology requirements (preferred: %v, requisite: %v)",
		clusterIDs, topologyReq.GetPreferred(), topologyReq.GetRequisite())
}

// GetClusterIDAndTopologyByTopology checks if the options contain a "clusterIDs"
// parameter and resolves the appropriate clusterID and its topology domain labels
// based on topology requirements. Returns ErrClusterIDNotSet if "clusterIDs" is
// not present in the options.
//
// The returned topology map is copied directly from the matched config entry,
// which is correct even when multiple entries share the same clusterID.
func GetClusterIDAndTopologyByTopology(
	options map[string]string,
	pathToConfig string,
	topologyReq *csi.TopologyRequirement,
) (string, map[string]string, error) {
	clusterIDsStr, ok := options[ClusterIDsKey]
	if !ok || clusterIDsStr == "" {
		return "", nil, ErrClusterIDNotSet
	}

	clusterIDs := strings.Split(clusterIDsStr, ",")

	return FindClusterByTopology(pathToConfig, clusterIDs, topologyReq)
}

// GetClusterIDByTopology checks if the options contain a "clusterIDs" parameter
// and resolves the appropriate clusterID based on topology requirements.
// Returns ErrClusterIDNotSet if "clusterIDs" is not present in the options.
func GetClusterIDByTopology(
	options map[string]string,
	pathToConfig string,
	topologyReq *csi.TopologyRequirement,
) (string, error) {
	clusterID, _, err := GetClusterIDAndTopologyByTopology(options, pathToConfig, topologyReq)

	return clusterID, err
}
