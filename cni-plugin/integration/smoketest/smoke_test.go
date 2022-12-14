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

func TestPodShouldFail(t *testing.T) {
	t.Parallel()

	podIP := os.Getenv("POD_WITH_NO_RULES_IP")
	if podIP == "" {
		t.Skipf("POD_WITH_NO_RULES_IP is not set")
	}

	t.Run("succeeds connecting to pod directly through container's exposed port", func(t *testing.T) {
		t.Fatalf("failed so I can see it's working.")
	})
}
