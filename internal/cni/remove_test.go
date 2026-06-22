package cni

import (
	"encoding/json"
	"errors"
	"os"
	"path"
	"testing"
)

// TestAppend ensures that the log is indexed by entry name.
func TestAppend(t *testing.T) {
	mgr := newTestInstaller(t)
	mgr.appendEntry(&testEntry{name: "first"})
	if len(mgr.log) != 1 {
		t.Fatalf("did not append entry name log")
	}
	if len(mgr.logIdx) != 1 {
		t.Fatalf("did not append entry name to index")
	}
	mgr.appendEntry(&testEntry{name: "first"})
	if len(mgr.log) != 1 {
		t.Fatalf("did not append entry name log")
	}
	if len(mgr.logIdx) != 1 {
		t.Fatalf("did not append entry name to index")
	}
	mgr.appendEntry(&testEntry{name: "second"})
	if len(mgr.log) != 2 {
		t.Fatalf("did not append entry name log")
	}
	if len(mgr.logIdx) != 2 {
		t.Fatalf("did not append entry name to index")
	}
}

// TestRemove tests remove on the installer to ensure that errors are collected
// but to not prevent calls to subsequent entries.
func TestRemove(t *testing.T) {
	type test struct {
		expErr string
		mgr    *installer
		name   string
		setup  func(*testing.T, *test)
	}
	tests := []test{
		{
			name:   "NoLog",
			expErr: "",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.mgr = newTestInstaller(t)
			},
		},
		{
			name:   "NoErrors",
			expErr: "",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.mgr = newTestInstaller(t)
				self.mgr.appendEntry(&testEntry{name: "one"})
				self.mgr.appendEntry(&testEntry{name: "two"})
				self.mgr.appendEntry(&testEntry{name: "three"})
				self.mgr.appendEntry(&testEntry{name: "four"})
			},
		},
		{
			name:   "Errors",
			expErr: "revert err at 1\nrevert error at 3",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.mgr = newTestInstaller(t)
				self.mgr.appendEntry(&testEntry{name: "one", err: errors.New("revert err at 1")})
				self.mgr.appendEntry(&testEntry{name: "two"})
				self.mgr.appendEntry(&testEntry{name: "three", err: errors.New("revert error at 3")})
				self.mgr.appendEntry(&testEntry{name: "four"})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.setup(t, &test)
			err := test.mgr.Remove()
			if assertErr(t, test.expErr, err) {
				return
			}
		})
	}
}

// TestRevert tests that each of the entry types reverts the change as expected.
func TestRevert(t *testing.T) {
	type test struct {
		e      entry
		expErr string
		name   string
		root   string
		setup  func(*testing.T, *test)
		assert func(*testing.T, *test)
	}
	tests := []test{
		{
			name:   "RevertInstalledNotExist",
			expErr: "",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.e = &installedFile{"testdata/no-such-file"}
			},
			assert: func(_ *testing.T, _ *test) {},
		},
		{
			name:   "RevertInstalled",
			expErr: "",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.root = t.TempDir()
				self.e = &installedFile{path.Join(self.root, "cni-binary")}
				mustCopyFile(t, self.root, "testdata/cni-binary")
			},
			assert: func(t *testing.T, self *test) {
				t.Helper()
				file := path.Join(self.root, "cni-binary")
				stat, err := os.Stat(file)
				if stat != nil {
					t.Fatalf("did not remove installed file=%s", file)
				} else {
					if os.IsNotExist(err) {
						return
					}
					t.Fatalf("unexpected error removing installed file err=%v", err)
				}
			},
		},
		{
			name:   "RevertK8sFileNotExist",
			expErr: "",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.e = &k8sFile{"testdata/no-such-file"}
			},
			assert: func(_ *testing.T, _ *test) {},
		},
		{
			name:   "RevertK8sFile",
			expErr: "",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.root = t.TempDir()
				self.e = &k8sFile{path.Join(self.root, "kubeconfig-src.yaml")}
				mustCopyFile(t, self.root, "testdata/kubeconfig-src.yaml")
			},
			assert: func(t *testing.T, self *test) {
				t.Helper()
				file := path.Join(self.root, "kubeconfig-src.yaml")
				stat, err := os.Stat(file)
				if stat != nil {
					t.Fatalf("did not remove installed file=%s", file)
				} else {
					if os.IsNotExist(err) {
						return
					}
					t.Fatalf("unexpected error removing installed file err=%v", err)
				}
			},
		},
		{
			name:   "RevertCNIFileNotExist",
			expErr: "",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.e = &cniFile{"testdata/no-such-file"}
			},
			assert: func(_ *testing.T, _ *test) {},
		},
		{
			name:   "RevertCNIFileNotJSON",
			expErr: "unexpected end of JSON input",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.root = t.TempDir()
				self.e = &cniFile{path.Join(self.root, "zero-byte-file")}
				mustCopyFile(t, self.root, "testdata/zero-byte-file")
			},
			assert: func(_ *testing.T, _ *test) {},
		},
		{
			name:   "RevertCNIFileNoPlugins",
			expErr: "cannot determine plugins from existing cni configuration",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.root = t.TempDir()
				self.e = &cniFile{path.Join(self.root, "cni-no-plugins.json")}
				mustCopyFile(t, self.root, "testdata/cni-no-plugins.json")
			},
			assert: func(_ *testing.T, _ *test) {},
		},
		{
			name:   "RevertCNIFileInvalidPlugin",
			expErr: "cannot extract plugin from configuration",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.root = t.TempDir()
				self.e = &cniFile{path.Join(self.root, "cni-invalid-plugin.json")}
				mustCopyFile(t, self.root, "testdata/cni-invalid-plugin.json")
			},
			assert: func(_ *testing.T, _ *test) {},
		},
		{
			name:   "RevertCNIFile",
			expErr: "",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.root = t.TempDir()
				self.e = &cniFile{path.Join(self.root, "10-calico-linkerd.conflist")}
				mustCopyFile(t, self.root, "testdata/10-calico-linkerd.conflist")
			},
			assert: func(t *testing.T, self *test) {
				t.Helper()
				name := path.Join(self.root, "10-calico-linkerd.conflist")
				val := mustReadUnmarshal(t, name, json.Unmarshal)
				plugins := val[cniKeyPlugins].([]any)
				for i := 0; i < len(plugins); i++ {
					plugin := plugins[i].(map[string]any)
					if plugin[cniKeyType] == "" {
						t.Fatalf("invalid plugin no key '%s' %+v", cniKeyType, plugin)
					}
					if plugin[cniKeyType] == cniValTypeLinkerd {
						t.Fatalf("did not revert linkerd plugin from config")
					}
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.setup(t, &test)
			err := test.e.revert()
			if assertErr(t, test.expErr, err) {
				return
			}
			test.assert(t, &test)
		})
	}
}

// testEntry returns err on revert.
type testEntry struct {
	name string
	err  error
}

// filename implements entry.
func (te testEntry) filename() string {
	return te.name
}

// revert implements entry.
func (te testEntry) revert() error {
	return te.err
}
