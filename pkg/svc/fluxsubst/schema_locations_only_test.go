package fluxsubst_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/fluxsubst"
	"github.com/stretchr/testify/require"
	"github.com/yannh/kubeconform/pkg/cache"
)

// TestOnlySchemaLocationsTypeFromTheSelectedSchema prevents cached implicit
// schemas from choosing a placeholder type that the explicit validator rejects.
//
//nolint:paralleltest // Owns an isolated process-global cache environment.
func TestOnlySchemaLocationsTypeFromTheSelectedSchema(t *testing.T) {
	home := t.TempDir()
	for _, key := range []string{"HOME", "USERPROFILE", "XDG_CACHE_HOME", "LOCALAPPDATA"} {
		t.Setenv(key, home)
	}

	cacheDir := fluxsubst.SchemaCacheDir()
	require.NoError(t, os.MkdirAll(cacheDir, 0o700))
	store := cache.NewOnDiskCache(cacheDir)
	require.NoError(
		t,
		store.Set("https://raw.githubusercontent.com/yannh/kubernetes-json-schema/master/"+
			"master-standalone-strict/configmap-v1.json", []byte(`{"type":"object","properties":{
"data":{"type":"object","properties":{"attempts":{"type":"string"}}}}}`)),
	)
	local := filepath.Join(t.TempDir(), "configmap_v1.json")
	require.NoError(t, os.WriteFile(local, []byte(`{"type":"object","properties":{
"data":{"type":"object","properties":{"attempts":{"type":"integer"}}}}}`), 0o600))

	input := []byte(
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: typing\ndata:\n  attempts: ${ATTEMPTS}\n",
	)
	opts := fluxsubst.SchemaOptions{
		Strict:              true,
		SchemaLocationsOnly: true,
		SchemaLocations:     []string{local},
	}
	expanded := fluxsubst.ExpandFluxSubstitutionsWithOptions(input, opts)
	require.YAMLEq(
		t,
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: typing\ndata:\n  attempts: 0\n",
		string(expanded),
	)

	opts.SchemaLocationsOnly = false
	expanded = fluxsubst.ExpandFluxSubstitutionsWithOptions(input, opts)
	require.YAMLEq(
		t,
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: typing\ndata:\n  attempts: placeholder\n",
		string(expanded),
	)
}
