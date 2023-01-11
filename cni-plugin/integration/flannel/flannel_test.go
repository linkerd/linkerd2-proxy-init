package flannel

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"testing"
)

const (
	ConfigDirectory = "/var/lib/rancher/k3s/agent/etc/cni/net.d"
	FlannelConflist = "10-flannel.conflist"
)

func ls(dir string, t *testing.T) []string {
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to list files: %v", err)
	}
	fileNames := make([]string, len(files))
	for i, f := range files {
		fileNames[i] = f.Name()
	}
	return fileNames
}

func contains(s []string, str string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}

	return false
}

func TestMain(m *testing.M) {
	runTests := flag.Bool("integration-tests", false, "must be provided to run the integration tests")
	flag.Parse()

	if !*runTests {
		fmt.Fprintln(os.Stderr, "integration tests not enabled: enable with -integration-tests")
		os.Exit(0)
	}

	os.Exit(m.Run())
}

func TestLinkerdCNIIsLastPlugin(t *testing.T) {
	t.Parallel()

	t.Run("succeeds when linkerd-cni is the last plugin", func(t *testing.T) {
		if _, err := os.Stat(ConfigDirectory); os.IsNotExist(err) {
			t.Fatalf("Directory does not exist. Check if volume mount exists: %s", ConfigDirectory)
		}

		filenames := ls(ConfigDirectory, t)

		if len(filenames) == 0 {
			t.Fatalf("no files found in %s", ConfigDirectory)
		}

		if len(filenames) > 2 {
			t.Fatalf("too many files found in %s: %s ", ConfigDirectory, filenames)
		}

		if !contains(filenames, FlannelConflist) {
			t.Fatalf("files do not contain %s, instead they contain: %s", FlannelConflist, filenames)
		}

		conflistFile, err := os.ReadFile(ConfigDirectory + "/" + FlannelConflist)
		if err != nil {
			t.Fatalf("could not read %s: %e", FlannelConflist, err)
		}

		var conflist map[string]any
		err = json.Unmarshal(conflistFile, &conflist)
		if err != nil {
			t.Fatalf("unmarshaling json failed: %e", err)
		}

		if conflist["cniVersion"] != "1.0.0" {
			t.Fatalf("expected cniVersion 1.0.0, instead saw %s", conflistFile)
		}

		plugins := conflist["plugins"].([]interface{})
		lastPlugin := plugins[len(plugins)-1].(map[string]any)
		if lastPlugin["name"] != "linkerd-cni" {
			t.Fatalf("linkerd-cni was not last in the plugins list")
		}
	})
}
