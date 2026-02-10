package helm

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/devantler-tech/ksail/v5/pkg/fsutil"
	helmv4strvals "helm.sh/helm/v4/pkg/strvals"
	"sigs.k8s.io/yaml"
)

func (c *Client) mergeValues(spec *ChartSpec, chartPath string) (map[string]any, error) {
	base := map[string]any{}

	err := mergeValueFiles(spec.ValueFiles, chartPath, base)
	if err != nil {
		return nil, err
	}

	err = mergeValuesYaml(spec.ValuesYaml, base)
	if err != nil {
		return nil, err
	}

	err = mergeSetValues(spec.SetValues, base)
	if err != nil {
		return nil, err
	}

	err = mergeSetJSONValues(spec.SetJSONVals, base)
	if err != nil {
		return nil, err
	}

	err = mergeSetFileValues(spec.SetFileVals, chartPath, base)
	if err != nil {
		return nil, err
	}

	return base, nil
}

func mergeValueFiles(valueFiles []string, chartPath string, base map[string]any) error {
	for _, filePath := range valueFiles {
		fileBytes, err := readFileFromPath(chartPath, filePath)
		if err != nil {
			return fmt.Errorf("failed to read values file %s: %w", filePath, err)
		}

		var parsedMap map[string]any

		err = yaml.Unmarshal(fileBytes, &parsedMap)
		if err != nil {
			return fmt.Errorf("failed to parse values file %s as YAML: %w", filePath, err)
		}

		if parsedMap != nil {
			mergeMapsInto(base, parsedMap)
		}
	}

	return nil
}

func mergeValuesYaml(valuesYaml string, base map[string]any) error {
	if valuesYaml == "" {
		return nil
	}

	var parsedMap map[string]any

	err := yaml.Unmarshal([]byte(valuesYaml), &parsedMap)
	if err != nil {
		return fmt.Errorf("failed to parse ValuesYaml: %w", err)
	}

	mergeMapsInto(base, parsedMap)

	return nil
}

func mergeSetValues(setValues map[string]string, base map[string]any) error {
	for key, val := range setValues {
		err := helmv4strvals.ParseInto(fmt.Sprintf("%s=%s", key, val), base)
		if err != nil {
			return fmt.Errorf("failed to parse set value %s=%s: %w", key, val, err)
		}
	}

	return nil
}

func mergeSetJSONValues(setJSONVals map[string]string, base map[string]any) error {
	for key, val := range setJSONVals {
		err := helmv4strvals.ParseJSON(fmt.Sprintf("%s=%s", key, val), base)
		if err != nil {
			return fmt.Errorf("failed to parse JSON value %s=%s: %w", key, val, err)
		}
	}

	return nil
}

func mergeSetFileValues(
	setFileVals map[string]string,
	chartPath string,
	base map[string]any,
) error {
	for key, filePath := range setFileVals {
		fileBytes, err := readFileFromPath(chartPath, filePath)
		if err != nil {
			return fmt.Errorf("failed to read file value %s: %w", filePath, err)
		}

		err = helmv4strvals.ParseInto(fmt.Sprintf("%s=%s", key, string(fileBytes)), base)
		if err != nil {
			return fmt.Errorf("failed to parse file value %s: %w", key, err)
		}
	}

	return nil
}

func readFileFromPath(chartPath, filePath string) ([]byte, error) {
	if filepath.IsAbs(filePath) {
		data, err := os.ReadFile(filePath) //nolint:gosec // filePath is validated by caller
		if err != nil {
			return nil, fmt.Errorf("read file %s: %w", filePath, err)
		}

		return data, nil
	}

	data, err := fsutil.ReadFileSafe(filepath.Dir(chartPath), filePath)
	if err != nil {
		return nil, fmt.Errorf("read file safe %s: %w", filePath, err)
	}

	return data, nil
}

func mergeMapsInto(dest, src map[string]any) {
	for key, srcVal := range src {
		if srcMap, ok := srcVal.(map[string]any); ok {
			if destVal, exists := dest[key]; exists {
				if destMap, ok := destVal.(map[string]any); ok {
					mergeMapsInto(destMap, srcMap)

					continue
				}
			}
		}

		dest[key] = srcVal
	}
}
