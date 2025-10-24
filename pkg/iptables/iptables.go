package iptables

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	util "github.com/linkerd/linkerd2-proxy-init/pkg/util"
)

const (
	// RedirectAllMode indicates redirecting all ports.
	RedirectAllMode = "redirect-all"

	// RedirectListedMode indicates redirecting a given list of ports.
	RedirectListedMode = "redirect-listed"

	// IptablesPreroutingChainName specifies an iptables `PREROUTING` chain,
	// responsible for packets that just arrived at the network interface.
	IptablesPreroutingChainName = "PREROUTING"

	// IptablesOutputChainName specifies an iptables `OUTPUT` chain.
	IptablesOutputChainName = "OUTPUT"

	// IptablesMultiportLimit specifies the maximum number of port references per single iptables command.
	IptablesMultiportLimit = 15
	outputChainName        = "PROXY_INIT_OUTPUT"
	redirectChainName      = "PROXY_INIT_REDIRECT"
)

var (
	// ExecutionTraceID provides a unique identifier for this script's execution.
	executionTraceID = strconv.Itoa(int(time.Now().Unix()))

	preroutingRuleRegex = regexp.MustCompile(`(?m)^-A PREROUTING (.+ )?-j PROXY_INIT_REDIRECT`)
	outputRuleRegex     = regexp.MustCompile(`(?m)^-A OUTPUT (.+ )?-j PROXY_INIT_OUTPUT`)
	redirectChainRegex  = regexp.MustCompile(`(?m)^:PROXY_INIT_REDIRECT `)
	outputChainRegex    = regexp.MustCompile(`(?m)^:PROXY_INIT_OUTPUT `)
)

// FirewallConfiguration specifies how to configure iptables.
type FirewallConfiguration struct {
	Mode                   string
	PortsToRedirectInbound []int
	InboundPortsToIgnore   []string
	OutboundPortsToIgnore  []string
	SubnetsToIgnore        []string
	ProxyInboundPort       int
	ProxyOutgoingPort      int
	ProxyUID               int
	ProxyGID               int
	SimulateOnly           bool
	NetNs                  string
	UseWaitFlag            bool
	BinPath                string
	SaveBinPath            string
	ContinueOnError        bool
}

// ConfigureFirewall configures iptables to redirect all desired traffic through the proxy, allowing for
// the workload to join the service mesh. A lot of this logic was based on
// https://github.com/istio/istio/blob/e83411e/pilot/docker/prepare_proxy.sh
func ConfigureFirewall(firewallConfiguration FirewallConfiguration) error {
	log.Debugf("tracing script execution as [%s]", executionTraceID)

	// Before executing, ensure the configured iptables binaries exist; if not, attempt a fallback.
	resolveBinFallback(&firewallConfiguration)

	existingRules, err := executeCommand(firewallConfiguration, firewallConfiguration.makeShowAllRules())
	if err != nil {
		log.Error("aborting firewall configuration")
		return err
	}

	commands := make([]*exec.Cmd, 0)

	commands = firewallConfiguration.addIncomingTrafficRules(existingRules, commands)

	commands = firewallConfiguration.addOutgoingTrafficRules(existingRules, commands)

	if firewallConfiguration.UseWaitFlag {
		log.Debug("'useWaitFlag' set: iptables will wait for xtables to become available")
	}

	for _, cmd := range commands {
		if firewallConfiguration.UseWaitFlag {
			cmd.Args = append(cmd.Args, "-w")
		}

		if _, err := executeCommand(firewallConfiguration, cmd); err != nil {
			if !firewallConfiguration.ContinueOnError {
				return err
			}

			log.Debugf("continuing despite error: %s", err)
		}
	}

	_, _ = executeCommand(firewallConfiguration, firewallConfiguration.makeShowAllRules())

	return nil
}

// CleanupFirewallConfig removes the iptables rules that have been added as a result of
// calling ConfigureFirewall.
func CleanupFirewallConfig(firewallConfiguration FirewallConfiguration) error {
	log.Debugf("tracing script execution as [%s]", executionTraceID)
	log.Debugf("using '%s' to clean-up firewall rules", firewallConfiguration.BinPath)
	log.Debugf("using '%s' to list all available rules", firewallConfiguration.SaveBinPath)

	// Ensure binaries exist before attempting cleanup as well
	resolveBinFallback(&firewallConfiguration)

	commands := make([]*exec.Cmd, 0)
	commands = firewallConfiguration.cleanupRules(commands)

	if firewallConfiguration.UseWaitFlag {
		log.Debug("'useWaitFlag' set: iptables will wait for xtables to become available")
	}

	for _, cmd := range commands {
		if firewallConfiguration.UseWaitFlag {
			cmd.Args = append(cmd.Args, "-w")
		}

		if _, err := executeCommand(firewallConfiguration, cmd); err != nil {
			if !firewallConfiguration.ContinueOnError {
				return err
			}

			log.Debugf("continuing despite error: %s", err)
		}
	}

	_, _ = executeCommand(firewallConfiguration, firewallConfiguration.makeShowAllRules())

	return nil
}

