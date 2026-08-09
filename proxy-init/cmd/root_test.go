package cmd

import (
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

	t.Run("It filters subnets by address family for dual-stack", func(t *testing.T) {
		mixedSubnets := []string{"172.16.0.0/12", "fd00::/8", "10.0.0.0/8", "2001:db8::/32"}

		// IPv4 pass (IPv6=false) should only include IPv4 subnets
		optIPv4 := &RootOptions{
			IncomingProxyPort: 1234,
			OutgoingProxyPort: 2345,
			SubnetsToIgnore:   mixedSubnets,
			IPTablesMode:      IPTablesModeLegacy,
			IPv6:              false,
		}
		config, err := BuildFirewallConfiguration(optIPv4)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
		expectedIPv4 := []string{"172.16.0.0/12", "10.0.0.0/8"}
		if !reflect.DeepEqual(config.SubnetsToIgnore, expectedIPv4) {
			t.Fatalf("Expected IPv4 subnets %v but got %v", expectedIPv4, config.SubnetsToIgnore)
		}

		// IPv6 pass (IPv6=true) should only include IPv6 subnets
		optIPv6 := &RootOptions{
			IncomingProxyPort: 1234,
			OutgoingProxyPort: 2345,
			SubnetsToIgnore:   mixedSubnets,
			IPTablesMode:      IPTablesModeLegacy,
			IPv6:              true,
		}
		config, err = BuildFirewallConfiguration(optIPv6)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
		expectedIPv6 := []string{"fd00::/8", "2001:db8::/32"}
		if !reflect.DeepEqual(config.SubnetsToIgnore, expectedIPv6) {
			t.Fatalf("Expected IPv6 subnets %v but got %v", expectedIPv6, config.SubnetsToIgnore)
		}
	})

	t.Run("It handles IPv4-only subnets with dual-stack enabled", func(t *testing.T) {
		ipv4Only := []string{"172.16.0.0/12"}

		// IPv6 pass should produce empty subnets, not error
		opt := &RootOptions{
			IncomingProxyPort: 1234,
			OutgoingProxyPort: 2345,
			SubnetsToIgnore:   ipv4Only,
			IPTablesMode:      IPTablesModeLegacy,
			IPv6:              true,
		}
		config, err := BuildFirewallConfiguration(opt)
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
		if len(config.SubnetsToIgnore) != 0 {
			t.Fatalf("Expected empty subnets for IPv6 pass with IPv4-only input, got %v", config.SubnetsToIgnore)
		}
	})
}
