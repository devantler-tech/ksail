package clusterautoscalerinstaller

// RenderedValuesYAML exposes the values the installer renders, so tests can
// build a deployed release that matches or diverges from them exactly.
func (i *Installer) RenderedValuesYAML() string {
	return i.valuesYaml
}
