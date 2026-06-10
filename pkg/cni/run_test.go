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
		name              string
		expInstalledFiles []string
		expK8sConfig      map[string]any
		expCNIConfig      map[string]any
		expErr            string
		mgr               *installer
		doIO              func(*testing.T, *test)
		setup             func(*testing.T, *test)
	}
	tests := []test{
		{
			name:              "TestRun",
			expInstalledFiles: nil,
			expCNIConfig:      nil,
			expK8sConfig:      nil,
			expErr:            "",
			doIO: func(t *testing.T, self *test) {
				// write the auth token
				info, err := os.Stat(self.mgr.serviceAccountTokenFilename)
				if err != nil {
					t.Fatalf("cannot stat token file=%s err=%v",
						self.mgr.serviceAccountTokenFilename, err)
				}
				token := mustReadFile(t, "testdata/auth-token-new")
				mustWriteFile(t, self.mgr.serviceAccountTokenFilename,
					token, info.Mode())
			},
			setup: func(t *testing.T, self *test) {
				t.Helper()
				root := t.TempDir()
				mustCopyFiles(t, root, "testdata")

				self.mgr = newTestInstaller(t)
				self.mgr.serviceAccountTokenFilename = path.Join(root, "auth-token")
				self.mgr.sources = []source{
					&environmentSource{key: "TEST_CONFIGURE_FROM_ENV"},
				}

				self.expK8sConfig = mustReadUnmarshal(t, "testdata/kubeconfig-exp-env.yaml", yaml.Unmarshal)
				self.expCNIConfig = mustReadUnmarshal(t, "testdata/10-calico-exp.conflist", json.Unmarshal)

				t.Setenv(containerMountPrefix.key, root)
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
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.setup(t, &test)
			ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*8)
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
		})
	}
}
