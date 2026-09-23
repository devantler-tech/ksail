package fluxinstaller

import (
	"maps"
	"reflect"
	"strings"

	"sigs.k8s.io/yaml"
)

const (
	// kustomizeSpecKey is the FluxInstance spec field KSail and the consumer both write patches to.
	kustomizeSpecKey = "kustomize"
	// kustomizePatchesKey holds the patch list inside spec.kustomize.
	kustomizePatchesKey = "patches"
	// verifyPatchPath is the only path KSail's own spec.kustomize patch touches.
	verifyPatchPath = "/spec/verify"
	// verifyPatchOperation is the only JSON6902 operation KSail's own verify patch performs.
	verifyPatchOperation = "add"
)

// mergeModelledField writes KSail's desired value for one modelled spec field into spec.
//
// A field whose type is an object (spec.distribution, spec.sync) is KSail's only in the keys that
// type models: each of those is replaced, or removed when KSail sets none, so a value KSail stops
// setting does not linger. Keys the type does not model, such as distribution.variant,
// distribution.artifactPullSecret and distribution.imagePullSecret, belong to the consumer and are
// left as they are. Any other field is KSail's as a whole.
func mergeModelledField(
	spec map[string]any,
	key string,
	fieldType reflect.Type,
	desired any,
	set bool,
) {
	objectType := fieldType
	if objectType.Kind() == reflect.Pointer {
		objectType = objectType.Elem()
	}

	if objectType.Kind() != reflect.Struct {
		if set {
			spec[key] = desired
		} else {
			delete(spec, key)
		}

		return
	}

	desiredObject, _ := desired.(map[string]any)
	merged := copyObject(spec[key])

	for _, modelledKey := range modelledJSONKeys(objectType) {
		value, ok := desiredObject[modelledKey]
		if ok {
			merged[modelledKey] = value
		} else {
			delete(merged, modelledKey)
		}
	}

	setOrDelete(spec, key, merged)
}

// mergeKustomize writes KSail's spec.kustomize patches into spec without disturbing the
// consumer's. desired is KSail's spec.kustomize, nil when it sets none. Keys of spec.kustomize
// other than patches are left as they are.
func mergeKustomize(spec map[string]any, desired any) {
	desiredKustomize, _ := desired.(map[string]any)
	desiredPatches, _ := desiredKustomize[kustomizePatchesKey].([]any)

	merged := copyObject(spec[kustomizeSpecKey])
	livePatches, _ := merged[kustomizePatchesKey].([]any)

	patches := mergeKustomizePatches(livePatches, desiredPatches)
	if len(patches) == 0 {
		delete(merged, kustomizePatchesKey)
	} else {
		merged[kustomizePatchesKey] = patches
	}

	setOrDelete(spec, kustomizeSpecKey, merged)
}

// mergeKustomizePatches returns the live patch list with KSail's own patches swapped for desired.
//
// KSail and the consumer share spec.kustomize.patches: KSail writes one patch there, the verify
// patch from buildSyncKustomize, and a consumer may declare any number in Git. Every live patch
// isKSailVerifyPatch does not recognise is the consumer's and is kept, in its original order. The
// patches it does recognise are KSail's: the first one's position receives desired (so an update
// does not reorder the list), the rest are dropped, and desired is appended when there were none.
// With desired empty, KSail's patches are simply removed.
func mergeKustomizePatches(live, desired []any) []any {
	merged := make([]any, 0, len(live)+len(desired))
	placed := false

	for _, patch := range live {
		if !isKSailVerifyPatch(patch) {
			merged = append(merged, patch)

			continue
		}

		if !placed {
			merged = append(merged, desired...)
			placed = true
		}
	}

	if !placed {
		merged = append(merged, desired...)
	}

	return merged
}

// isKSailVerifyPatch reports whether a live spec.kustomize patch is the one KSail writes.
//
// The rule is target plus content, never position: the patch targets the OCIRepository named
// flux-system, and its body is a JSON6902 list of exactly one add operation on /spec/verify. A
// patch with that target that changes anything else, operates on /spec/verify other than by add
// (remove, replace, test), or adds /spec/verify together with something else, is the consumer's
// and is kept. A consumer patch that matches the rule, one add on /spec/verify of flux-system, is
// indistinguishable from KSail's and is treated as KSail's: spec.workload.flux.verify is where
// that verification is configured, so KSail's setting replaces it, and removes it when
// verification is off. A strategic-merge patch setting spec.verify is not recognised and is kept;
// it then competes with KSail's patch.
func isKSailVerifyPatch(patch any) bool {
	entry, isObject := patch.(map[string]any)
	if !isObject {
		return false
	}

	target, _ := entry["target"].(map[string]any)
	if target["kind"] != fluxOCIRepositoryKind || target["name"] != defaultOCIRepositoryName {
		return false
	}

	body, isString := entry["patch"].(string)
	if !isString {
		return false
	}

	var operations []map[string]any

	err := yaml.Unmarshal([]byte(body), &operations)
	if err != nil || len(operations) != 1 {
		return false
	}

	return operations[0]["op"] == verifyPatchOperation &&
		operations[0]["path"] == verifyPatchPath
}

// modelledJSONKeys returns the JSON names of the fields structType models.
func modelledJSONKeys(structType reflect.Type) []string {
	keys := make([]string, 0, structType.NumField())

	for field := range structType.Fields() {
		name := jsonFieldName(field)
		if name != "" {
			keys = append(keys, name)
		}
	}

	return keys
}

// jsonFieldName returns the JSON name of a struct field, or "" when the field is not serialised
// under its own name.
func jsonFieldName(field reflect.StructField) string {
	name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
	if name == "-" {
		return ""
	}

	return name
}

// copyObject returns a shallow copy of value when it is an object, and an empty object otherwise.
func copyObject(value any) map[string]any {
	object, _ := value.(map[string]any)
	copied := make(map[string]any, len(object))
	maps.Copy(copied, object)

	return copied
}

// setOrDelete stores object under key, or removes key when object is empty.
func setOrDelete(spec map[string]any, key string, object map[string]any) {
	if len(object) == 0 {
		delete(spec, key)

		return
	}

	spec[key] = object
}
