package configmanager

import (
	"fmt"
	"reflect"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	mapstructure "github.com/go-viper/mapstructure/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// clusterDecodeHook is the pre-compiled mapstructure decode hook for KSail cluster
// configuration. It is stateless (no captured mutable state) and therefore safe
// to share across all ConfigManager instances. Precomputing it avoids allocating
// a new function value on every Load() call.
//
//nolint:gochecknoglobals // Stateless decode hook shared by all ConfigManager instances.
var clusterDecodeHook = mapstructure.ComposeDecodeHookFunc(
	metav1DurationDecodeHook(),
	autoscalerExpanderListDecodeHook(),
)

// autoscalerExpanderListDecodeHook normalises a scalar autoscaler expander value
// into an AutoscalerExpanderList so that both the legacy scalar form
// (expander: LeastWaste) and the priority-list form (expander: [A, B]) are
// accepted. A comma-separated scalar (expander: "LeastNodes,LeastWaste") is split
// into its entries, matching the upstream cluster-autoscaler --expander syntax.
func autoscalerExpanderListDecodeHook() mapstructure.DecodeHookFuncType {
	return func(fromType reflect.Type, toType reflect.Type, data any) (any, error) {
		if toType != reflect.TypeFor[v1alpha1.AutoscalerExpanderList]() {
			return data, nil
		}

		if fromType.Kind() != reflect.String {
			return data, nil
		}

		raw, ok := data.(string)
		if !ok {
			return v1alpha1.AutoscalerExpanderList{}, nil
		}

		return v1alpha1.SplitAutoscalerExpanders(raw), nil
	}
}

// metav1DurationDecodeHook converts duration strings (e.g. "1m", "30s") into metav1.Duration values
// so that string values in ksail.yaml or environment variables are accepted.
func metav1DurationDecodeHook() mapstructure.DecodeHookFuncType {
	return func(fromType reflect.Type, toType reflect.Type, data any) (any, error) {
		durationType := reflect.TypeFor[metav1.Duration]()
		pointerDurationType := reflect.TypeFor[*metav1.Duration]()

		if toType != durationType && toType != pointerDurationType {
			return data, nil
		}

		if fromType.Kind() != reflect.String {
			return data, nil
		}

		raw, ok := data.(string)
		if !ok {
			return data, nil
		}

		if raw == "" {
			if toType == pointerDurationType {
				return &metav1.Duration{}, nil
			}

			return metav1.Duration{}, nil
		}

		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("parse duration %q: %w", raw, err)
		}

		durationValue := metav1.Duration{Duration: parsed}

		if toType == pointerDurationType {
			return &durationValue, nil
		}

		return durationValue, nil
	}
}
