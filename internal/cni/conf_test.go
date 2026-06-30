package cni

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"
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
			},
		},
		{
			name:              "ReconfigureK8sSiblingCAFile",
			dstConfigFilename: "",
			srcTokenFilename:  "testdata/auth-token",
			expErr:            "",
			expConfig:         nil,
			setup: func(t *testing.T, self *test) {
				t.Helper()
				root := t.TempDir()
				self.dstConfigFilename = path.Join(root, "config.yml")
				self.expConfig = mustReadUnmarshal(t, "testdata/kubeconfig-exp-sibling.yaml", yaml.Unmarshal)
				self.mgr = newTestInstaller(t)

				t.Setenv(svcHost.key, "localhost")
				t.Setenv(svcPort.key, "8080")
			},
		},
		{
			name:              "ReconfigureK8sEnvKubeCAFile",
			dstConfigFilename: "",
			srcTokenFilename:  "testdata/auth-token",
			expErr:            "",
			expConfig:         nil,
			setup: func(t *testing.T, self *test) {
				t.Helper()
				root := t.TempDir()
				self.dstConfigFilename = path.Join(root, "config.yml")
				self.expConfig = mustReadUnmarshal(t, "testdata/kubeconfig-exp-env.yaml", yaml.Unmarshal)
				self.mgr = newTestInstaller(t)

				t.Setenv(kubeCAFile.key, "testdata/k8s/ca.crt")
				t.Setenv(svcHost.key, "localhost")
				t.Setenv(svcPort.key, "8080")
			},
		},
		{
			name:              "ReconfigureK8sEnvZeroAuthorityData",
			dstConfigFilename: "",
			srcTokenFilename:  "testdata/auth-token",
			expErr:            "certificate authority data is zero-length",
			expConfig:         nil,
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.mgr = newTestInstaller(t)

				t.Setenv(kubeCAFile.key, "testdata/k8s/ca-zero.crt")
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
			expFileHash:    "341bcfc4025295e880f80537e4c53acb7017f612d02a594b8afd81d226ef606b",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.configFilename = mustCopyFile(t, t.TempDir(), "testdata/10-calico.conflist")
				self.mgr = newTestInstaller(t)
				self.mgr.fileHashSet[self.configFilename] = "341bcfc4025295e880f80537e4c53acb7017f612d02a594b8afd81d226ef606b"
			},
		},
		{
			name:           "ConfigureFromEnvironmentSingle",
			configFilename: "",
			expErr:         "",
			expFileHash:    "bf997c592ad9e2fbfcd20b537bcc2f25f39cf4663480333c5845c7f3766ea1d0",
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
			expFileHash:    "e3861068c0aba86e574cf7c0ed20a5d972c2053e132b64ceb1a169ccb47f7f5b",
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
			expFileHash:    "bf997c592ad9e2fbfcd20b537bcc2f25f39cf4663480333c5845c7f3766ea1d0",
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
			expFileHash:    "e3861068c0aba86e574cf7c0ed20a5d972c2053e132b64ceb1a169ccb47f7f5b",
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
			name:           "ConfigureFromFileAgain",
			configFilename: "",
			expErr:         "",
			expFileHash:    "e3861068c0aba86e574cf7c0ed20a5d972c2053e132b64ceb1a169ccb47f7f5b",
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.configFilename = mustCopyFile(t, t.TempDir(), "testdata/10-calico-linkerd.conflist")
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
			expFileHash:    "e3861068c0aba86e574cf7c0ed20a5d972c2053e132b64ceb1a169ccb47f7f5b",
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
			expFileHash:    "e3861068c0aba86e574cf7c0ed20a5d972c2053e132b64ceb1a169ccb47f7f5b",
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
			// .conf files are deleted and re-written as .conflist files
			key := test.configFilename
			if strings.HasSuffix(test.configFilename, ".conf") {
				key = fmt.Sprintf("%slist", test.configFilename)
				if _, err := os.Stat(test.configFilename); err == nil {
					t.Fatalf("did not delete .conf file as expected '%s'", test.configFilename)
				} else if !os.IsNotExist(err) {
					t.Fatalf("unexpected error stat-ing file '%s' %v",
						test.configFilename, err)
				}
			}
			if test.expFileHash != test.mgr.fileHashSet[key] {
				t.Fatalf("configuration file hash is not set as expected '%s'<>'%s'",
					test.expFileHash,
					test.mgr.fileHashSet[test.configFilename])
			}
		})
	}
}
