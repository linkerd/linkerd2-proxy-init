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

	"github.com/linkerd/linkerd2-proxy-init/ports"
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
	ProxyInboundPort       int
	ProxyOutgoingPort      int
	ProxyUID               int
	SimulateOnly           bool
	NetNs                  string
	UseWaitFlag            bool
}

// ConfigureFirewall configures a pod's internal iptables to redirect all desired traffic through the proxy, allowing for
// the pod to join the service mesh. A lot of this logic was based on
// https://github.com/istio/istio/blob/e83411e/pilot/docker/prepare_proxy.sh
func ConfigureFirewall(firewallConfiguration FirewallConfiguration) error {
	log.Debugf("Tracing this script execution as [%s]", ExecutionTraceID)

	b := bytes.Buffer{}
	if err := executeCommand(firewallConfiguration, makeShowAllRules(), &b); err != nil {
		log.Error("Aborting firewall configuration")
		return err
	}

	commands := make([]*exec.Cmd, 0)

	matches := chainRegex.FindAllString(b.String(), 1)
	if len(matches) > 0 {
		log.Infof("Found %d existing chains. Skipping iptables setup.", len(matches))
		log.Debugf("Chains: %v", matches)
		return nil
	}

	commands = addIncomingTrafficRules(commands, firewallConfiguration)

	commands = addOutgoingTrafficRules(commands, firewallConfiguration)

	for _, cmd := range commands {
		if err := executeCommand(firewallConfiguration, cmd, nil); err != nil {
			log.Error("Aborting firewall configuration")
			return err
		}
	}

	_ = executeCommand(firewallConfiguration, makeShowAllRules(), nil)

	return nil
}

// formatComment is used to format iptables comments in such way that it is possible to identify when the rules were added.
// This helps debug when iptables has some stale rules from previous runs, something that can happen frequently on minikube.
func formatComment(text string) string {
	return fmt.Sprintf("proxy-init/%s/%s", text, ExecutionTraceID)
}

func addOutgoingTrafficRules(commands []*exec.Cmd, firewallConfiguration FirewallConfiguration) []*exec.Cmd {
	commands = append(commands, makeCreateNewChain(outputChainName, "redirect-common-chain"))

	// Ignore traffic from the proxy
	if firewallConfiguration.ProxyUID > 0 {
		log.Infof("Ignoring uid %d", firewallConfiguration.ProxyUID)
		commands = append(commands, makeIgnoreUserID(outputChainName, firewallConfiguration.ProxyUID, "ignore-proxy-user-id"))
	} else {
		log.Info("Not ignoring any uid")
	}

	// Ignore loopback
	commands = append(commands, makeIgnoreLoopback(outputChainName, "ignore-loopback"))
	// Ignore ports
	commands = addRulesForIgnoredPorts(firewallConfiguration.OutboundPortsToIgnore, outputChainName, commands)

	log.Infof("Redirecting all OUTPUT to %d", firewallConfiguration.ProxyOutgoingPort)
	commands = append(commands, makeRedirectChainToPort(outputChainName, firewallConfiguration.ProxyOutgoingPort, "redirect-all-outgoing-to-proxy-port"))

	// Redirect all remaining outbound traffic to the proxy.
	commands = append(
		commands,
		makeJumpFromChainToAnotherForAllProtocols(
			IptablesOutputChainName,
			outputChainName,
			"install-proxy-init-output",
			false))

	return commands
}

func addIncomingTrafficRules(commands []*exec.Cmd, firewallConfiguration FirewallConfiguration) []*exec.Cmd {
	commands = append(commands, makeCreateNewChain(redirectChainName, "redirect-common-chain"))
	commands = addRulesForIgnoredPorts(firewallConfiguration.InboundPortsToIgnore, redirectChainName, commands)
	commands = addRulesForInboundPortRedirect(firewallConfiguration, redirectChainName, commands)

	// Redirect all remaining inbound traffic to the proxy.
	commands = append(
		commands,
		makeJumpFromChainToAnotherForAllProtocols(
			IptablesPreroutingChainName,
			redirectChainName,
			"install-proxy-init-prerouting",
			false))

	return commands
}

func addRulesForInboundPortRedirect(firewallConfiguration FirewallConfiguration, chainName string, commands []*exec.Cmd) []*exec.Cmd {
	if firewallConfiguration.Mode == RedirectAllMode {
		log.Info("Will redirect all INPUT ports to proxy")
		// Create a new chain for redirecting inbound and outbound traffic to the proxy port.
		commands = append(commands, makeRedirectChainToPort(chainName,
			firewallConfiguration.ProxyInboundPort,
			"redirect-all-incoming-to-proxy-port"))

	} else if firewallConfiguration.Mode == RedirectListedMode {
		log.Infof("Will redirect some INPUT ports to proxy: %v", firewallConfiguration.PortsToRedirectInbound)
		for _, port := range firewallConfiguration.PortsToRedirectInbound {
			commands = append(
				commands,
				makeRedirectChainToPortBasedOnDestinationPort(
					chainName,
					port,
					firewallConfiguration.ProxyInboundPort,
					fmt.Sprintf("redirect-port-%d-to-proxy-port", port)))
		}
	}
	return commands
}

