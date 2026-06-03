package cni

import (
	"encoding/json"
	"path"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestConfigureCNI(t *testing.T) {
	mgr := newTestInstaller(t)
	type test struct {
		name      string
		sources   []source
		expConfig map[string]any
		expErr    string
		setup     func(*testing.T, *test)
	}
	tests := []test{
		{
			name:      "NoConfigurationSource",
			sources:   []source{},
			expConfig: nil,
			expErr:    errNoConfigurationSource.Error(),
			setup:     func(_ *testing.T, _ *test) {},
		},
		{
			name: "EnvSourceNotSet",
			sources: []source{
				&environmentSource{
					key: "CNI_NETWORK_CONFIG",
				},
			},
			expConfig: nil,
			expErr:    errNoConfigurationSource.Error(),
			setup:     func(_ *testing.T, _ *test) {},
		},
		{
			name: "EnvSource",
			sources: []source{
				&environmentSource{
					key: "CNI_NETWORK_CONFIG",
				},
			},
			expConfig: nil,
			expErr:    "",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.expConfig = mustReadUnmarshal(t, "testdata/cni-exp.json", json.Unmarshal)
				t.Setenv("CNI_NETWORK_CONFIG", string(mustReadFile(t, "testdata/cni-src.json")))
				t.Setenv(containerMountPrefix.key, "/media")
				t.Setenv(cniConfigDir.key, "/config")
				t.Setenv(kubeConfigFilenameVar.key, "/test-linkerd-cni-kubeconfig")
			},
		},
		{
			name: "FileSourceNotSet",
			sources: []source{
				&fileSource{
					filename: "testdata/no-such-file.json",
				},
			},
			expConfig: nil,
			expErr:    errNoConfigurationSource.Error(),
			setup: func(t *testing.T, _ *test) {
				t.Setenv(containerMountPrefix.key, "/media")
				t.Setenv(cniConfigDir.key, "/config")
				t.Setenv(kubeConfigFilenameVar.key, "/test-linkerd-cni-kubeconfig")
			},
		},
		{
			name: "FileSource",
			sources: []source{
				&fileSource{
					filename: "testdata/cni-src.json",
				},
			},
			expConfig: nil,
			expErr:    "",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.expConfig = mustReadUnmarshal(t, "testdata/cni-exp.json", json.Unmarshal)
				t.Setenv(containerMountPrefix.key, "/media")
				t.Setenv(cniConfigDir.key, "/config")
				t.Setenv(kubeConfigFilenameVar.key, "/test-linkerd-cni-kubeconfig")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.setup(t, &test)
			actConfigData, err := mgr.configureCNI(test.sources)
			if assertErr(t, test.expErr, err) {
				return
			}
			var actConfig map[string]any
			err = json.Unmarshal(actConfigData, &actConfig)
			if err != nil {
				t.Fatalf("cannot unmarshal actual config data err=%v", err)
			}
			assertDeepEqual(t, test.expConfig, actConfig)
		})
	}
}

func TestReconfigureK8s(t *testing.T) {
	type test struct {
		name              string
		dstConfigFilename string
		srcTokenFilename  string
		expConfig         map[string]any
		expErr            string
		mgr               *installer
		setup             func(*testing.T, *test)
	}
	tests := []test{
		{
			name:              "NoSuchTokenFile",
			dstConfigFilename: "",
			srcTokenFilename:  "no-such-file",
			expConfig:         nil,
			expErr:            "open no-such-file: no such file or directory",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.mgr = newTestInstaller(t)
			},
		},
		{
			name:              "NoSuchConfigDirectory",
			dstConfigFilename: "/no-such-directory/config.yaml",
			srcTokenFilename:  "testdata/auth-token",
			expConfig:         nil,
			expErr:            "open /no-such-directory/config.yaml.install: no such file or directory",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.mgr = newTestInstaller(t)
				t.Setenv(svcHost.key, "localhost")
				t.Setenv(svcPort.key, "8080")
				t.Setenv(svcProtocol.key, "https")
			},
		},
		{
			name:              "NoEnvHost",
			dstConfigFilename: "",
			srcTokenFilename:  "testdata/auth-token",
			expConfig:         nil,
			expErr:            "service-host is zero length",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.mgr = newTestInstaller(t)
				t.Setenv(svcPort.key, "8080")
				t.Setenv(svcProtocol.key, "https")
			},
		},
		{
			name:              "NoEnvPort",
			dstConfigFilename: "",
			srcTokenFilename:  "testdata/auth-token",
			expConfig:         nil,
			expErr:            "service-port is zero length",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.mgr = newTestInstaller(t)
				t.Setenv(svcHost.key, "localhost")
				t.Setenv(svcProtocol.key, "https")
			},
		},
		{
			name:              "ReconfigureK8s",
			dstConfigFilename: "",
			srcTokenFilename:  "testdata/auth-token",
			expErr:            "",
			expConfig:         nil,
			setup: func(t *testing.T, self *test) {
				t.Helper()
				root := t.TempDir()
				self.dstConfigFilename = path.Join(root, "config.yml")
				self.expConfig = mustReadUnmarshal(t, "testdata/kubeconfig-exp.yaml", yaml.Unmarshal)
				self.mgr = newTestInstaller(t)

				t.Setenv(kubeCAFile.key, "testdata/k8s/ca.crt")
				t.Setenv(svcHost.key, "localhost")
				t.Setenv(svcPort.key, "8080")
			},
		},
		{
			name:              "ZeroToken",
			dstConfigFilename: "",
			srcTokenFilename:  "testdata/zero-byte-file",
			expErr:            "token-data is zero-length",
			expConfig:         nil,
			setup: func(t *testing.T, self *test) {
				t.Helper()
				root := t.TempDir()
				self.dstConfigFilename = path.Join(root, "config.yml")
				self.mgr = newTestInstaller(t)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.setup(t, &test)
			err := test.mgr.reconfigureK8s(test.dstConfigFilename, test.srcTokenFilename)
			if assertErr(t, test.expErr, err) {
				return
			}
			actConfig := mustReadUnmarshal(t, test.dstConfigFilename, yaml.Unmarshal)
			assertDeepEqual(t, test.expConfig, actConfig)
		})
	}
}

