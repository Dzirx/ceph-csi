/*
Copyright 2025 The CephCSI Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	volumeSnapshotClassGVR = schema.GroupVersionResource{
		Group:    "snapshot.storage.k8s.io",
		Version:  "v1",
		Resource: "volumesnapshotclasses",
	}
)

type volumeSnapshotClassObject struct {
	Driver     string            `json:"driver"`
	Parameters map[string]string `json:"parameters"`
}

func newDynamicClient() (dynamic.Interface, error) {
	var cfg *rest.Config
	var err error
	cPath := os.Getenv("KUBERNETES_CONFIG_PATH")
	if cPath != "" {
		cfg, err = clientcmd.BuildConfigFromFlags("", cPath)
	} else {
		cfg, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster config: %w", err)
	}
	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}
	return client, nil
}

// GetSnapshotterSecretRefFromVolumeSnapshotClasses lists all VolumeSnapshotClasses
// for the given CSI driver and returns the snapshotter-secret ref for the given
// clusterID. Used by DeleteSnapshot when req.GetSecrets() is empty (v1 VSC format).
// Returns (nil, nil) when no matching class is found.
func GetSnapshotterSecretRefFromVolumeSnapshotClasses(
	driverName, clusterID string,
	getSnapshotterRef func(params map[string]string, clusterID string) (*corev1.SecretReference, bool, error),
) (*corev1.SecretReference, error) {
	dynClient, err := newDynamicClient()
	if err != nil {
		return nil, err
	}

	list, err := dynClient.Resource(volumeSnapshotClassGVR).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list volumesnapshotclasses: %w", err)
	}

	for i := range list.Items {
		raw, mErr := list.Items[i].MarshalJSON()
		if mErr != nil {
			continue
		}
		var vsc volumeSnapshotClassObject
		if mErr = json.Unmarshal(raw, &vsc); mErr != nil {
			continue
		}
		if vsc.Driver != driverName {
			continue
		}
		ref, isV1, rErr := getSnapshotterRef(vsc.Parameters, clusterID)
		if rErr != nil || !isV1 || ref == nil {
			continue
		}
		return ref, nil
	}

	return nil, nil
}
