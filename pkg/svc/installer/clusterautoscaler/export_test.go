package clusterautoscalerinstaller

import "sigs.k8s.io/yaml"

// RenderedValuesYAML exposes the values the installer renders, so tests can
// build a deployed release that matches or diverges from them exactly.
func (i *Installer) RenderedValuesYAML() string {
	values, err := i.RenderedValues()
	if err != nil {
		panic("render cluster-autoscaler values: " + err.Error())
	}

	out, err := yaml.Marshal(values)
	if err != nil {
		panic("encode cluster-autoscaler values: " + err.Error())
	}

	return string(out)
}
