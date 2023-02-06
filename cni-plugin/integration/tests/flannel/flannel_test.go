package flannel

import (
	"os"
	"testing"

	"github.com/linkerd/linkerd2-proxy-init/cni-plugin/integration/testutil"
)

const (
	ConfigDirectory = "/var/lib/rancher/k3s/agent/etc/cni/net.d"
	FlannelConflist = "10-flannel.conflist"
)

var runner *testutil.TestRunner

func TestMain(m *testing.M) {
	runner = testutil.NewTestRunner(ConfigDirectory, FlannelConflist)
	os.Exit(m.Run())
}

func TestLinkerdIsLastCNIPlugin(t *testing.T) {
	if err := runner.CheckCNIPluginIsLast(); err != nil {
		t.Fatalf("Unexpected error: %e", err)
	}
}
