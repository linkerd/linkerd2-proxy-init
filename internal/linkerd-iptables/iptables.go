package iptables

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	util "github.com/linkerd/linkerd2-proxy-init/internal/linkerd-util"
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
	ExecutionTraceID = strconv.Itoa(int(time.Now().Unix()))

	chainRegex = regexp.MustCompile(`-A (PROXY_INIT_OUTPUT|PROXY_INIT_REDIRECT).*`)
)

// FirewallConfiguration specifies how to configure a pod's iptables.
type FirewallConfiguration struct {
	Mode                   string
	PortsToRedirectInbound []int
	InboundPortsToIgnore   []string
	OutboundPortsToIgnore  []string
	SubnetsToIgnore        []string
	ProxyInboundPort       int
	ProxyOutgoingPort      int
	ProxyUID               int
	SimulateOnly           bool
	NetNs                  string
	UseWaitFlag            bool
	BinPath                string
	SaveBinPath            string
}

// ConfigureFirewall configures a pod's internal iptables to redirect all desired traffic through the proxy, allowing for
// the pod to join the service mesh. A lot of this logic was based on
// https://github.com/istio/istio/blob/e83411e/pilot/docker/prepare_proxy.sh
func ConfigureFirewall(firewallConfiguration FirewallConfiguration) error {
	log.Debugf("tracing script execution as [%s]", ExecutionTraceID)
	log.Debugf("using '%s' to set-up firewall rules", firewallConfiguration.BinPath)
	log.Debugf("using '%s' to list all available rules", firewallConfiguration.SaveBinPath)

	b := bytes.Buffer{}
	if err := executeCommand(firewallConfiguration, firewallConfiguration.makeShowAllRules(), &b); err != nil {
		log.Error("aborting firewall configuration")
		return err
	}

	commands := make([]*exec.Cmd, 0)

	matches := chainRegex.FindAllString(b.String(), 1)
	if len(matches) > 0 {
		log.Infof("skipping iptables setup: found %d existing chains", len(matches))
		log.Debugf("matching chains: %v", matches)
		return nil
	}

	commands = firewallConfiguration.addIncomingTrafficRules(commands)

	commands = firewallConfiguration.addOutgoingTrafficRules(commands)

	if firewallConfiguration.UseWaitFlag {
		log.Debug("'useWaitFlag' set: iptables will wait for xtables to become available")
	}

	for _, cmd := range commands {
		if firewallConfiguration.UseWaitFlag {
			cmd.Args = append(cmd.Args, "-w")
		}

		if err := executeCommand(firewallConfiguration, cmd, nil); err != nil {
			return err
		}
	}

	_ = executeCommand(firewallConfiguration, firewallConfiguration.makeShowAllRules(), nil)

	return nil
}

// formatComment is used to format iptables comments in such way that it is possible to identify when the rules were added.
// This helps debug when iptables has some stale rules from previous runs, something that can happen frequently on minikube.
func formatComment(text string) string {
	return fmt.Sprintf("proxy-init/%s/%s", text, ExecutionTraceID)
}

func (fc FirewallConfiguration) addOutgoingTrafficRules(commands []*exec.Cmd) []*exec.Cmd {
	commands = append(commands, fc.makeCreateNewChain(outputChainName, "redirect-common-chain"))

	// Ignore traffic from the proxy
	if fc.ProxyUID > 0 {
		commands = append(commands, fc.makeIgnoreUserID(outputChainName, fc.ProxyUID, "ignore-proxy-user-id"))
	}

	// Ignore loopback
	commands = append(commands, fc.makeIgnoreLoopback(outputChainName, "ignore-loopback"))
	// Ignore ports
	commands = fc.addRulesForIgnoredPorts(fc.OutboundPortsToIgnore, outputChainName, commands)

	commands = append(commands, fc.makeRedirectChainToPort(outputChainName, fc.ProxyOutgoingPort, "redirect-all-outgoing-to-proxy-port"))

	// Redirect all remaining outbound traffic to the proxy.
	commands = append(
		commands,
		fc.makeJumpFromChainToAnotherForAllProtocols(
			IptablesOutputChainName,
			outputChainName,
			"install-proxy-init-output",
			false))

	return commands
}

func (fc FirewallConfiguration) addIncomingTrafficRules(commands []*exec.Cmd) []*exec.Cmd {
	commands = append(commands, fc.makeCreateNewChain(redirectChainName, "redirect-common-chain"))
	commands = fc.addRulesForIgnoredPorts(fc.InboundPortsToIgnore, redirectChainName, commands)
	commands = fc.addRulesForIgnoredSubnets(redirectChainName, commands)
	commands = fc.addRulesForInboundPortRedirect(redirectChainName, commands)

	// Redirect all remaining inbound traffic to the proxy.
	commands = append(
		commands,
		fc.makeJumpFromChainToAnotherForAllProtocols(
			IptablesPreroutingChainName,
			redirectChainName,
			"install-proxy-init-prerouting",
			false))

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

func executeCommand(firewallConfiguration FirewallConfiguration, cmd *exec.Cmd, cmdOut io.Writer) error {
	if firewallConfiguration.NetNs != "" {
		// BusyBox's `nsenter` needs `--` to separate nsenter arguments from the
		// command.
		//
		// See https://github.com/rancher/k3s/issues/1434#issuecomment-629315909
		args := append([]string{"--net", firewallConfiguration.NetNs, "--"}, cmd.Args...)
		cmd = exec.Command("nsenter", args...)
	}
	log.Info(cmd.String())

	if firewallConfiguration.SimulateOnly {
		return nil
	}

	out, err := cmd.CombinedOutput()

	if len(out) > 0 {
		log.Infof("%s", out)
	}

	if err != nil {
		return err
	}

	if cmdOut == nil {
		return nil
	}

	_, err = io.WriteString(cmdOut, string(out))
	if err != nil {
		return err
	}

	return nil
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

func (fc FirewallConfiguration) makeCreateNewChain(name string, comment string) *exec.Cmd {
	return exec.Command(fc.BinPath,
		"-t", "nat",
		"-N", name,
		"-m", "comment",
		"--comment", formatComment(comment))
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
