package cni

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// TestRun tests the Installer's Run call.  It clones the testdata directory to
// the test temp directory, sets environment variables for:
//
//	container mount prefix -> test temp dir
//	binary dir -> test temp dir + bin
//	config dir -> test temp dir + etc
//	binary dir -> testdata + bin
//	kube ca file -> testdata + k8s + ca.crt (does not exist, does not need to)
//	kube config file -> linkerd-kubeconfig.json
//	service host|port -> localhost:8080
//	cni config -> value of testdata/cni-src.json
//
// the runs the installer.  It checks for valid file data:
//
//	installed binaries
//	k8s config
//	cni config
func TestRun(t *testing.T) {
	type test struct {
		name               string
		expInstalledFiles  []string
		expK8sConfig       map[string]any
		expCNIConfig       map[string]any
		expCNIConfigRevert map[string]any
		expCNIConfigFile   string
		expErr             string
		expLog             []entry
		expRemoveErr       string
		mgr                *installer
		doIO               func(*testing.T, *test)
		root               string
		setup              func(*testing.T, *test)
	}
	tests := []test{
		{
			name:              "TestRun",
			expInstalledFiles: nil,
			expCNIConfig:      nil,
			expK8sConfig:      nil,
			expErr:            "",
			expLog:            nil,
			expRemoveErr:      "",
			doIO: func(t *testing.T, self *test) {
				// this mimics what kubernetes is doing with the auth token
				// directories:
				//	create a new directory
				//	write the new token to it
				//	remove the link ..data -> auth-root
				//	create the link ..data -> new-auth-root
				//
				// this will fire a create event for the auth token file's root
				newAuthRoot := path.Join(self.root, "..new-auth-root")
				mustMkdir(t, newAuthRoot)
				mustCopyFile(t, newAuthRoot, "testdata/auth-token-new")
				mustRemove(t, path.Join(self.root, "..data"))
				mustLink(t, newAuthRoot, path.Join(self.root, "..data"))
				mustRemove(t, path.Join(self.root, "..auth-root"))
			},
			setup: func(t *testing.T, self *test) {
				t.Helper()
				self.root = t.TempDir()
				mustCopyFiles(t, self.root, "testdata")

				self.mgr = newTestInstaller(t)
				self.mgr.serviceAccountTokenFilename = path.Join(self.root, "auth-token")
				self.mgr.sources = []source{
					&environmentSource{key: "TEST_CONFIGURE_FROM_ENV"},
				}
				// mimic the kubernetes file-system:
				//	root/..auth-root/auth-token <- the auth token file
				//	root/..data -> root/..auth-root
				//	root/auth-token -> root/..data/auth-token
				// when the token is update is is written to a new auth root
				// directory, and ..data is updated to point at the new root
				authRoot := path.Join(self.root, "..auth-root")
				mustMkdir(t, authRoot)
				mustCopyFile(t, authRoot, self.mgr.serviceAccountTokenFilename)
				mustRemove(t, self.mgr.serviceAccountTokenFilename)
				dataDir := path.Join(self.root, "..data")
				mustLink(t, authRoot, dataDir)
				mustLink(t, path.Join(dataDir, "auth-token"), self.mgr.serviceAccountTokenFilename)

				self.expK8sConfig = mustReadUnmarshal(t, "testdata/kubeconfig-exp-env.yaml", yaml.Unmarshal)
				self.expCNIConfig = mustReadUnmarshal(t, "testdata/10-calico-exp.conflist", json.Unmarshal)
				self.expCNIConfigRevert = mustReadUnmarshal(t, "testdata/etc/10-calico.conflist", json.Unmarshal)
				self.expCNIConfigFile = path.Join(self.root, "etc", "10-calico.conflist")

				t.Setenv(containerMountPrefix.key, self.root)
				t.Setenv(cniBinDir.key, "bin")
				t.Setenv(cniConfigDir.key, "etc")
				// files in this directory are "installed" to the cni bin dir
				// above
				t.Setenv(containerCNIBinDir.key, "testdata/bin")
				// this file does not exist; nor does it need to; the path
				// is encoded into the config struct
				t.Setenv(kubeCAFile.key, "testdata/k8s/ca.crt")
				t.Setenv(kubeConfigFilenameVar.key, "linkerd-kubeconfig.json")
				t.Setenv(svcHost.key, "localhost")
				t.Setenv(svcPort.key, "8080")
				t.Setenv("TEST_CONFIGURE_FROM_ENV", string(mustReadFile(t, "testdata/cni-src.json")))

				// kubeconfig filename is based on the environment set in the
				// block above
				self.expCNIConfig["plugins"].([]any)[2].(map[string]any)["kubernetes"].(map[string]any)["kubeconfig"] = pluginKubeConfigFilename()
				self.expInstalledFiles = []string{"testdata/bin/cni-binary"}
				for _, file := range self.expInstalledFiles {
					self.expLog = append(self.expLog, &installedFile{
						name: path.Join(hostCNIBin(), path.Base(file)),
					})
				}
				self.expLog = append(self.expLog,
					&k8sFile{name: kubeConfigFilename()},
					&cniFile{name: self.expCNIConfigFile},
				)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.setup(t, &test)
			ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*16)
			defer cancel()

			err := test.mgr.Run(ctx)
			if assertErr(t, test.expErr, err) {
				return
			}
			for _, expInstalledFile := range test.expInstalledFiles {
				expInstalledData := mustReadFile(t, expInstalledFile)
				actInstalledFile := path.Join(hostCNIBin(), path.Base(expInstalledFile))
				actInstalledData := mustReadFile(t, actInstalledFile)
				if !bytes.Equal(expInstalledData, actInstalledData) {
					t.Fatalf("expected installed file does not equal actual '%s'<>'%s'",
						expInstalledFile, actInstalledFile)
				}
			}
			actK8sConfig := mustReadUnmarshal(t, kubeConfigFilename(), yaml.Unmarshal)
			assertDeepEqual(t, test.expK8sConfig, actK8sConfig)

			actCNIConfig := mustReadUnmarshal(t, path.Join(hostCNIConfig(), "10-calico.conflist"), json.Unmarshal)
			assertDeepEqual(t, test.expCNIConfig, actCNIConfig)

			// cancel the installer; run remove; check that the log matches
			// expectations; check that the install files and the k8s config
			// file are removed, and that the cni config file matches
			// original spec
			cancel()
			err = test.mgr.Remove()
			if assertErr(t, test.expRemoveErr, err) {
				return
			}
			if len(test.expLog) != len(test.mgr.log) {
				t.Fatalf("expected log size does not equal actual '%d'<>'%d'",
					len(test.expLog), len(test.mgr.log))
			}
			for i := 0; i < len(test.expLog); i++ {
				assertDeepEqual(t, test.expLog[i], test.mgr.log[i])
			}
			for _, expInstalledFile := range test.expInstalledFiles {
				actInstalledFile := path.Join(hostCNIBin(), path.Base(expInstalledFile))
				if _, err := os.Stat(actInstalledFile); !os.IsNotExist(err) {
					t.Fatalf("expected installed file was not removed")
				}
			}
			if _, err := os.Stat(kubeConfigFilename()); !os.IsNotExist(err) {
				t.Fatalf("expected k8s config file was not removed")
			}
			if _, err := os.Stat(test.expCNIConfigFile); err != nil {
				t.Fatalf("cannot stat cni config file after revert err=%v", err)
			}
			actCNIConfig = mustReadUnmarshal(t, test.expCNIConfigFile, json.Unmarshal)
			assertDeepEqual(t, test.expCNIConfigRevert, actCNIConfig)
		})
	}
}
