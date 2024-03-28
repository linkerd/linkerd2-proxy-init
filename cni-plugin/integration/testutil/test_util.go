package testutil

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/mitchellh/mapstructure"
)

// TestRunner is a helper struct used for cni-plugin integration test
type TestRunner struct {
	confDir  string
	confFile string
}

// NewTestRunner creates a new TestRunner helper based on a provided directory
// path and a CNI conf file name
func NewTestRunner(confDir, confFile string) *TestRunner {
	return &TestRunner{
		confDir,
		confFile,
	}
}

// Given a configuration directory (e.g `/host/etc/cni/net.d`), traverse directory
// and collect files into a map where each key is the filename and value is an
// empty struct. Used as a util function to check if a given file exists in a
// directory.
func (r *TestRunner) walkConfDir() (map[string]struct{}, error) {
	files, err := os.ReadDir(r.confDir)
	if err != nil {
		return nil, err
	}

	fileNames := make(map[string]struct{}, len(files))
	for _, f := range files {
		fileNames[f.Name()] = struct{}{}
	}

	return fileNames, nil
}

// ProxyInit is the configuration for the proxy-init binary
type ProxyInit struct {
	IncomingProxyPort     int      `json:"incoming-proxy-port"`
	OutgoingProxyPort     int      `json:"outgoing-proxy-port"`
	ProxyUID              int      `json:"proxy-uid"`
	PortsToRedirect       []int    `json:"ports-to-redirect"`
	InboundPortsToIgnore  []string `json:"inbound-ports-to-ignore"`
	OutboundPortsToIgnore []string `json:"outbound-ports-to-ignore"`
	SubnetsToIgnore       []string `json:"subnets-to-ignore"`
	Simulate              bool     `json:"simulate"`
	UseWaitFlag           bool     `json:"use-wait-flag"`
	IPTablesMode          string   `json:"iptables-mode"`
	IPv6                  bool     `json:"ipv6"`
}

// LinkerdPlugin is what we use for CNI configuration in the plugins section
type LinkerdPlugin struct {
	Name       string         `json:"name"`
	Type       string         `json:"type"`
	LogLevel   string         `json:"log_level"`
	Policy     map[string]any `json:"policy"`
	Kubernetes map[string]any `json:"kubernetes"`
	ProxyInit  ProxyInit      `json:"linkerd"`
}

// Checks that the embedded linkerd config is of the expected form
// and contains the right values:
//
//	"linkerd": {
//	   "incoming-proxy-port": 4143,
//	   "outgoing-proxy-port": 4140,
//	   "proxy-uid": 2102,
//	   "ports-to-redirect": [],
//	   "inbound-ports-to-ignore": ["4191","4190"],
//	   "simulate": false,
//	   "use-wait-flag": false
//	 }
func checkLinkerdCniConf(plugin map[string]any) error {
	linkerdPlugin := &LinkerdPlugin{}
	config := &mapstructure.DecoderConfig{
		TagName:     "json",
		Result:      linkerdPlugin,
		ErrorUnused: true, // will fail if there are unparsed map elements
	}

	decoder, err := mapstructure.NewDecoder(config)
	if err != nil {
		return err
	}

	if err := decoder.Decode(plugin); err != nil {
		return err
	}

	proxyInit := linkerdPlugin.ProxyInit
	incomingProxyPort := proxyInit.IncomingProxyPort
	if incomingProxyPort != 4143 {
		return fmt.Errorf("incoming-proxy-port has wrong value, expected: %v, found: %v",
			4143, incomingProxyPort)
	}

	outgoingProxyPort := proxyInit.OutgoingProxyPort
	if outgoingProxyPort != 4140 {
		return fmt.Errorf("outgoing-proxy-port has wrong value, expected: %v, found: %v",
			4140, outgoingProxyPort)
	}

	proxyUID := proxyInit.ProxyUID
	if proxyUID != 2102 {
		return fmt.Errorf("proxy-uid has wrong value, expected: %v, found: %v", 2102, proxyUID)
	}

	simulate := proxyInit.Simulate
	if simulate {
		return fmt.Errorf("simulate has wrong value, expected: %v, found: %v", false, simulate)
	}

	useWaitFlag := proxyInit.UseWaitFlag
	if useWaitFlag {
		return fmt.Errorf("use-wait-flag has wrong value, expected: %v, found: %v",
			false, useWaitFlag)
	}

	if len(proxyInit.PortsToRedirect) > 0 {
		return fmt.Errorf("ports-to-redirect contains items and should not")
	}

	inboundPortsToIgnore := proxyInit.InboundPortsToIgnore

	expectedInboundPortsToIgnore := [2]string{"4191", "4190"}
	if inboundPortsToIgnore[0] != expectedInboundPortsToIgnore[0] ||
		inboundPortsToIgnore[1] != expectedInboundPortsToIgnore[1] {
		return fmt.Errorf("inbound-ports-to-ignore has wrong elements: found: %v, expected %v",
			inboundPortsToIgnore, expectedInboundPortsToIgnore)
	}

	return nil
}

// CheckCNIPluginIsLast will, based on a configuration directory path, and a CNI
// conflist file name, determine whether 'linkerd-cni' has appended itself to
// the existing plugin, and if it has been configured properly
func (r *TestRunner) CheckCNIPluginIsLast() error {
	if _, err := os.Stat(r.confDir); os.IsNotExist(err) {
		return fmt.Errorf("Directory does not exist. Check if volume mount exists: %s", r.confDir)
	}

	filenames, err := r.walkConfDir()

	if err != nil {
		return fmt.Errorf("unable to read files from directory %s due to error: %w", r.confDir, err)
	}

	if len(filenames) == 0 {
		return fmt.Errorf("no files found in %s", r.confDir)
	}

	if len(filenames) > 3 {
		return fmt.Errorf("too many files found in %s: %s ", r.confDir, filenames)
	}

	if _, ok := filenames[r.confFile]; !ok {
		return fmt.Errorf("filenames does not contain %s, instead it contains: %s", r.confFile, filenames)
	}

	conflistFile, err := os.ReadFile(r.confDir + "/" + r.confFile)
	if err != nil {
		return fmt.Errorf("could not read %s: %w", r.confFile, err)
	}

	var conflist map[string]any
	err = json.Unmarshal(conflistFile, &conflist)
	if err != nil {
		return fmt.Errorf("unmarshaling conflist json failed: %w", err)
	}

	plugins := conflist["plugins"].([]interface{})
	// check that the last plugin is linkerd
	linkerdPlugin := plugins[len(plugins)-1].(map[string]any)
	if linkerdPlugin["name"] != "linkerd-cni" {
		return fmt.Errorf("linkerd-cni was not last in the plugins list")
	}

	if err = checkLinkerdCniConf(linkerdPlugin); err != nil {
		return fmt.Errorf("Configuration contains bad values: %w", err)
	}

	return nil
}
