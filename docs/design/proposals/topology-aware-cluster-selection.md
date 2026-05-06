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
2. **`clusterIDs`** StorageClass parameter — either a legacy comma-separated
   list of candidate cluster IDs or, for CephFS, a v1 YAML/JSON list encoded
   as a block-scalar string in the StorageClass.
3. **`Volume.AccessibleTopology`** — the driver returns the selected cluster
   topology so that the external-provisioner can translate it into
   `PV.spec.nodeAffinity`.

For CephFS, the v1 `clusterIDs` format also allows per-cluster:

- `fsName`
- `pool`
- `csi.storage.k8s.io/provisioner-secret-*`
- `csi.storage.k8s.io/node-stage-secret-*`
- `csi.storage.k8s.io/controller-expand-secret-*`

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

For CephFS, `clusterIDs` can also use a v1 YAML/JSON list. Because
`StorageClass.parameters` is `map[string]string`, this list must be passed as
one string value, for example with YAML block-scalar syntax:

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: csi-cephfs-topology-v1
provisioner: cephfs.csi.ceph.com
parameters:
  clusterIDs: |
    - clusterID: cluster-poland
      csi.storage.k8s.io/provisioner-secret-name: csi-cephfs-secret-poland
      csi.storage.k8s.io/provisioner-secret-namespace: ceph-system
      csi.storage.k8s.io/node-stage-secret-name: csi-cephfs-node-poland
      csi.storage.k8s.io/node-stage-secret-namespace: ceph-system
      csi.storage.k8s.io/controller-expand-secret-name: csi-cephfs-expand-poland
      csi.storage.k8s.io/controller-expand-secret-namespace: ceph-system
      fsName: cephfs-poland
      pool: cephfs-poland.data
      topologyDomainLabels:
        - topology.kubernetes.io/zone: zone-poland
    - clusterID: cluster-france
      csi.storage.k8s.io/provisioner-secret-name: csi-cephfs-secret-france
      csi.storage.k8s.io/provisioner-secret-namespace: ceph-system
      csi.storage.k8s.io/node-stage-secret-name: csi-cephfs-node-france
      csi.storage.k8s.io/node-stage-secret-namespace: ceph-system
      csi.storage.k8s.io/controller-expand-secret-name: csi-cephfs-expand-france
      csi.storage.k8s.io/controller-expand-secret-namespace: ceph-system
      fsName: cephfs-france
      pool: cephfs-france.data
      topologyDomainLabels:
        - topology.kubernetes.io/zone: zone-france
volumeBindingMode: WaitForFirstConsumer
reclaimPolicy: Delete
```

#### Secret

When `clusterIDs` is used, the provisioner Secret may carry both the standard
credentials and per-cluster overrides. If keys named
`<clusterID>.userID` and `<clusterID>.userKey` are present, Ceph-CSI uses
them for the cluster selected during `CreateVolume`. Otherwise it falls back
to the plain `userID` and `userKey` entries.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: csi-cephfs-secret
  namespace: ceph-system
stringData:
  userID: admin
  userKey: AQDefaultKey==
  cluster-poland.userID: admin-lublin
  cluster-poland.userKey: AQLublinKey==
  cluster-france.userID: admin-ovh
  cluster-france.userKey: AQOvhKey==
```

For CephFS v1 `clusterIDs`, the secret references can also be carried directly
inside the `clusterIDs` entry. In that mode:

- `CreateVolume` resolves `ProvisionerSecretRef`
- `NodeStageVolume` resolves `NodeStageSecretRef` from `VolumeContext`
- `ControllerExpandVolume` resolves `ControllerExpandSecretRef` using the PV's
  stored `volumeAttributes`, because CSI `ControllerExpandVolumeRequest` does
  not carry `VolumeContext`

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
Resolve "clusterIDs"
  ├── v1 YAML/JSON list? ──YES──► GetClusterInfoByTopologyV1(options, topologyReq)
  │                                │
  │                                ├── Parse block-scalar YAML/JSON list
  │                                ├── Expand topologyDomainLabels into candidates
  │                                ├── Match Preferred/Requisite topologies
  │                                └── Return matching ClusterInfo
  │
  ├── legacy comma-separated list? ──YES──► GetClusterIDByTopology(options, configPath, topologyReq)
  │                                           │
  │                                           ▼
  │                                         FindClusterByTopology(configPath, clusterIDs, topologyReq)
  │                                           │
  │                                           ├── Read all ClusterInfo entries from config.json
  │                                           ├── Filter to only clusters in the clusterIDs list
  │                                           ├── Match TopologyDomainLabels against Preferred topologies
  │                                           ├── Fallback: match against Requisite topologies
  │                                           └── Return first matching clusterID
  │
  NO ──► error: clusterID or clusterIDs must be set
        │
        ▼
  Continue with selected clusterID
  (monitors, pools, fsName, secrets, OMAP — resolved from the selected path)
        │
        ▼
  Copy selected cluster's TopologyDomainLabels
  into Volume.AccessibleTopology
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
7. The driver copies the selected cluster's `TopologyDomainLabels` into
   `Volume.AccessibleTopology`.
8. The external-provisioner translates `AccessibleTopology` into
   `PV.spec.nodeAffinity`, so later pod scheduling is constrained to nodes
   that match the selected cluster topology.
9. The selected `clusterID` is encoded into the `volumeHandle`, so all
   subsequent operations (mount, expand, delete) use the correct cluster
   without needing topology resolution again.

For CephFS v1 `clusterIDs`, steps 4-6 are slightly richer:

