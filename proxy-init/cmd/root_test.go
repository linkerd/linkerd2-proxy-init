package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/linkerd/linkerd2-proxy-init/pkg/iptables"
)

func TestBuildFirewallConfiguration(t *testing.T) {
	t.Run("It produces a FirewallConfiguration for the default config", func(t *testing.T) {
		expectedIncomingProxyPort := 1234
		expectedOutgoingProxyPort := 2345
		expectedProxyUserID := 33
		expectedProxyGroupID := 33
		expectedConfig := &iptables.FirewallConfiguration{
			Mode:                   iptables.RedirectAllMode,
			PortsToRedirectInbound: make([]int, 0),
			InboundPortsToIgnore:   make([]string, 0),
			OutboundPortsToIgnore:  make([]string, 0),
			SubnetsToIgnore:        make([]string, 0),
			ProxyInboundPort:       expectedIncomingProxyPort,
			ProxyOutgoingPort:      expectedOutgoingProxyPort,
			ProxyUID:               expectedProxyUserID,
			ProxyGID:               expectedProxyGroupID,
			SimulateOnly:           false,
			UseWaitFlag:            false,
			BinPath:                "iptables-legacy",
			SaveBinPath:            "iptables-legacy-save",
		}

		options := newRootOptions()
		options.IncomingProxyPort = expectedIncomingProxyPort
		options.OutgoingProxyPort = expectedOutgoingProxyPort
		options.ProxyUserID = expectedProxyUserID
		options.IPv6 = false
		options.ProxyGroupID = expectedProxyGroupID

		config, err := BuildFirewallConfiguration(options)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}

		if !reflect.DeepEqual(config, expectedConfig) {
			t.Fatalf("Expected config \n[%+v]\n but got \n[%+v]", expectedConfig, config)
		}
	})

	t.Run("It rejects invalid config options", func(t *testing.T) {
		for _, tt := range []struct {
			options      *RootOptions
			errorMessage string
		}{
			{
				options: &RootOptions{
					IncomingProxyPort: -1,
					OutgoingProxyPort: 1234,
					IPTablesMode:      IPTablesModeLegacy,
				},
				errorMessage: "--incoming-proxy-port must be a valid TCP port number",
			},
			{
				options: &RootOptions{
					IncomingProxyPort: 100000,
					OutgoingProxyPort: 1234,
					IPTablesMode:      IPTablesModeLegacy,
				},
				errorMessage: "--incoming-proxy-port must be a valid TCP port number",
			},
			{
				options: &RootOptions{
					IncomingProxyPort: 1234,
					OutgoingProxyPort: -1,
					IPTablesMode:      IPTablesModeLegacy,
				},
				errorMessage: "--outgoing-proxy-port must be a valid TCP port number",
			},
			{
				options: &RootOptions{
					IncomingProxyPort: 1234,
					OutgoingProxyPort: 100000,
					IPTablesMode:      IPTablesModeLegacy,
				},
				errorMessage: "--outgoing-proxy-port must be a valid TCP port number",
			},
			{
				options: &RootOptions{
					SubnetsToIgnore: []string{"1.1.1.1/24", "0.0.0.0"},
					IPTablesMode:    IPTablesModeLegacy,
				},
				errorMessage: "0.0.0.0 is not a valid CIDR address",
			},
		} {
			_, err := BuildFirewallConfiguration(tt.options)
			if err == nil {
				t.Fatalf("Expected error for config [%v], got nil", tt.options)
			}
			if err.Error() != tt.errorMessage {
				t.Fatalf("Expected error [%s] for config [%v], got [%s]",
					tt.errorMessage, tt.options, err.Error())
			}
		}
	})

	t.Run("Overrides handled properly", func(t *testing.T) {
		for _, tt := range []struct {
			options      *RootOptions
			errorMessage string
		}{
			{
				// Tests that subnets are parsed properly and trimmed of excess whitespace
				options: &RootOptions{
					SubnetsToIgnore: []string{"1.1.1.1/24 "},
					IPTablesMode:    IPTablesModeLegacy,
				},
				errorMessage: "",
			},
		} {
			_, err := BuildFirewallConfiguration(tt.options)
			if err != nil {
				t.Fatalf("Got error error for config [%v]", tt.options)
			}
		}
	})
}

// TestSetTimeoutCloseWait checks the close-wait timeout is written as a
// decimal string to the given path. It targets a temp file in place of the
// procfs entry, and catches a wrong encoding or a write that never lands.
func TestSetTimeoutCloseWait(t *testing.T) {
	// Point the helper at a fresh file standing in for procfs.
	path := filepath.Join(t.TempDir(), "nf_conntrack_tcp_timeout_close_wait")

	// Write the timeout; procfs expects the bare number.
	if err := setTimeoutCloseWait(path, 3600); err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}

	// Read it back to confirm the exact bytes written.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Unexpected error: %s", err)
	}
	if string(got) != "3600" {
		t.Fatalf("Expected file contents [3600] but got [%s]", got)
	}
}

// TestSetTimeoutCloseWaitMissingDir checks a write to a nonexistent directory
// returns an error. It targets a path under a missing directory, as on a node
// without nf_conntrack loaded, and catches the failure being swallowed.
func TestSetTimeoutCloseWaitMissingDir(t *testing.T) {
	// Build a path whose parent directory does not exist.
	path := filepath.Join(t.TempDir(), "missing", "nf_conntrack_tcp_timeout_close_wait")

	// The write must fail so proxy-init exits instead of silently skipping.
	if err := setTimeoutCloseWait(path, 3600); err == nil {
		t.Fatal("Expected an error for a missing directory but got nil")
	}
}
