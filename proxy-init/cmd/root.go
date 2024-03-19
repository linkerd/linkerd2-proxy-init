package cmd

import (
	"fmt"
	"net"
	"os/exec"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/linkerd/linkerd2-proxy-init/internal/iptables"
	"github.com/linkerd/linkerd2-proxy-init/internal/util"
)

const (
	// IPTablesModeLegacy signals the usage of the iptables-legacy commands
	IPTablesModeLegacy = "legacy"
	// IPTablesModeNFT signals the usage of the iptables-nft commands
	IPTablesModeNFT = "nft"

	cmdLegacy         = "iptables-legacy"
	cmdLegacySave     = "iptables-legacy-save"
	cmdLegacyIPv6     = "ip6tables-legacy"
	cmdLegacyIPv6Save = "ip6tables-legacy-save"
	cmdNFT            = "iptables-nft"
	cmdNFTSave        = "iptables-nft-save"
	cmdNFTIPv6        = "ip6tables-nft"
	cmdNFTIPv6Save    = "ip6tables-nft-save"
)

// RootOptions provides the information that will be used to build a firewall configuration.
type RootOptions struct {
	IncomingProxyPort     int
	OutgoingProxyPort     int
	ProxyUserID           int
	PortsToRedirect       []int
	InboundPortsToIgnore  []string
	OutboundPortsToIgnore []string
	SubnetsToIgnore       []string
	SimulateOnly          bool
	NetNs                 string
	UseWaitFlag           bool
	TimeoutCloseWaitSecs  int
	LogFormat             string
	LogLevel              string
	FirewallBinPath       string
	FirewallSaveBinPath   string
	IPTablesMode          string
	IPv6                  bool
}

func newRootOptions() *RootOptions {
	return &RootOptions{
		IncomingProxyPort:     -1,
		OutgoingProxyPort:     -1,
		ProxyUserID:           -1,
		PortsToRedirect:       make([]int, 0),
		InboundPortsToIgnore:  make([]string, 0),
		OutboundPortsToIgnore: make([]string, 0),
		SubnetsToIgnore:       make([]string, 0),
		SimulateOnly:          false,
		NetNs:                 "",
		UseWaitFlag:           false,
		TimeoutCloseWaitSecs:  0,
		LogFormat:             "plain",
		LogLevel:              "info",
		FirewallBinPath:       "",
		FirewallSaveBinPath:   "",
		IPTablesMode:          "",
		IPv6:                  true,
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
		RunE: func(_ *cobra.Command, _ []string) error {

			if options.TimeoutCloseWaitSecs != 0 {
				sysctl := exec.Command("sysctl", "-w",
					fmt.Sprintf("net.netfilter.nf_conntrack_tcp_timeout_close_wait=%d", options.TimeoutCloseWaitSecs),
				)
				out, err := sysctl.CombinedOutput()
				if err != nil {
					log.Error(string(out))
					return err
				}
				log.Info(string(out))
			}

			log.SetFormatter(getFormatter(options.LogFormat))
			err := setLogLevel(options.LogLevel)
			if err != nil {
				return err
			}

			// always trigger the IPv4 rules
			optIPv4 := *options
			optIPv4.IPv6 = false
			config, err := BuildFirewallConfiguration(&optIPv4)
			if err != nil {
				return err
			}

			if err = iptables.ConfigureFirewall(*config); err != nil {
				return err
			}

			if !options.IPv6 {
				return nil
			}

			// trigger the IPv6 rules
			config, err = BuildFirewallConfiguration(options)
			if err != nil {
				return err
			}

			// We couldn't find a robust way of checking IPv6 support besides trying to just call ip6tables-save.
			// If IPv4 rules worked but not IPv6, let's not fail the container (the actual problem will get logged).
			_ = iptables.ConfigureFirewall(*config)

			return nil
		},
	}

	cmd.PersistentFlags().IntVarP(&options.IncomingProxyPort, "incoming-proxy-port", "p", options.IncomingProxyPort, "Port to redirect incoming traffic")
	cmd.PersistentFlags().IntVarP(&options.OutgoingProxyPort, "outgoing-proxy-port", "o", options.OutgoingProxyPort, "Port to redirect outgoing traffic")
	cmd.PersistentFlags().IntVarP(&options.ProxyUserID, "proxy-uid", "u", options.ProxyUserID, "User ID that the proxy is running under. Any traffic coming from this user will be ignored to avoid infinite redirection loops.")
	cmd.PersistentFlags().IntSliceVarP(&options.PortsToRedirect, "ports-to-redirect", "r", options.PortsToRedirect, "Port to redirect to proxy, if no port is specified then ALL ports are redirected")
	cmd.PersistentFlags().StringSliceVar(&options.InboundPortsToIgnore, "inbound-ports-to-ignore", options.InboundPortsToIgnore, "Inbound ports and/or port ranges (inclusive) to ignore and not redirect to proxy. This has higher precedence than any other parameters.")
	cmd.PersistentFlags().StringSliceVar(&options.OutboundPortsToIgnore, "outbound-ports-to-ignore", options.OutboundPortsToIgnore, "Outbound ports and/or port ranges (inclusive) to ignore and not redirect to proxy. This has higher precedence than any other parameters.")
	cmd.PersistentFlags().StringSliceVar(&options.SubnetsToIgnore, "subnets-to-ignore", options.SubnetsToIgnore, "Subnets to ignore and not redirect to proxy. This has higher precedence than any other parameters.")
	cmd.PersistentFlags().BoolVar(&options.SimulateOnly, "simulate", options.SimulateOnly, "Don't execute any command, just print what would be executed")
	cmd.PersistentFlags().StringVar(&options.NetNs, "netns", options.NetNs, "Optional network namespace in which to run the iptables commands")
	cmd.PersistentFlags().BoolVarP(&options.UseWaitFlag, "use-wait-flag", "w", options.UseWaitFlag, "Appends the \"-w\" flag to the iptables commands")
	cmd.PersistentFlags().IntVar(&options.TimeoutCloseWaitSecs, "timeout-close-wait-secs", options.TimeoutCloseWaitSecs, "Sets nf_conntrack_tcp_timeout_close_wait")
	cmd.PersistentFlags().StringVar(&options.LogFormat, "log-format", options.LogFormat, "Configure log format ('plain' or 'json')")
	cmd.PersistentFlags().StringVar(&options.LogLevel, "log-level", options.LogLevel, "Configure log level")
	cmd.PersistentFlags().StringVar(&options.IPTablesMode, "iptables-mode", options.IPTablesMode, "Variant of iptables command to use (\"legacy\" or \"nft\"); overrides --firewall-bin-path and --firewall-save-bin-path")
	cmd.PersistentFlags().BoolVar(&options.IPv6, "ipv6", options.IPv6, "Set rules both via iptables and ip6tables to support dual-stack networking")

	// these two flags are kept for backwards-compatibility, but --iptables-mode is preferred
	cmd.PersistentFlags().StringVar(&options.FirewallBinPath, "firewall-bin-path", options.FirewallBinPath, "Path to iptables binary")
	cmd.PersistentFlags().StringVar(&options.FirewallSaveBinPath, "firewall-save-bin-path", options.FirewallSaveBinPath, "Path to iptables-save binary")
	return cmd
}

