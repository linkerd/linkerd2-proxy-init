package cilium

import (
	"os"
	"testing"

	"github.com/linkerd/linkerd2-proxy-init/cni-plugin/integration/testutil"
)

const (
	// ConfigDirectory = "/var/lib/rancher/k3s/agent/etc/cni/net.d"
	ConfigDirectory = "/host/etc/cni/net.d"
	CiliumConflist  = "05-cilium.conflist"
)

var runner *testutil.TestRunner

func TestMain(m *testing.M) {
	runner = testutil.NewTestRunner(ConfigDirectory, CiliumConflist)
	os.Exit(m.Run())
}

func TestLinkerdIsLastCNIPlugin(t *testing.T) {
	if err := runner.CheckCNIPluginIsLast(); err != nil {
		t.Fatalf("Unexpected error: %e", err)
	}
}
