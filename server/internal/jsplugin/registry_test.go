package jsplugin

import "testing"

func TestRegistryPluginsSortedByID(t *testing.T) {
	registry, err := LoadBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	plugins := registry.Plugins()
	if len(plugins) == 0 {
		t.Fatal("expected builtins")
	}
	for i := 1; i < len(plugins); i++ {
		if plugins[i-1].Manifest.ID > plugins[i].Manifest.ID {
			t.Fatalf("plugins not sorted: %s before %s", plugins[i-1].Manifest.ID, plugins[i].Manifest.ID)
		}
	}
}