func (fc FirewallConfiguration) cleanupRules(commands []*exec.Cmd) []*exec.Cmd {
	// delete ref from prerouting
	commands = append(
		commands,
		fc.makeJumpFromChainToAnotherForAllProtocols(
			IptablesPreroutingChainName,
			redirectChainName,
			"install-proxy-init-prerouting",
			true))

	// delete ref from output
	commands = append(
		commands,
		fc.makeJumpFromChainToAnotherForAllProtocols(
			IptablesOutputChainName,
			outputChainName,
			"install-proxy-init-output",
			true))

	// flush chains
	commands = append(commands, fc.makeFlushChain(outputChainName))
	commands = append(commands, fc.makeFlushChain(redirectChainName))

	// delete chains
	commands = append(commands, fc.makeDeleteChain(outputChainName))
	commands = append(commands, fc.makeDeleteChain(redirectChainName))

	return commands
}

// formatComment is used to format iptables comments
func formatComment(text string) string {
	return fmt.Sprintf("proxy-init/%s", text)
}

func (fc FirewallConfiguration) addOutgoingTrafficRules(existingRules []byte, commands []*exec.Cmd) []*exec.Cmd {
	if outputChainRegex.Find(existingRules) == nil {
		commands = append(commands, fc.makeCreateNewChain(outputChainName))
	} else {
		commands = append(commands, fc.makeFlushChain(outputChainName))
	}

	// Ignore traffic from the proxy
	if fc.ProxyUID > 0 {
		commands = append(commands, fc.makeIgnoreUserID(outputChainName, fc.ProxyUID, "ignore-proxy-user-id"))
	}

	// Ignore traffic from the proxy
	if fc.ProxyGID > 0 {
		commands = append(commands, fc.makeIgnoreGroupID(outputChainName, fc.ProxyGID, "ignore-proxy-group-id"))
	}

	// Ignore loopback
	commands = append(commands, fc.makeIgnoreLoopback(outputChainName, "ignore-loopback"))
	// Ignore ports
	commands = fc.addRulesForIgnoredPorts(fc.OutboundPortsToIgnore, outputChainName, commands)

	commands = append(commands, fc.makeRedirectChainToPort(outputChainName, fc.ProxyOutgoingPort, "redirect-all-outgoing-to-proxy-port"))

	if outputRuleRegex.Find(existingRules) == nil {
		// Redirect all remaining outbound traffic to the proxy.
		commands = append(
			commands,
			fc.makeJumpFromChainToAnotherForAllProtocols(
				IptablesOutputChainName,
				outputChainName,
				"install-proxy-init-output",
				false))
	}

	return commands
}

func (fc FirewallConfiguration) addIncomingTrafficRules(existingRules []byte, commands []*exec.Cmd) []*exec.Cmd {
	if redirectChainRegex.Find(existingRules) == nil {
		commands = append(commands, fc.makeCreateNewChain(redirectChainName))
	} else {
		commands = append(commands, fc.makeFlushChain(redirectChainName))
	}
	commands = fc.addRulesForIgnoredPorts(fc.InboundPortsToIgnore, redirectChainName, commands)
	commands = fc.addRulesForIgnoredSubnets(redirectChainName, commands)
	commands = fc.addRulesForInboundPortRedirect(redirectChainName, commands)

	if preroutingRuleRegex.Find(existingRules) == nil {
		// Redirect all remaining inbound traffic to the proxy.
		commands = append(
			commands,
			fc.makeJumpFromChainToAnotherForAllProtocols(
				IptablesPreroutingChainName,
				redirectChainName,
				"install-proxy-init-prerouting",
				false))
	}

	return commands
}

func (fc FirewallConfiguration) addRulesForInboundPortRedirect(chainName string, commands []*exec.Cmd) []*exec.Cmd {
	if fc.Mode == RedirectAllMode {
		// Create a new chain for redirecting inbound and outbound traffic to the proxy port.
		commands = append(commands, fc.makeRedirectChainToPort(
			chainName,
			fc.ProxyInboundPort,
			"redirect-all-incoming-to-proxy-port"))

	} else if fc.Mode == RedirectListedMode {
		for _, port := range fc.PortsToRedirectInbound {
			commands = append(
				commands,
				fc.makeRedirectChainToPortBasedOnDestinationPort(
					chainName,
					port,
					fc.ProxyInboundPort,
					fmt.Sprintf("redirect-port-%d-to-proxy-port", port)))
		}
	}
	return commands
}