func addRulesForIgnoredPorts(portsToIgnore []string, chainName string, commands []*exec.Cmd) []*exec.Cmd {
	for _, destinations := range makeMultiportDestinations(portsToIgnore) {
		log.Infof("Will ignore port %s on chain %s", destinations, chainName)

		commands = append(commands, makeIgnorePorts(chainName, destinations, fmt.Sprintf("ignore-port-%s", strings.Join(destinations, ","))))
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
		if portRange, err := ports.ParsePortRange(portOrRange); err == nil {
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
			log.Errorf("Invalid port configuration of \"%s\": %s", portOrRange, err.Error())
		}
	}
	return append(destinationSlices, destinations)
}

func executeCommand(firewallConfiguration FirewallConfiguration, cmd *exec.Cmd, cmdOut io.Writer) error {
	if strings.HasSuffix(cmd.Path, "iptables") && firewallConfiguration.UseWaitFlag {
		log.Info("Setting UseWaitFlag: iptables will wait for xtables to become available")
		cmd.Args = append(cmd.Args, "-w")
	}

	if len(firewallConfiguration.NetNs) > 0 {
		nsenterArgs := []string{fmt.Sprintf("--net=%s", firewallConfiguration.NetNs)}
		originalCmd := strings.Trim(fmt.Sprintf("%v", cmd.Args), "[]")
		originalCmdAsArgs := strings.Split(originalCmd, " ")
		// separate nsenter args from the rest with `--`,
		// only needed for hosts using BusyBox binaries, like k3s
		// see https://github.com/rancher/k3s/issues/1434#issuecomment-629315909
		originalCmdAsArgs = append([]string{"--"}, originalCmdAsArgs...)
		finalArgs := append(nsenterArgs, originalCmdAsArgs...)
		cmd = exec.Command("nsenter", finalArgs...)
	}

	log.Infof("%s", strings.Trim(fmt.Sprintf("%v", cmd.Args), "[]"))

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

func makeIgnoreUserID(chainName string, uid int, comment string) *exec.Cmd {
	return exec.Command("iptables",
		"-t", "nat",
		"-A", chainName,
		"-m", "owner",
		"--uid-owner", strconv.Itoa(uid),
		"-j", "RETURN",
		"-m", "comment",
		"--comment", formatComment(comment))
}

func makeCreateNewChain(name string, comment string) *exec.Cmd {
	return exec.Command("iptables",
		"-t", "nat",
		"-N", name,
		"-m", "comment",
		"--comment", formatComment(comment))
}

func makeRedirectChainToPort(chainName string, portToRedirect int, comment string) *exec.Cmd {
	return exec.Command("iptables",
		"-t", "nat",
		"-A", chainName,
		"-p", "tcp",
		"-j", "REDIRECT",
		"--to-port", strconv.Itoa(portToRedirect),
		"-m", "comment",
		"--comment", formatComment(comment))
}

func makeIgnorePorts(chainName string, destinations []string, comment string) *exec.Cmd {
	return exec.Command("iptables",
		"-t", "nat",
		"-A", chainName,
		"-p", "tcp",
		"--match", "multiport",
		"--dports", strings.Join(destinations, ","),
		"-j", "RETURN",
		"-m", "comment",
		"--comment", formatComment(comment))
}

func makeIgnoreLoopback(chainName string, comment string) *exec.Cmd {
	return exec.Command("iptables",
		"-t", "nat",
		"-A", chainName,
		"-o", "lo",
		"-j", "RETURN",
		"-m", "comment",
		"--comment", formatComment(comment))
}

func makeRedirectChainToPortBasedOnDestinationPort(chainName string, destinationPort int, portToRedirect int, comment string) *exec.Cmd {
	return exec.Command("iptables",
		"-t", "nat",
		"-A", chainName,
		"-p", "tcp",
		"--destination-port", strconv.Itoa(destinationPort),
		"-j", "REDIRECT",
		"--to-port", strconv.Itoa(portToRedirect),
		"-m", "comment",
		"--comment", formatComment(comment))
}

func makeJumpFromChainToAnotherForAllProtocols(
	chainName string, targetChain string, comment string, delete bool) *exec.Cmd {
	action := "-A"
	if delete {
		action = "-D"
	}

	return exec.Command("iptables",
		"-t", "nat",
		action, chainName,
		"-j", targetChain,
		"-m", "comment",
		"--comment", formatComment(comment))
}

func makeShowAllRules() *exec.Cmd {
	return exec.Command("iptables-save", "-t", "nat")
}

// asDestination formats the provided `PortRange` for output in commands.
func asDestination(portRange ports.PortRange) string {
	if portRange.LowerBound == portRange.UpperBound {
		return fmt.Sprintf("%d", portRange.LowerBound)
	}

	return fmt.Sprintf("%d:%d", portRange.LowerBound, portRange.UpperBound)
}
