package extension

import (
	"encoding/json"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ObjectMeta struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace,omitempty"`
	UID               string            `json:"uid,omitempty"`
	ResourceVersion   string            `json:"resourceVersion,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	Finalizers        []string          `json:"finalizers,omitempty"`
	CreationTimestamp metav1.Time       `json:"creationTimestamp,omitempty"`
	DeletionTimestamp *metav1.Time      `json:"deletionTimestamp,omitempty"`
}

type Condition struct {
	Type               string      `json:"type,omitempty"`
	Status             string      `json:"status,omitempty"`
	Reason             string      `json:"reason,omitempty"`
	Message            string      `json:"message,omitempty"`
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}

type ClusterCondition struct {
	Type   string `json:"type"`
	Status string `json:"status"`
}

type ClusterStatus struct {
	Conditions []ClusterCondition `json:"conditions,omitempty"`
}

type Cluster struct {
	APIVersion string        `json:"apiVersion,omitempty"`
	Kind       string        `json:"kind,omitempty"`
	Metadata   ObjectMeta    `json:"metadata"`
	Status     ClusterStatus `json:"status,omitempty"`
}

type Provider struct {
	Name  string `json:"name,omitempty"`
	URL   string `json:"url,omitempty"`
	Email string `json:"email,omitempty"`
}

type ExtensionVersionInfo struct {
	Version           string      `json:"version"`
	CreationTimestamp metav1.Time `json:"creationTimestamp,omitempty"`
}

type Extension struct {
	APIVersion string          `json:"apiVersion,omitempty"`
	Kind       string          `json:"kind,omitempty"`
	Metadata   ObjectMeta      `json:"metadata"`
	Spec       ExtensionSpec   `json:"spec,omitempty"`
	Status     ExtensionStatus `json:"status,omitempty"`
}

type ExtensionSpec struct {
	DisplayName map[string]string    `json:"displayName,omitempty"`
	Description map[string]string    `json:"description,omitempty"`
	Provider    map[string]*Provider `json:"provider,omitempty"`
	Category    string               `json:"category,omitempty"`
}

type ExtensionStatus struct {
	State                     string                        `json:"state,omitempty"`
	Enabled                   *bool                         `json:"enabled,omitempty"`
	PlannedInstallVersion     string                        `json:"plannedInstallVersion,omitempty"`
	InstalledVersion          string                        `json:"installedVersion,omitempty"`
	RecommendedVersion        string                        `json:"recommendedVersion,omitempty"`
	Versions                  []ExtensionVersionInfo        `json:"versions,omitempty"`
	Conditions                []Condition                   `json:"conditions,omitempty"`
	ClusterSchedulingStatuses map[string]InstallationStatus `json:"clusterSchedulingStatuses,omitempty"`
}

type ExtensionVersion struct {
	APIVersion string               `json:"apiVersion,omitempty"`
	Kind       string               `json:"kind,omitempty"`
	Metadata   ObjectMeta           `json:"metadata"`
	Spec       ExtensionVersionSpec `json:"spec"`
}

type ExtensionVersionSpec struct {
	DisplayName          map[string]string    `json:"displayName,omitempty"`
	Description          map[string]string    `json:"description,omitempty"`
	Provider             map[string]*Provider `json:"provider,omitempty"`
	Version              string               `json:"version"`
	Repository           string               `json:"repository,omitempty"`
	Category             string               `json:"category,omitempty"`
	KubeVersion          string               `json:"kubeVersion,omitempty"`
	KSVersion            string               `json:"ksVersion,omitempty"`
	Home                 string               `json:"home,omitempty"`
	Docs                 string               `json:"docs,omitempty"`
	ChartURL             string               `json:"chartURL,omitempty"`
	Namespace            string               `json:"namespace,omitempty"`
	InstallationMode     string               `json:"installationMode,omitempty"`
	ExternalDependencies []ExternalDependency `json:"externalDependencies,omitempty"`
}

type ExternalDependency struct {
	Name     string `json:"name"`
	Type     string `json:"type,omitempty"`
	Version  string `json:"version,omitempty"`
	Required bool   `json:"required,omitempty"`
}

type ExtensionRef struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Placement struct {
	Clusters        []string              `json:"clusters,omitempty"`
	ClusterSelector *metav1.LabelSelector `json:"clusterSelector,omitempty"`
}

type ClusterScheduling struct {
	Placement *Placement        `json:"placement,omitempty"`
	Overrides map[string]string `json:"overrides,omitempty"`
}

type InstallPlan struct {
	APIVersion string            `json:"apiVersion,omitempty"`
	Kind       string            `json:"kind,omitempty"`
	Metadata   ObjectMeta        `json:"metadata"`
	Spec       InstallPlanSpec   `json:"spec"`
	Status     InstallPlanStatus `json:"status,omitempty"`
}

type InstallPlanSpec struct {
	Extension         ExtensionRef       `json:"extension"`
	Enabled           bool               `json:"enabled"`
	UpgradeStrategy   string             `json:"upgradeStrategy,omitempty"`
	Config            string             `json:"config,omitempty"`
	ClusterScheduling *ClusterScheduling `json:"clusterScheduling,omitempty"`
}

type InstallPlanState struct {
	State              string      `json:"state,omitempty"`
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}

type InstallationStatus struct {
	State           string             `json:"state,omitempty"`
	Version         string             `json:"version,omitempty"`
	ConfigHash      string             `json:"configHash,omitempty"`
	TargetNamespace string             `json:"targetNamespace,omitempty"`
	ReleaseName     string             `json:"releaseName,omitempty"`
	JobName         string             `json:"jobName,omitempty"`
	Conditions      []Condition        `json:"conditions,omitempty"`
	StateHistory    []InstallPlanState `json:"stateHistory,omitempty"`
}

type InstallPlanStatus struct {
	InstallationStatus
	Enabled                   *bool                         `json:"enabled,omitempty"`
	ClusterSchedulingStatuses map[string]InstallationStatus `json:"clusterSchedulingStatuses,omitempty"`
}

type Job = batchv1.Job
type PodList = corev1.PodList
type Namespace = corev1.Namespace

type JSONDocument struct {
	data json.RawMessage
}