func (fc FirewallConfiguration) addRulesForIgnoredPorts(portsToIgnore []string, chainName string, commands []*exec.Cmd) []*exec.Cmd {
	for _, destinations := range makeMultiportDestinations(portsToIgnore) {
		commands = append(commands, fc.makeIgnorePorts(chainName, destinations, fmt.Sprintf("ignore-port-%s", strings.Join(destinations, ","))))
	}
	return commands
}

func (fc FirewallConfiguration) addRulesForIgnoredSubnets(chainName string, commands []*exec.Cmd) []*exec.Cmd {
	for _, subnet := range fc.SubnetsToIgnore {
		commands = append(commands, fc.makeIgnoreSubnet(chainName, subnet, fmt.Sprintf("ignore-subnet-%s", subnet)))
	}
	return commands
}

func makeMultiportDestinations(portsToIgnore []string) [][]string {
	destinationSlices := make([][]string, 0)
	destinationPortCount := 0
	if portsToIgnore == nil || len(portsToIgnore) < 1 {
		return destinationSlices
	}
	destinations := make([]string, 0)
	for _, portOrRange := range portsToIgnore {
		if portRange, err := util.ParsePortRange(portOrRange); err == nil {
			// The number of ports referenced for the range
			portCount := 2
			if portRange.LowerBound == portRange.UpperBound {
				// We'll condense for single port ranges
				portCount = 1
			}
			// Check port capacity for the current command
			if destinationPortCount+portCount > IptablesMultiportLimit {
				destinationSlices = append(destinationSlices, destinations)
				destinationPortCount = 0
				destinations = make([]string, 0)
			}
			destinations = append(destinations, asDestination(portRange))
			destinationPortCount += portCount
		} else {
			log.Errorf("invalid port configuration of \"%s\": %s", portOrRange, err.Error())
		}
	}
	return append(destinationSlices, destinations)
}

func executeCommand(firewallConfiguration FirewallConfiguration, cmd *exec.Cmd) ([]byte, error) {
	if firewallConfiguration.NetNs != "" {
		// BusyBox's `nsenter` needs `--` to separate nsenter arguments from the
		// command.
		//
		// See https://github.com/rancher/k3s/issues/1434#issuecomment-629315909
		nsArgs := fmt.Sprintf("--net=%s", firewallConfiguration.NetNs)
		args := append([]string{nsArgs, "--"}, cmd.Args...)
		cmd = exec.Command("nsenter", args...)
	}
	log.Info(cmd.String())

	if firewallConfiguration.SimulateOnly {
		return nil, nil
	}

	out, err := cmd.CombinedOutput()

	if len(out) > 0 {
		log.Infof("%s", out)
	}

	return out, err
}

func (fc FirewallConfiguration) makeIgnoreUserID(chainName string, uid int, comment string) *exec.Cmd {
	return exec.Command(fc.BinPath,
		"-t", "nat",
		"-A", chainName,
		"-m", "owner",
		"--uid-owner", strconv.Itoa(uid),
		"-j", "RETURN",
		"-m", "comment",
		"--comment", formatComment(comment))
}

func (fc FirewallConfiguration) makeIgnoreGroupID(chainName string, gid int, comment string) *exec.Cmd {
	return exec.Command(fc.BinPath,
		"-t", "nat",
		"-A", chainName,
		"-m", "owner",
		"--gid-owner", strconv.Itoa(gid),
		"-j", "RETURN",
		"-m", "comment",
		"--comment", formatComment(comment))
}

func (fc FirewallConfiguration) makeFlushChain(name string) *exec.Cmd {
	return exec.Command(fc.BinPath,
		"-t", "nat",
		"-F", name)
}

func (fc FirewallConfiguration) makeDeleteChain(name string) *exec.Cmd {
	return exec.Command(fc.BinPath,
		"-t", "nat",
		"-X", name)
}

func (fc FirewallConfiguration) makeCreateNewChain(name string) *exec.Cmd {
	return exec.Command(fc.BinPath,
		"-t", "nat",
		"-N", name)
}

func (fc FirewallConfiguration) makeRedirectChainToPort(chainName string, portToRedirect int, comment string) *exec.Cmd {
	return exec.Command(fc.BinPath,
		"-t", "nat",
		"-A", chainName,
		"-p", "tcp",
		"-j", "REDIRECT",
		"--to-port", strconv.Itoa(portToRedirect),
		"-m", "comment",
		"--comment", formatComment(comment))
}

