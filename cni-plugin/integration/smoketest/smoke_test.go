package smoketest

import (
	"flag"
	"fmt"
	"os"
	"testing"
)

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
