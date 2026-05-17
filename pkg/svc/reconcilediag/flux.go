package reconcilediag

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

const fluxNamespace = "flux-system"

// fluxGVRKustomizations returns the GVR for Flux Kustomizations.
func fluxGVRKustomizations() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group: "kustomize.toolkit.fluxcd.io", Version: "v1", Resource: "kustomizations",
	}
}

// fluxGVRHelmReleases returns the GVR for Flux HelmReleases.
func fluxGVRHelmReleases() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group: "helm.toolkit.fluxcd.io", Version: "v2", Resource: "helmreleases",
	}
}

// fluxGVROCIRepositories returns the GVR for Flux OCIRepositories.
func fluxGVROCIRepositories() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group: "source.toolkit.fluxcd.io", Version: "v1", Resource: "ocirepositories",
	}
}

// FluxCollector gathers diagnostics for Flux reconciliation failures.
type FluxCollector struct {
	Dynamic   dynamic.Interface
	Clientset kubernetes.Interface
}

// Collect gathers a diagnostic report for Flux failures.
// All sub-collectors are best-effort: individual failures are silently skipped
// so the report contains whatever data is available.
func (c *FluxCollector) Collect(ctx context.Context) *Report {
	report := &Report{
		EventNamespace: fluxNamespace,
		EventLookback:  defaultEventLookback,
	}

	report.Sections = append(
		report.Sections,
		c.collectFailingCRs(ctx, "Failing Kustomizations", fluxGVRKustomizations(), fluxNamespace),
		c.collectFailingCRs(ctx, "Failing HelmReleases", fluxGVRHelmReleases(), ""),
		c.collectFailingCRs(
			ctx,
			"Failing OCIRepositories",
			fluxGVROCIRepositories(),
			fluxNamespace,
		),
	)

	report.FailingPods = c.collectFailingPods(ctx)
	report.Events = c.collectWarningEvents(ctx)

	return report
}

// collectFailingCRs lists CRs of a given GVR and returns those that are not ready.
// If namespace is empty, the query is cluster-wide.
// The method recovers from panics (e.g., when CRDs are not installed) so
// diagnostic collection never crashes the CLI.
func (c *FluxCollector) collectFailingCRs(
	ctx context.Context,
	heading string,
	gvr schema.GroupVersionResource,
	namespace string,
) ResourceSection {
	section := ResourceSection{Heading: heading}

	recovered := safeCollectCRs(ctx, c.Dynamic, gvr, namespace)
	if recovered == nil {
		return section
	}

	for i := range recovered {
		item := &recovered[i]

		ready, reason, message := extractReadyCondition(item)
		if ready {
			continue
		}

		// Skip items with no conditions yet (still initializing).
		if reason == "" && message == "" {
			continue
		}

		itemNamespace := item.GetNamespace()
		// Omit namespace if it matches the default for this resource type.
		if itemNamespace == namespace {
			itemNamespace = ""
		}

		section.Resources = append(section.Resources, FailingResource{
			Name:      item.GetName(),
			Namespace: itemNamespace,
			Reason:    reason,
			Message:   message,
		})
	}

	slices.SortFunc(section.Resources, func(a, b FailingResource) int {
		return strings.Compare(a.Namespace+"/"+a.Name, b.Namespace+"/"+b.Name)
	})

	return section
}

// safeCollectCRs lists CRs, recovering from panics (e.g., unregistered CRDs in fake clients).
func safeCollectCRs(
	ctx context.Context,
	dynClient dynamic.Interface,
	gvr schema.GroupVersionResource,
	namespace string,
) []unstructured.Unstructured {
	var items []unstructured.Unstructured

	func() {
		defer func() {
			if r := recover(); r != nil {
				items = nil
			}
		}()

		var client dynamic.ResourceInterface

		if namespace != "" {
			client = dynClient.Resource(gvr).Namespace(namespace)
		} else {
			client = dynClient.Resource(gvr)
		}

		list, err := client.List(ctx, metav1.ListOptions{})
		if err != nil {
			return
		}

		items = list.Items
	}()

	return items
}

// extractReadyCondition finds the Ready condition and returns (ready, reason, message).
func extractReadyCondition(obj *unstructured.Unstructured) (bool, string, string) {
	conditions, found, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if !found {
		return false, "", ""
	}

	for _, cond := range conditions {
		condMap, ok := cond.(map[string]any)
		if !ok {
			continue
		}

		condType, _, _ := unstructured.NestedString(condMap, "type")
		if condType != "Ready" {
			continue
		}

		condStatus, _, _ := unstructured.NestedString(condMap, "status")
		reason, _, _ := unstructured.NestedString(condMap, "reason")
		message, _, _ := unstructured.NestedString(condMap, "message")

		return condStatus == "True", reason, message
	}

	return false, "", ""
}

// collectFailingPods returns a pre-formatted string of failing pods in the Flux namespace.
func (c *FluxCollector) collectFailingPods(ctx context.Context) string {
	return k8s.DiagnosePodFailures(ctx, c.Clientset, []string{fluxNamespace})
}

// collectWarningEvents returns recent warning events from the Flux namespace.
func (c *FluxCollector) collectWarningEvents(ctx context.Context) []WarningEvent {
	return collectNamespaceWarningEvents(ctx, c.Clientset, fluxNamespace)
}

// collectNamespaceWarningEvents is a shared helper that queries warning events
// from a single namespace, filtering to the default lookback window (defaultEventLookback).
func collectNamespaceWarningEvents(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespace string,
) []WarningEvent {
	events, err := clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: "type=" + corev1.EventTypeWarning,
	})
	if err != nil {
		return nil
	}

	now := time.Now()
	cutoff := now.Add(-defaultEventLookback)

	var result []WarningEvent

	for i := range events.Items {
		evt := &events.Items[i]

		eventTime := eventTimestamp(evt)
		if eventTime.Before(cutoff) {
			continue
		}

		result = append(result, WarningEvent{
			Age:       now.Sub(eventTime),
			Kind:      evt.InvolvedObject.Kind,
			Name:      evt.InvolvedObject.Name,
			Namespace: evt.InvolvedObject.Namespace,
			Message:   fmt.Sprintf("%s (%s)", evt.Message, evt.Reason),
		})
	}

	slices.SortFunc(result, func(a, b WarningEvent) int {
		return cmp.Compare(a.Age, b.Age)
	})

	return result
}

// eventTimestamp returns the most relevant timestamp for an event.
func eventTimestamp(evt *corev1.Event) time.Time {
	if !evt.LastTimestamp.IsZero() {
		return evt.LastTimestamp.Time
	}

	if evt.EventTime.Time.IsZero() {
		return evt.CreationTimestamp.Time
	}

	return evt.EventTime.Time
}