func (fc FirewallConfiguration) makeIgnorePorts(chainName string, destinations []string, comment string) *exec.Cmd {
	return exec.Command(fc.BinPath,
		"-t", "nat",
		"-A", chainName,
		"-p", "tcp",
		"--match", "multiport",
		"--dports", strings.Join(destinations, ","),
		"-j", "RETURN",
		"-m", "comment",
		"--comment", formatComment(comment))
}

func (fc FirewallConfiguration) makeIgnoreSubnet(chainName string, subnet string, comment string) *exec.Cmd {
	return exec.Command(fc.BinPath,
		"-t", "nat",
		"-A", chainName,
		"-p", "all",
		"-j", "RETURN",
		"-s", subnet,
		"-m", "comment",
		"--comment", formatComment(comment))
}

func (fc FirewallConfiguration) makeIgnoreLoopback(chainName string, comment string) *exec.Cmd {
	return exec.Command(fc.BinPath,
		"-t", "nat",
		"-A", chainName,
		"-o", "lo",
		"-j", "RETURN",
		"-m", "comment",
		"--comment", formatComment(comment))
}

func (fc FirewallConfiguration) makeRedirectChainToPortBasedOnDestinationPort(chainName string, destinationPort int, portToRedirect int, comment string) *exec.Cmd {
	return exec.Command(fc.BinPath,
		"-t", "nat",
		"-A", chainName,
		"-p", "tcp",
		"--destination-port", strconv.Itoa(destinationPort),
		"-j", "REDIRECT",
		"--to-port", strconv.Itoa(portToRedirect),
		"-m", "comment",
		"--comment", formatComment(comment))
}

func (fc FirewallConfiguration) makeJumpFromChainToAnotherForAllProtocols(chainName string, targetChain string, comment string, delete bool) *exec.Cmd {
	action := "-A"
	if delete {
		action = "-D"
	}

	return exec.Command(fc.BinPath,
		"-t", "nat",
		action, chainName,
		"-j", targetChain,
		"-m", "comment",
		"--comment", formatComment(comment))
}

func (fc FirewallConfiguration) makeShowAllRules() *exec.Cmd {
	return exec.Command(fc.SaveBinPath, "-t", "nat")
}

// asDestination formats the provided `PortRange` for output in commands.
func asDestination(portRange util.PortRange) string {
	if portRange.LowerBound == portRange.UpperBound {
		return fmt.Sprintf("%d", portRange.LowerBound)
	}

	return fmt.Sprintf("%d:%d", portRange.LowerBound, portRange.UpperBound)
}

// resolveBinFallback ensures the configured BinPath and SaveBinPath exist on PATH; if not, it
// tries reasonable alternatives of the same family (ip6tables vs iptables). Returns true if a
// fallback was applied.
func resolveBinFallback(fc *FirewallConfiguration) {
	// helper to check presence
	has := func(name string) bool {
		_, err := exec.LookPath(name)
		return err == nil
	}

	// Both present? nothing to do
	if has(fc.BinPath) && has(fc.SaveBinPath) {
		log.WithFields(log.Fields{
			"requestedBin":     fc.BinPath,
			"requestedSaveBin": fc.SaveBinPath,
		}).Debug("iptables: using configured binaries")
		return
	}

	// Decide family based on current name
	ipv6 := strings.Contains(fc.BinPath, "ip6tables") || strings.Contains(fc.SaveBinPath, "ip6tables")

	// Candidate orders: prefer nft, then plain, then legacy
	var candidates [][2]string
	if ipv6 {
		candidates = [][2]string{
			{"ip6tables-nft", "ip6tables-nft-save"},
			{"ip6tables", "ip6tables-save"},
			{"ip6tables-legacy", "ip6tables-legacy-save"},
		}
	} else {
		candidates = [][2]string{
			{"iptables-nft", "iptables-nft-save"},
			{"iptables", "iptables-save"},
			{"iptables-legacy", "iptables-legacy-save"},
		}
	}

	// Use first candidate where both exist
	for _, pair := range candidates {
		if has(pair[0]) && has(pair[1]) {
			if pair[0] != fc.BinPath || pair[1] != fc.SaveBinPath {
				log.WithFields(log.Fields{
					"requestedBin":     fc.BinPath,
					"requestedSaveBin": fc.SaveBinPath,
					"fallbackBin":      pair[0],
					"fallbackSaveBin":  pair[1],
				}).Warn("iptables: configured binaries not found; applying fallback to available binaries")
			}
			fc.BinPath = pair[0]
			fc.SaveBinPath = pair[1]
			return
		}
	}

	// No candidates found; keep as-is and let execution fail with a clear error later
	log.WithFields(log.Fields{"binPath": fc.BinPath, "saveBinPath": fc.SaveBinPath}).Error("iptables: no suitable binaries found on PATH; commands may fail")
}
