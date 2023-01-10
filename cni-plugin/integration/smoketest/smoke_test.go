package smoketest

import (
	"flag"
	"fmt"
	"os"
	"testing"
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

func TestPodShouldSucced(t *testing.T) {
	t.Parallel()

	t.Run("success in all in your mind", func(t *testing.T) {
		fmt.Println("we did it!")
	})
}

func TestPodShouldSkip(t *testing.T) {
	t.Parallel()

	t.Run("succeeds connecting to pod directly through container's exposed port", func(t *testing.T) {
		t.Skip("skipping because it's not ready yet.")
	})
}

func TestCanReadConfigFiles(t *testing.T) {
	t.Parallel()

	directory := "/var/lib/rancher/k3s/agent/etc/cni/net.d"

	t.Run("succeeds when we are able to read the linkerd-cni config file", func(t *testing.T) {
		filenames := ls(directory, t)
		if len(filenames) == 0 {
			t.Fatalf("no files found in %s", directory)
		}
		if !contains(filenames, "ZZZ-linkerd-cni-kubeconfig") {
			t.Fatalf("files do not contain ZZZ-linkerd-cni-kubeconfig, instead they contain: %s", filenames)
		}

	})
}