func TestReconfigureCNI(t *testing.T) {
	type test struct {
		name           string
		configFilename string
		expErr         string
		expFileHash    string
		mgr            *installer
		setup          func(*testing.T, *test)
	}
	tests := []test{
		{
			name:           "NoSuchConfigFilename",
			configFilename: "",
			expErr:         "", // no expected error
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.configFilename = "testdata/10-calico-no-such-file.conflist"
				self.mgr = newTestInstaller(t)
			},
		},
		{
			name:           "FileHashExists",
			configFilename: "",
			expErr:         "",
			expFileHash:    "f3a5162f9a1d3c2695a7639d4ca6c862cbf76eae692fa872ad1675c22cc7ccbe",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.configFilename = mustCopyFile(t, t.TempDir(), "testdata/10-calico.conflist")
				self.mgr = newTestInstaller(t)
				self.mgr.fileHashSet[self.configFilename] = "f3a5162f9a1d3c2695a7639d4ca6c862cbf76eae692fa872ad1675c22cc7ccbe"
			},
		},
		{
			name:           "ConfigureFromEnvironmentSingle",
			configFilename: "",
			expErr:         "",
			expFileHash:    "359027232e5561a414ec1b639e6fb3ffb359fcdf5cb86bfafbbaefb9d809d40f",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.configFilename = mustCopyFile(t, t.TempDir(), "testdata/10-calico.conf")
				self.mgr = newTestInstaller(t)
				const envKey = "TEST_CONFIGURE_FROM_ENV"
				self.mgr.sources = append(self.mgr.sources, &environmentSource{
					key: envKey,
				})
				t.Setenv(envKey, string(mustReadFile(t, "testdata/cni-src.json")))
			},
		},
		{
			name:           "ConfigureFromEnvironment",
			configFilename: "",
			expErr:         "",
			expFileHash:    "3c8b1e29bbddb441472dd5262a8c9d7a7e2094b5dcf91cb11537770dbf311077",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.configFilename = mustCopyFile(t, t.TempDir(), "testdata/10-calico.conflist")
				self.mgr = newTestInstaller(t)
				const envKey = "TEST_CONFIGURE_FROM_ENV"
				self.mgr.sources = append(self.mgr.sources, &environmentSource{
					key: envKey,
				})
				t.Setenv(envKey, string(mustReadFile(t, "testdata/cni-src.json")))
			},
		},
		{
			name:           "ConfigureFromFileSingle",
			configFilename: "",
			expErr:         "",
			expFileHash:    "359027232e5561a414ec1b639e6fb3ffb359fcdf5cb86bfafbbaefb9d809d40f",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.configFilename = mustCopyFile(t, t.TempDir(), "testdata/10-calico.conf")
				self.mgr = newTestInstaller(t)
				self.mgr.sources = append(self.mgr.sources, &fileSource{
					filename: "testdata/cni-src.json",
				})
			},
		},
		{
			name:           "ConfigureFromFile",
			configFilename: "",
			expErr:         "",
			expFileHash:    "3c8b1e29bbddb441472dd5262a8c9d7a7e2094b5dcf91cb11537770dbf311077",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.configFilename = mustCopyFile(t, t.TempDir(), "testdata/10-calico.conflist")
				self.mgr = newTestInstaller(t)
				self.mgr.sources = append(self.mgr.sources, &fileSource{
					filename: "testdata/cni-src.json",
				})
			},
		},
		{
			name:           "ConfigureFromEnvironmentAndFile",
			configFilename: "",
			expErr:         "",
			expFileHash:    "3c8b1e29bbddb441472dd5262a8c9d7a7e2094b5dcf91cb11537770dbf311077",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.configFilename = mustCopyFile(t, t.TempDir(), "testdata/10-calico.conflist")
				self.mgr = newTestInstaller(t)
				self.mgr.sources = append(self.mgr.sources,
					&environmentSource{
						key: "TEST_CONFIGURE_FROM_ENV", // deliberately not set
					},
					&fileSource{
						filename: "testdata/cni-src.json",
					},
				)
			},
		},
		{
			name:           "ConfigureFromFileAndEnvironment",
			configFilename: "",
			expErr:         "",
			expFileHash:    "3c8b1e29bbddb441472dd5262a8c9d7a7e2094b5dcf91cb11537770dbf311077",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.configFilename = mustCopyFile(t, t.TempDir(), "testdata/10-calico.conflist")
				self.mgr = newTestInstaller(t)
				self.mgr.sources = append(self.mgr.sources,
					&fileSource{
						filename: "testdata/no-such-file.json",
					},
					&environmentSource{
						key: "TEST_CONFIGURE_FROM_ENV",
					},
				)
				t.Setenv("TEST_CONFIGURE_FROM_ENV", string(mustReadFile(t, "testdata/cni-src.json")))
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.setup(t, &test)
			err := test.mgr.reconfigureCNI(test.configFilename)
			if assertErr(t, test.expErr, err) {
				return
			}
			if test.expFileHash != test.mgr.fileHashSet[test.configFilename] {
				t.Fatalf("configuration file hash is not set as expected '%s'<>'%s'",
					test.expFileHash,
					test.mgr.fileHashSet[test.configFilename])
			}
		})
	}
}
