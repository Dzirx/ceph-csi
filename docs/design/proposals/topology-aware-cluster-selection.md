# Topology-Aware Multi-Cluster Volume Provisioning

## Problem

Ceph-CSI currently supports only a single Ceph cluster per StorageClass.
In distributed Kubernetes environments spanning multiple geographic zones
(e.g. `zone-poland`, `zone-france`), each backed by a separate Ceph cluster,
there is no way to automatically provision volumes on the zone-local Ceph
cluster based on pod scheduling topology.

Administrators must create a separate StorageClass per zone/cluster, and
application teams must manually select the correct StorageClass depending on
where their workloads run. This defeats the purpose of Kubernetes topology-aware
scheduling and creates operational overhead.

Reference: [ceph/ceph-csi#5177](https://github.com/ceph/ceph-csi/issues/5177)

## Solution Overview

Introduce **topology-aware cluster selection** — the CSI driver dynamically
picks the right Ceph cluster at `CreateVolume` time based on the node's
topology zone, using two new configuration mechanisms:

1. **`topologyDomainLabels`** in `config.json` — associates each cluster entry
   with Kubernetes topology labels (e.g. zone).
2. **`clusterIDs`** StorageClass parameter — a comma-separated list of
   candidate cluster IDs. The driver selects the one matching the volume's
   topology requirements.

### Design Principles

- **Minimal invasiveness**: the existing `clusterID` parameter and all current
  flows remain unchanged. The new mechanism is an alternative path.
- **Backward compatibility**: configs without `topologyDomainLabels` work
  exactly as before. StorageClasses with a single `clusterID` are unaffected.
- **Incremental approach** (Phase 2 planned): in a future iteration, `clusterID`
  can be made fully optional when `clusterIDs` + topology are provided.

## Architecture

### Configuration

#### config.json (CSI ConfigMap)

Each cluster entry gains an optional `topologyDomainLabels` field:

```json
[
  {
    "clusterID": "cluster-poland",
    "topologyDomainLabels": {
      "topology.kubernetes.io/zone": "zone-poland"
    },
    "monitors": ["10.0.1.1:6789"],
    "rbd": {}
  },
  {
    "clusterID": "cluster-france",
    "topologyDomainLabels": {
      "topology.kubernetes.io/zone": "zone-france"
    },
    "monitors": ["10.0.2.1:6789"],
    "rbd": {}
  }
]
```

#### StorageClass

A new parameter `clusterIDs` lists the candidate clusters. The StorageClass
**must** use `volumeBindingMode: WaitForFirstConsumer` so that Kubernetes
provides topology hints to the CSI driver via `AccessibilityRequirements`.

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: csi-rbd-topology
provisioner: rbd.csi.ceph.com
parameters:
  clusterIDs: "cluster-poland,cluster-france"
  pool: replicapool
  imageFeatures: layering
  csi.storage.k8s.io/provisioner-secret-name: csi-rbd-secret
  csi.storage.k8s.io/provisioner-secret-namespace: ceph-system
volumeBindingMode: WaitForFirstConsumer
reclaimPolicy: Delete
```

### Request Flow

```
CreateVolume request
        │
        ▼
GetClusterID(options)
  ├── clusterID found in params? ──YES──► use it (existing fast path)
  │
  NO
  │
  ▼
GetClusterIDByTopology(options, configPath, topologyReq)
  ├── "clusterIDs" param present?
  │     │
  │    YES
  │     │
  │     ▼
  │   FindClusterByTopology(configPath, clusterIDs, topologyReq)
  │     │
  │     ├── Read all ClusterInfo entries from config.json
  │     ├── Filter to only clusters in the clusterIDs list
  │     ├── Match TopologyDomainLabels against Preferred topologies
  │     ├── Fallback: match against Requisite topologies
  │     └── Return first matching clusterID
  │
  NO ──► error: clusterID or clusterIDs must be set
        │
        ▼
  Continue with selected clusterID
  (monitors, pools, OMAP — all resolved as usual)
```

### Topology Matching Algorithm

`matchClusterTopology(cluster, segments)` checks that **all** labels defined
in the cluster's `TopologyDomainLabels` are present and equal in the topology
segment. This allows multi-dimensional matching (e.g. zone + region).

The driver checks **Preferred** topologies first (in order), then falls back to
**Requisite** topologies. This follows the CSI spec's intent: preferred
topologies represent the CO's scheduling preference, while requisite represents
hard constraints.

## How Volume Creation Works with Topology

When a pod is scheduled on a node in `zone-poland`, the following happens:

1. Kubernetes sees `volumeBindingMode: WaitForFirstConsumer` and delays
   provisioning until the pod is scheduled to a node.
2. Once the pod is bound to a node in `zone-poland`, Kubernetes calls
   `CreateVolume` with `AccessibilityRequirements` containing the node's
   topology segments (e.g. `topology.kubernetes.io/zone: zone-poland`).
3. `genVolFromVolumeOptions` (RBD) or `GetClusterInformation` (CephFS) tries
   `GetClusterID` first — this fails because there is no single `clusterID`
   in the StorageClass parameters.
4. The fallback calls `GetClusterIDByTopology`, which:
   - Parses the `clusterIDs` parameter (`"cluster-poland,cluster-france"`)
   - Reads all `ClusterInfo` entries from `config.json`
   - Filters to only the listed cluster IDs
   - Matches each cluster's `TopologyDomainLabels` against the
     `AccessibilityRequirements` — finds `cluster-poland`
5. The selected `clusterID` (`cluster-poland`) is used to resolve monitors
   from `config.json` — the driver connects to the Ceph cluster in Poland.
6. The **RBD image (or CephFS subvolume) is created in that cluster**.
7. The selected `clusterID` is encoded into the `volumeHandle`, so all
   subsequent operations (mount, expand, delete) use the correct cluster
   without needing topology resolution again.

**The key outcome: the RBD image is physically created in the Ceph cluster
that matches the node's topology zone.** This ensures data locality — the
storage backend is in the same zone as the compute node.

**Important requirement:** The StorageClass **must** use
`volumeBindingMode: WaitForFirstConsumer`. With `Immediate` binding,
Kubernetes calls `CreateVolume` before scheduling the pod, so no
`AccessibilityRequirements` are provided and topology selection fails.

## Changes

### New / Modified Files

| File | Change |
|------|--------|
| `api/deploy/kubernetes/csi-config-map.go` | Added `TopologyDomainLabels map[string]string` field to `ClusterInfo` struct |
| `vendor/.../csi-config-map.go` | Same (vendor copy) |
| `internal/util/csiconfig.go` | Added constant `ClusterIDsKey`, 4 new functions: `readAllClusterInfos`, `matchClusterTopology`, `FindClusterByTopology`, `GetClusterIDByTopology` |
| `internal/util/csiconfig_test.go` | Added `TestFindClusterByTopology` and `TestGetClusterIDByTopology` |
| `internal/rbd/controllerserver.go` | Relaxed `validateVolumeReq` to accept `clusterID` OR `clusterIDs`; passed `AccessibilityRequirements` to `genVolFromVolumeOptions` |
| `internal/rbd/rbd_util.go` | Added `topologyReq *csi.TopologyRequirement` parameter to `genVolFromVolumeOptions`; added fallback from `GetClusterID` to `GetClusterIDByTopology` |
| `internal/rbd/nodeserver.go` | Pass `nil` for new `topologyReq` parameter (node-side operations don't need topology selection) |
| `internal/cephfs/store/volumeoptions.go` | Added `topologyReq` parameter to `GetClusterInformation` and `getVolumeOptions`; added topology fallback in `GetClusterInformation` |
| `internal/cephfs/store/volumegroup.go` | Pass `nil` for new `topologyReq` parameter |
| `internal/cephfs/controllerserver.go` | Pass `nil` for new `topologyReq` parameter (snapshot operations) |
| `deploy/csi-config-map-sample.yaml` | Added documentation and example for `topologyDomainLabels` |

### What Is NOT Changed

- `GetClusterID()` — unchanged, still reads `clusterID` from options
- `GetMonsAndClusterID()` — unchanged, works with the selected clusterID
- Volume handle encoding/decoding — unchanged, the selected clusterID is
  encoded into the volumeHandle as before
- Node plugin operations (NodeStage, NodePublish) — unchanged, volumeHandle
  already carries the correct clusterID
- DR cluster mapping (`cluster-mapping.json`) — unchanged, orthogonal feature

## Backward Compatibility

| Scenario | Behavior |
|----------|----------|
| Existing config.json without `topologyDomainLabels` | Works unchanged — field is `omitempty` |
| StorageClass with single `clusterID` | Fast path — `GetClusterID` succeeds, topology never consulted |
| StorageClass with `clusterIDs` + `WaitForFirstConsumer` | New path — topology-based cluster selection |
| StorageClass with `clusterIDs` but `Immediate` binding | Fails — no `AccessibilityRequirements` provided by CO |
| Delete/Expand/Mount of volumes created with topology | Works — volumeHandle has the selected clusterID encoded |

## Future Work (Phase 2)

In a future iteration, once this approach is validated:

- Make `clusterID` fully optional in StorageClass when `clusterIDs` is provided
  (currently both are accepted, but at least one is required)
- Remove the need for the fallback pattern — `clusterIDs` becomes a first-class
  alternative to `clusterID`
- Add E2E tests with multi-cluster topology setup
