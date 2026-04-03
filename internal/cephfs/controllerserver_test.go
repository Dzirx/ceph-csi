/*
Copyright 2026 The Ceph-CSI Authors.

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

package cephfs

import (
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/require"

	"github.com/ceph/ceph-csi/internal/cephfs/core"
	"github.com/ceph/ceph-csi/internal/cephfs/store"
)

func TestBuildCreateVolumeResponseSetsAccessibleTopology(t *testing.T) {
	t.Parallel()

	req := &csi.CreateVolumeRequest{
		Parameters: map[string]string{
			"clusterID": "cluster-a",
		},
	}
	volOptions := &store.VolumeOptions{
		SubVolume: core.SubVolume{
			Size: 1024,
		},
		RootPath: "/volumes/csi/subvol-a",
		Topology: map[string]string{
			"topology.kubernetes.io/zone": "zone-a",
		},
	}
	vID := &store.VolumeIdentifier{
		VolumeID:     "vol-0001",
		FsSubvolName: "subvol-a",
	}

	resp := buildCreateVolumeResponse(req, volOptions, vID)

	require.NotNil(t, resp)
	require.NotNil(t, resp.Volume)
	require.Len(t, resp.Volume.AccessibleTopology, 1)
	require.Equal(t, volOptions.Topology, resp.Volume.AccessibleTopology[0].Segments)
}
