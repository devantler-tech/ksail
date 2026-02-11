package configmanager

import (
	"fmt"
	"reflect"
	"time"

	mapstructure "github.com/go-viper/mapstructure/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
