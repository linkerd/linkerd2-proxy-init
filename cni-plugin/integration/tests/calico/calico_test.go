package calico_test

import (
	"os"
	"testing"

	"github.com/linkerd/linkerd2-proxy-init/cni-plugin/integration/testutil"
)

const (
	ConfigDirectory = "/host/etc/cni/net.d"
	CalicoConflist  = "10-calico.conflist"
)

var runner *testutil.TestRunner

func TestMain(m *testing.M) {
	runner = testutil.NewTestRunner(ConfigDirectory, CalicoConflist)
	os.Exit(m.Run())
}

func TestLinkerdIsLastCNIPlugin(t *testing.T) {
	if err := runner.CheckCNIPluginIsLast(); err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}
}
