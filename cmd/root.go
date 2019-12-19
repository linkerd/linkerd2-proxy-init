package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/linkerd/linkerd2-proxy-init/iptables"
	"github.com/linkerd/linkerd2-proxy-init/ports"
)

// RootOptions provides the information that will be used to build a firewall configuration.
type RootOptions struct {
	IncomingProxyPort     int
	OutgoingProxyPort     int
	ProxyUserID           int
	PortsToRedirect       []int
	InboundPortsToIgnore  []string
	OutboundPortsToIgnore []string
	SimulateOnly          bool
	NetNs                 string
	UseWaitFlag           bool
}

func newRootOptions() *RootOptions {
	return &RootOptions{
		IncomingProxyPort:     -1,
		OutgoingProxyPort:     -1,
		ProxyUserID:           -1,
		PortsToRedirect:       make([]int, 0),
		InboundPortsToIgnore:  make([]string, 0),
		OutboundPortsToIgnore: make([]string, 0),
		SimulateOnly:          false,
		NetNs:                 "",
		UseWaitFlag:           false,
	}
}

// NewRootCmd returns a configured cobra.Command for the `proxy-init` command.
// TODO: consider moving this to `/proxy-init/main.go`
func NewRootCmd() *cobra.Command {
	options := newRootOptions()

	cmd := &cobra.Command{
		Use:   "proxy-init",
		Short: "proxy-init adds a Kubernetes pod to the Linkerd service mesh",
		Long:  "proxy-init adds a Kubernetes pod to the Linkerd service mesh.",
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := BuildFirewallConfiguration(options)
			if err != nil {
				return err
			}
			return iptables.ConfigureFirewall(*config)
		},
	}

	cmd.PersistentFlags().IntVarP(&options.IncomingProxyPort, "incoming-proxy-port", "p", options.IncomingProxyPort, "Port to redirect incoming traffic")
	cmd.PersistentFlags().IntVarP(&options.OutgoingProxyPort, "outgoing-proxy-port", "o", options.OutgoingProxyPort, "Port to redirect outgoing traffic")
	cmd.PersistentFlags().IntVarP(&options.ProxyUserID, "proxy-uid", "u", options.ProxyUserID, "User ID that the proxy is running under. Any traffic coming from this user will be ignored to avoid infinite redirection loops.")
	cmd.PersistentFlags().IntSliceVarP(&options.PortsToRedirect, "ports-to-redirect", "r", options.PortsToRedirect, "Port to redirect to proxy, if no port is specified then ALL ports are redirected")
	cmd.PersistentFlags().StringSliceVar(&options.InboundPortsToIgnore, "inbound-ports-to-ignore", options.InboundPortsToIgnore, "Inbound ports and/or port ranges (inclusive) to ignore and not redirect to proxy. This has higher precedence than any other parameters.")
	cmd.PersistentFlags().StringSliceVar(&options.OutboundPortsToIgnore, "outbound-ports-to-ignore", options.OutboundPortsToIgnore, "Outbound ports and/or port ranges (inclusive) to ignore and not redirect to proxy. This has higher precedence than any other parameters.")
	cmd.PersistentFlags().BoolVar(&options.SimulateOnly, "simulate", options.SimulateOnly, "Don't execute any command, just print what would be executed")
	cmd.PersistentFlags().StringVar(&options.NetNs, "netns", options.NetNs, "Optional network namespace in which to run the iptables commands")
	cmd.PersistentFlags().BoolVarP(&options.UseWaitFlag, "use-wait-flag", "w", options.UseWaitFlag, "Appends the \"-w\" flag to the iptables commands")

	return cmd
}

// BuildFirewallConfiguration returns an iptables FirewallConfiguration suitable to use to configure iptables.
func BuildFirewallConfiguration(options *RootOptions) (*iptables.FirewallConfiguration, error) {
	if !ports.IsValid(options.IncomingProxyPort) {
		return nil, fmt.Errorf("--incoming-proxy-port must be a valid TCP port number")
	}

	if !ports.IsValid(options.OutgoingProxyPort) {
		return nil, fmt.Errorf("--outgoing-proxy-port must be a valid TCP port number")
	}

	firewallConfiguration := &iptables.FirewallConfiguration{
		ProxyInboundPort:       options.IncomingProxyPort,
		ProxyOutgoingPort:      options.OutgoingProxyPort,
		ProxyUID:               options.ProxyUserID,
		PortsToRedirectInbound: options.PortsToRedirect,
		InboundPortsToIgnore:   options.InboundPortsToIgnore,
		OutboundPortsToIgnore:  options.OutboundPortsToIgnore,
		SimulateOnly:           options.SimulateOnly,
		NetNs:                  options.NetNs,
		UseWaitFlag:            options.UseWaitFlag,
	}

	if len(options.PortsToRedirect) > 0 {
		firewallConfiguration.Mode = iptables.RedirectListedMode
	} else {
		firewallConfiguration.Mode = iptables.RedirectAllMode
	}

	return firewallConfiguration, nil
}
