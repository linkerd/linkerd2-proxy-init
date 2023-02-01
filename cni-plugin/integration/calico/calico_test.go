package calico_test

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"testing"
)

const (
	ConfigDirectory = "/etc/cni/net.d"
	CalicoConflist  = "10-calico.conflist"
)

// Given a directory, return a map of filename->struct{}
func files(directory string) (map[string]struct{}, error) {
	files, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}

	fileNames := make(map[string]struct{}, len(files))
	for _, f := range files {
		fileNames[f.Name()] = struct{}{}
	}

	return fileNames, nil
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

func TestLinkerdIsLastCNIPlugin(t *testing.T) {
	t.Parallel()

	t.Run("succeeds when linkerd-cni is the last plugin", func(t *testing.T) {
		if _, err := os.Stat(ConfigDirectory); os.IsNotExist(err) {
			t.Fatalf("Directory does not exist. Check if volume mount exists: %s", ConfigDirectory)
		}

		filenames, err := files(ConfigDirectory)

		if err != nil {
			t.Fatalf("unable to read files from directory %s due to error: %e", ConfigDirectory, err)
		}

		if len(filenames) == 0 {
			t.Fatalf("no files found in %s", ConfigDirectory)
		}

		if len(filenames) > 2 {
			t.Fatalf("too many files found in %s: %s ", ConfigDirectory, filenames)
		}

		if _, ok := filenames[CalicoConflist]; !ok {
			t.Fatalf("filenames does not contain %s, instead it contains: %s", CalicoConflist, filenames)
		}

		conflistFile, err := os.ReadFile(ConfigDirectory + "/" + CalicoConflist)
		if err != nil {
			t.Fatalf("could not read %s: %e", CalicoConflist, err)
		}

		var conflist map[string]any
		err = json.Unmarshal(conflistFile, &conflist)
		if err != nil {
			t.Fatalf("unmarshaling conflist json failed: %e", err)
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