4. `GetClusterInfoByTopologyV1` parses the block-scalar YAML/JSON string from
   `parameters.clusterIDs` and returns the matching entry directly.
5. The matching entry may override `fsName`, `pool`, and the per-cluster
   secret references for provision, node-stage, and controller-expand.
6. Monitors are still resolved from `config.json`, but CephFS-specific runtime
   options can now come from the selected `clusterIDs` entry.

### Secret Resolution After CreateVolume

For CephFS v1 `clusterIDs`, the selected cluster information is used by later
operations as follows:

1. `CreateVolume` uses `ProvisionerSecretRef` if it is set in the selected
   `clusterIDs` entry.
2. `NodeStageVolume` receives `VolumeContext`, so it can resolve
   `NodeStageSecretRef` from the selected `clusterIDs` block without changing
   the shared volume lookup path.
3. `ControllerExpandVolume` does not receive `VolumeContext` in the CSI spec,
   so the controller looks up the PV by `volumeHandle`, reads
   `PV.spec.csi.volumeAttributes`, and resolves `ControllerExpandSecretRef`
   from there.

**The key outcome: the RBD image is physically created in the Ceph cluster
that matches the node's topology zone.** This ensures data locality — the
storage backend is in the same zone as the compute node. When
`topologyDomainLabels` are configured for the selected cluster, the resulting
PV also carries a matching node affinity via `AccessibleTopology`.

**Important requirement:** The StorageClass **must** use
`volumeBindingMode: WaitForFirstConsumer`. With `Immediate` binding,
Kubernetes calls `CreateVolume` before scheduling the pod, so no
`AccessibilityRequirements` are provided and topology selection fails.

## Changes

### New / Modified Files

| File | Change |
|------|--------|
| `api/deploy/kubernetes/csi-config-map.go` | Added `CephFS` per-cluster fields (`FsName`, `Pool`, secret refs) and `SCClusterEntry` for v1 `clusterIDs` |
| `vendor/.../csi-config-map.go` | Same (vendor copy) |
| `internal/util/csiconfig.go` | Added v1 `clusterIDs` parser/helper functions and per-cluster secret-ref lookup helpers |
| `internal/util/csiconfig_test.go` | Added tests for v1 `clusterIDs` parsing, topology selection, and secret-ref resolution |
| `internal/rbd/controllerserver.go` | Relaxed `validateVolumeReq` to accept `clusterID` OR `clusterIDs`; passed `AccessibilityRequirements` to `genVolFromVolumeOptions`; added fallback that sets `rbdVol.Topology` from the selected cluster when `clusterIDs` is used |
| `internal/rbd/rbd_util.go` | Added `topologyReq *csi.TopologyRequirement` parameter to `genVolFromVolumeOptions`; added fallback from `GetClusterID` to `GetClusterIDByTopology` |
| `internal/rbd/controllerserver_test.go` | Added coverage for serializing `Topology` into `Volume.AccessibleTopology` |
| `internal/rbd/nodeserver.go` | Pass `nil` for new `topologyReq` parameter (node-side operations don't need topology selection) |
| `internal/cephfs/store/volumeoptions.go` | Added v1 `clusterIDs` resolution in `GetClusterInformation`; propagates selected topology and per-cluster provisioner secret/fs/pool settings into `VolumeOptions` |
| `internal/cephfs/store/volumegroup.go` | Pass `nil` for new `topologyReq` parameter |
| `internal/cephfs/nodeserver.go` | Resolves `NodeStageSecretRef` in the node path using `VolumeContext` |
| `internal/cephfs/controllerserver.go` | Resolves `ControllerExpandSecretRef` in the expand path using the PV's `volumeAttributes` |
| `internal/cephfs/controllerserver_test.go` | Added coverage for serializing `Topology` into `Volume.AccessibleTopology` |
| `internal/util/k8s/persistentvolumes.go` | Added helper to find a PV by `volumeHandle` for expand-time secret resolution |
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
| StorageClass with `clusterIDs` + `WaitForFirstConsumer` | New path — topology-based cluster selection; when the selected cluster has `topologyDomainLabels`, `AccessibleTopology` is returned and the PV gets matching `nodeAffinity` |
| CephFS StorageClass with v1 `clusterIDs: | ...` | New CephFS-only path — topology selects a full per-cluster entry, including `fsName`, `pool`, and secret refs |
| StorageClass with `clusterIDs` but selected cluster has no `topologyDomainLabels` | Volume provisioning still works, but no additional `AccessibleTopology` / PV node affinity is produced |
| StorageClass with `clusterIDs` but `Immediate` binding | Fails — no `AccessibilityRequirements` provided by CO |
| StorageClass with `topologyConstrainedPools` | Existing pool-based topology handling is preserved; cluster-level topology is only used as a fallback when no volume topology was already set |
| Delete/Expand/Mount of volumes created with topology | Works — volumeHandle has the selected clusterID encoded |
| CephFS v1 `clusterIDs` with no per-cluster secret refs | Falls back to the standard request secret behavior |

## Future Work (Phase 2)

In a future iteration, once this approach is validated:

- Make `clusterID` fully optional in StorageClass when `clusterIDs` is provided
  (currently both are accepted, but at least one is required)
- Remove the need for the fallback pattern — `clusterIDs` becomes a first-class
  alternative to `clusterID`
- Add E2E tests with multi-cluster topology setup, including verification that
  `AccessibleTopology` is translated into `PV.spec.nodeAffinity` and prevents
  scheduling volumes onto nodes outside the selected cluster topology
