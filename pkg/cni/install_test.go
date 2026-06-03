package cni

import (
	"bytes"
	"os"
	"path"
	"testing"
)

func TestInstall(t *testing.T) {
	type test struct {
		name     string
		dst      string
		src      []string
		expFiles []string
		expErr   string
		mgr      *installer
		setup    func(*testing.T, *test)
	}
	tests := []test{
		{
			name:     "Install",
			dst:      t.TempDir(),
			src:      []string{"testdata/cni-binary", "testdata/10-calico.conflist"},
			expFiles: nil,
			expErr:   "",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.mgr = newTestInstaller(t)
				self.expFiles = make([]string, len(self.src))
				copy(self.expFiles, self.src)
			},
		},
		{
			name:     "InstallOverwrite",
			dst:      t.TempDir(),
			src:      []string{"testdata/cni-binary", "testdata/10-calico.conflist"},
			expFiles: nil,
			expErr:   "",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.expFiles = make([]string, len(self.src))
				copy(self.expFiles, self.src)
				// write zero-byte file to ensure overwrite behavior
				var err error
				var filename string
				for _, src := range self.src {
					filename = path.Join(self.dst, path.Base(src))
					err = os.WriteFile(filename, []byte{}, writeFilePerm)
					if err != nil {
						t.Fatalf("cannot write filename=%s err=%v", filename, err)
					}
				}
			},
		},
		{
			name:     "DestinationDoesNotExist",
			dst:      "no-such-directory",
			src:      nil,
			expFiles: nil,
			expErr:   "stat no-such-directory: no such file or directory",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.mgr = newTestInstaller(t)
			},
		},
		{
			name:     "DestinationIsNotADirectory",
			dst:      "testdata/cni-binary",
			src:      nil,
			expFiles: nil,
			expErr:   "dst=testdata/cni-binary is not a directory",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.mgr = newTestInstaller(t)
			},
		},
		{
			name:     "SourceDoesNotExist",
			dst:      t.TempDir(),
			src:      []string{"testdata/cni-binary-does-not-exist"},
			expFiles: nil,
			expErr:   "stat testdata/cni-binary-does-not-exist: no such file or directory",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.mgr = newTestInstaller(t)
			},
		},
		{
			name:     "SecondSourceDoesNotExist",
			dst:      t.TempDir(),
			src:      []string{"testdata/cni-binary", "testdata/cni-binary-does-not-exist"},
			expFiles: nil,
			expErr:   "stat testdata/cni-binary-does-not-exist: no such file or directory",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.mgr = newTestInstaller(t)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.setup(t, &test)
			actFiles, err := test.mgr.install(test.dst, test.src...)
			if assertErr(t, test.expErr, err) {
				return
			}
			if len(actFiles) != len(test.expFiles) {
				t.Fatalf("expected files installed does not match actual '%d'<>'%d' (%v<>%v)",
					len(test.expFiles), len(actFiles),
					test.expFiles, actFiles)
			}
			for at, actFile := range actFiles {
				actData := mustReadFile(t, actFile)
				expData := mustReadFile(t, test.expFiles[at])
				if !bytes.Equal(expData, actData) {
					t.Fatalf("expected file does not equal actual at=%d '%s'<>'%s",
						at, actFile, test.expFiles[at])
				}
			}
		})

	}
}

func TestInstallRegularFiles(t *testing.T) {
	type test struct {
		name     string
		dst      string
		src      string
		expErr   string
		expFiles []string
		mgr      *installer
		setup    func(*testing.T, *test)
	}
	tests := []test{
		{
			name:   "NoSuchSrcDir",
			dst:    "",
			src:    "/test/opt/cni/bin",
			expErr: "open /test/opt/cni/bin: no such file or directory",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.mgr = newTestInstaller(t)
			},
		},
		{
			name:   "NoSuchDstDir",
			dst:    "/test/host/opt/cni/bin",
			src:    "testdata/bin",
			expErr: "stat /test/host/opt/cni/bin: no such file or directory",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.mgr = newTestInstaller(t)
			},
		},
		{
			name:   "DstIsNotDir",
			dst:    "testdata/bin/cni-binary",
			src:    "testdata/bin",
			expErr: "dst=testdata/bin/cni-binary is not a directory",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.mgr = newTestInstaller(t)
			},
		},
		{
			name:     "InstallRegularFiles",
			dst:      "",
			src:      "testdata/bin",
			expErr:   "",
			expFiles: nil,
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.mgr = newTestInstaller(t)
				self.dst = t.TempDir()
				mustCopyFiles(t, self.dst, "testdata")
				self.expFiles = []string{path.Join(self.dst, "cni-binary")}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.setup(t, &test)
			actFiles, err := test.mgr.installRegularFiles(test.dst, test.src)
			if assertErr(t, test.expErr, err) {
				return
			}
			if len(test.expFiles) != len(actFiles) {
				t.Fatalf("expected files installed does not match actual '%d'<>'%d' (%v<>%v)",
					len(test.expFiles), len(actFiles),
					test.expFiles, actFiles)
			}
			for at, actFile := range actFiles {
				actData := mustReadFile(t, actFile)
				expData := mustReadFile(t, test.expFiles[at])
				if !bytes.Equal(expData, actData) {
					t.Fatalf("expected file does not equal actual at=%d '%s'<>'%s",
						at, actFile, test.expFiles[at])
				}
			}
		})
	}
}
