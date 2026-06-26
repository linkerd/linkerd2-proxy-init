package cni

import "testing"

func TestNewInstaller(t *testing.T) {
	t.Setenv(cniNetworkConfigFile.key, "/tmp/linkerd.yaml")
	mgr := NewInstaller()
	expSources := []string{
		"env:CNI_NETWORK_CONFIG",
		"file:/tmp/linkerd.yaml",
	}
	switch mgr := mgr.(type) {
	case *installer:
		if len(mgr.sources) != len(expSources) {
			t.Fatalf("default sources list is incorrect %d<>%d", len(expSources), len(mgr.sources))
		}
		for i := range expSources {
			if mgr.sources[i].name() != expSources[i] {
				t.Fatalf("default source is incorrect '%s'<>'%s'",
					expSources[i], mgr.sources[i].name())
			}
		}
	default:
		t.Fatalf("unexpected type %T", mgr)
	}
}
