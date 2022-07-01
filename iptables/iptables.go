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
	SubnetsToIgnore        []string
	ProxyInboundPort       int
	ProxyOutgoingPort      int
	ProxyUID               int
	SimulateOnly           bool
	NetNs                  string
	UseWaitFlag            bool
	UseNFTBackend          bool
}

// ConfigureFirewall configures a pod's internal iptables to redirect all desired traffic through the proxy, allowing for
// the pod to join the service mesh. A lot of this logic was based on
// https://github.com/istio/istio/blob/e83411e/pilot/docker/prepare_proxy.sh
func ConfigureFirewall(firewallConfiguration FirewallConfiguration) error {
	log.Debugf("tracing script execution as [%s]", ExecutionTraceID)

	iptablesBin := getBinaryName(firewallConfiguration.UseNFTBackend)
	log.Debugf("setting up iptables routing by calling into '%s'", iptablesBin)

	b := bytes.Buffer{}
	if err := executeCommand(iptablesBin, firewallConfiguration, makeShowAllRules(iptablesBin), &b); err != nil {
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

	commands = addIncomingTrafficRules(commands, iptablesBin, firewallConfiguration)

	commands = addOutgoingTrafficRules(commands, iptablesBin, firewallConfiguration)

	for _, cmd := range commands {
		if err := executeCommand(iptablesBin, firewallConfiguration, cmd, nil); err != nil {
			return err
		}
	}

	_ = executeCommand(iptablesBin, firewallConfiguration, makeShowAllRules(iptablesBin), nil)

	return nil
}

// formatComment is used to format iptables comments in such way that it is possible to identify when the rules were added.
// This helps debug when iptables has some stale rules from previous runs, something that can happen frequently on minikube.
func formatComment(text string) string {
	return fmt.Sprintf("proxy-init/%s/%s", text, ExecutionTraceID)
}

func addOutgoingTrafficRules(commands []*exec.Cmd, bin string, firewallConfiguration FirewallConfiguration) []*exec.Cmd {

	commands = append(commands, makeCreateNewChain(bin, outputChainName, "redirect-common-chain"))

	// Ignore traffic from the proxy
	if firewallConfiguration.ProxyUID > 0 {
		commands = append(commands, makeIgnoreUserID(bin, outputChainName, firewallConfiguration.ProxyUID, "ignore-proxy-user-id"))
	}

	// Ignore loopback
	commands = append(commands, makeIgnoreLoopback(bin, outputChainName, "ignore-loopback"))
	// Ignore ports
	commands = addRulesForIgnoredPorts(bin, firewallConfiguration.OutboundPortsToIgnore, outputChainName, commands)

	commands = append(commands, makeRedirectChainToPort(bin, outputChainName, firewallConfiguration.ProxyOutgoingPort, "redirect-all-outgoing-to-proxy-port"))

	// Redirect all remaining outbound traffic to the proxy.
	commands = append(
		commands,
		makeJumpFromChainToAnotherForAllProtocols(
			bin,
			IptablesOutputChainName,
			outputChainName,
			"install-proxy-init-output",
			false))

	return commands
}

func addIncomingTrafficRules(commands []*exec.Cmd, bin string, firewallConfiguration FirewallConfiguration) []*exec.Cmd {
	commands = append(commands, makeCreateNewChain(bin, redirectChainName, "redirect-common-chain"))
	commands = addRulesForIgnoredPorts(bin, firewallConfiguration.InboundPortsToIgnore, redirectChainName, commands)
	commands = addRulesForIgnoredSubnets(bin, firewallConfiguration.SubnetsToIgnore, redirectChainName, commands)
	commands = addRulesForInboundPortRedirect(bin, firewallConfiguration, redirectChainName, commands)

	// Redirect all remaining inbound traffic to the proxy.
	commands = append(
		commands,
		makeJumpFromChainToAnotherForAllProtocols(
			bin,
			IptablesPreroutingChainName,
			redirectChainName,
			"install-proxy-init-prerouting",
			false))

	return commands
}

func addRulesForInboundPortRedirect(bin string, firewallConfiguration FirewallConfiguration, chainName string, commands []*exec.Cmd) []*exec.Cmd {
	if firewallConfiguration.Mode == RedirectAllMode {
		// Create a new chain for redirecting inbound and outbound traffic to the proxy port.
		commands = append(commands, makeRedirectChainToPort(
			bin,
			chainName,
			firewallConfiguration.ProxyInboundPort,
			"redirect-all-incoming-to-proxy-port"))

	} else if firewallConfiguration.Mode == RedirectListedMode {
		for _, port := range firewallConfiguration.PortsToRedirectInbound {
			commands = append(
				commands,
				makeRedirectChainToPortBasedOnDestinationPort(
					bin,
					chainName,
					port,
					firewallConfiguration.ProxyInboundPort,
					fmt.Sprintf("redirect-port-%d-to-proxy-port", port)))
		}
	}
	return commands
}

func addRulesForIgnoredPorts(bin string, portsToIgnore []string, chainName string, commands []*exec.Cmd) []*exec.Cmd {
	for _, destinations := range makeMultiportDestinations(portsToIgnore) {
		commands = append(commands, makeIgnorePorts(bin, chainName, destinations, fmt.Sprintf("ignore-port-%s", strings.Join(destinations, ","))))
	}
	return commands
}

func addRulesForIgnoredSubnets(bin string, subnetsToIgnore []string, chainName string, commands []*exec.Cmd) []*exec.Cmd {
	for _, subnet := range subnetsToIgnore {
		commands = append(commands, makeIgnoreSubnet(bin, chainName, subnet, fmt.Sprintf("ignore-subnet-%s", subnet)))
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
			log.Errorf("invalid port configuration of \"%s\": %s", portOrRange, err.Error())
		}
	}
	return append(destinationSlices, destinations)
}