// BuildFirewallConfiguration returns an iptables FirewallConfiguration suitable to use to configure iptables.
func BuildFirewallConfiguration(options *RootOptions) (*iptables.FirewallConfiguration, error) {
	if options.IPTablesMode != "" && options.IPTablesMode != IPTablesModeLegacy && options.IPTablesMode != IPTablesModeNFT {
		return nil, fmt.Errorf("--iptables-mode valid values are only \"%s\" and \"%s\"", IPTablesModeLegacy, IPTablesModeNFT)
	}

	if options.IPTablesMode == "" {
		switch options.FirewallBinPath {
		case "", cmdLegacy:
			options.IPTablesMode = IPTablesModeLegacy
		case cmdNFT:
			options.IPTablesMode = IPTablesModeNFT
		default:
			return nil, fmt.Errorf("--firewall-bin-path valid values are only \"%s\" and \"%s\"", cmdLegacy, cmdNFT)
		}
	}

	if !util.IsValidPort(options.IncomingProxyPort) {
		return nil, fmt.Errorf("--incoming-proxy-port must be a valid TCP port number")
	}

	if !util.IsValidPort(options.OutgoingProxyPort) {
		return nil, fmt.Errorf("--outgoing-proxy-port must be a valid TCP port number")
	}

	cmd, cmdSave := getCommands(options)

	sanitizedSubnets := []string{}
	for _, subnet := range options.SubnetsToIgnore {
		subnet := strings.TrimSpace(subnet)
		_, _, err := net.ParseCIDR(subnet)
		if err != nil {
			return nil, fmt.Errorf("%s is not a valid CIDR address", subnet)
		}

		sanitizedSubnets = append(sanitizedSubnets, subnet)
	}

	firewallConfiguration := &iptables.FirewallConfiguration{
		ProxyInboundPort:       options.IncomingProxyPort,
		ProxyOutgoingPort:      options.OutgoingProxyPort,
		ProxyUID:               options.ProxyUserID,
		PortsToRedirectInbound: options.PortsToRedirect,
		InboundPortsToIgnore:   options.InboundPortsToIgnore,
		OutboundPortsToIgnore:  options.OutboundPortsToIgnore,
		SubnetsToIgnore:        sanitizedSubnets,
		SimulateOnly:           options.SimulateOnly,
		NetNs:                  options.NetNs,
		UseWaitFlag:            options.UseWaitFlag,
		BinPath:                cmd,
		SaveBinPath:            cmdSave,
	}

	if len(options.PortsToRedirect) > 0 {
		firewallConfiguration.Mode = iptables.RedirectListedMode
	} else {
		firewallConfiguration.Mode = iptables.RedirectAllMode
	}

	return firewallConfiguration, nil
}

func getFormatter(format string) log.Formatter {
	switch format {
	case "json":
		return &log.JSONFormatter{}
	default:
		return &log.TextFormatter{FullTimestamp: true}
	}
}

func getCommands(options *RootOptions) (string, string) {
	if options.IPTablesMode == IPTablesModeLegacy {
		if options.IPv6 {
			return cmdLegacyIPv6, cmdLegacyIPv6Save
		}
		return cmdLegacy, cmdLegacySave
	}

	if options.IPv6 {
		return cmdNFTIPv6, cmdNFTIPv6Save
	}

	return cmdNFT, cmdNFTSave
}

func setLogLevel(logLevel string) error {
	level, err := log.ParseLevel(logLevel)
	if err != nil {
		return err
	}
	log.SetLevel(level)
	return nil
}