func executeCommand(bin string, firewallConfiguration FirewallConfiguration, cmd *exec.Cmd, cmdOut io.Writer) error {
	if strings.HasSuffix(cmd.Path, bin) && firewallConfiguration.UseWaitFlag {
		log.Info("'useWaitFlag' set: iptables will wait for xtables to become available")
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

func makeIgnoreUserID(bin string, chainName string, uid int, comment string) *exec.Cmd {
	return exec.Command(bin,
		"-t", "nat",
		"-A", chainName,
		"-m", "owner",
		"--uid-owner", strconv.Itoa(uid),
		"-j", "RETURN",
		"-m", "comment",
		"--comment", formatComment(comment))
}

func makeCreateNewChain(bin string, name string, comment string) *exec.Cmd {
	return exec.Command(bin,
		"-t", "nat",
		"-N", name,
		"-m", "comment",
		"--comment", formatComment(comment))
}

func makeRedirectChainToPort(bin string, chainName string, portToRedirect int, comment string) *exec.Cmd {
	return exec.Command(bin,
		"-t", "nat",
		"-A", chainName,
		"-p", "tcp",
		"-j", "REDIRECT",
		"--to-port", strconv.Itoa(portToRedirect),
		"-m", "comment",
		"--comment", formatComment(comment))
}

func makeIgnorePorts(bin string, chainName string, destinations []string, comment string) *exec.Cmd {
	return exec.Command(bin,
		"-t", "nat",
		"-A", chainName,
		"-p", "tcp",
		"--match", "multiport",
		"--dports", strings.Join(destinations, ","),
		"-j", "RETURN",
		"-m", "comment",
		"--comment", formatComment(comment))
}

func makeIgnoreSubnet(bin string, chainName string, subnet string, comment string) *exec.Cmd {
	return exec.Command(bin,
		"-t", "nat",
		"-A", chainName,
		"-p", "all",
		"-j", "RETURN",
		"-s", subnet,
		"-m", "comment",
		"--comment", formatComment(comment))
}

func makeIgnoreLoopback(bin string, chainName string, comment string) *exec.Cmd {
	return exec.Command(bin,
		"-t", "nat",
		"-A", chainName,
		"-o", "lo",
		"-j", "RETURN",
		"-m", "comment",
		"--comment", formatComment(comment))
}

func makeRedirectChainToPortBasedOnDestinationPort(bin string, chainName string, destinationPort int, portToRedirect int, comment string) *exec.Cmd {
	return exec.Command(bin,
		"-t", "nat",
		"-A", chainName,
		"-p", "tcp",
		"--destination-port", strconv.Itoa(destinationPort),
		"-j", "REDIRECT",
		"--to-port", strconv.Itoa(portToRedirect),
		"-m", "comment",
		"--comment", formatComment(comment))
}

func makeJumpFromChainToAnotherForAllProtocols(bin string, chainName string, targetChain string, comment string, delete bool) *exec.Cmd {
	action := "-A"
	if delete {
		action = "-D"
	}

	return exec.Command(bin,
		"-t", "nat",
		action, chainName,
		"-j", targetChain,
		"-m", "comment",
		"--comment", formatComment(comment))
}

func makeShowAllRules(bin string) *exec.Cmd {
	return exec.Command(bin, "-t", "nat")
}

// asDestination formats the provided `PortRange` for output in commands.
func asDestination(portRange ports.PortRange) string {
	if portRange.LowerBound == portRange.UpperBound {
		return fmt.Sprintf("%d", portRange.LowerBound)
	}

	return fmt.Sprintf("%d:%d", portRange.LowerBound, portRange.UpperBound)
}

// getBinaryName will return the name of the iptables binary to call into,
// depending on whether the NFT Kernel API mode is on.
func getBinaryName(useNft bool) string {
	bin := "iptables"
	if useNft {
		bin = "iptables-nft"
	}
	return bin
}
