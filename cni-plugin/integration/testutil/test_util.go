package testutil

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
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
func checkLinkerdCniConf(wrapperConf map[string]any) error {
	var conf = wrapperConf["linkerd"].(map[string]any)

	var incomingProxyPort = conf["incoming-proxy-port"].(float64)
	if incomingProxyPort != 4143 {
		return fmt.Errorf("incoming-proxy-port has wrong value: expected: %v, found: %v",
			4143, incomingProxyPort)
	}

	var outgoingProxyPort = conf["outgoing-proxy-port"].(float64)
	if outgoingProxyPort != 4140 {
		return fmt.Errorf("outgoing-proxy-port has wrong value, expected: %v, found: %v",
			4140, outgoingProxyPort)
	}

	var proxyUID = conf["proxy-uid"].(float64)
	if proxyUID != 2102 {
		return fmt.Errorf("proxy-uid has wrong value, expected: %v, found: %v", 2102, proxyUID)
	}

	var simulate = conf["simulate"].(bool)
	if simulate {
		return fmt.Errorf("simulate has wrong value, expected: %v, found: %v", false, simulate)
	}

	var useWaitFlag = conf["use-wait-flag"].(bool)
	if useWaitFlag {
		return fmt.Errorf("use-wait-flag has wrong value, expected: %v, found: %v",
			false, useWaitFlag)
	}

	if len(conf["ports-to-redirect"].([]any)) > 0 {
		return fmt.Errorf("ports-to-redirect contains items and should not")
	}

	var inboundPortsToIgnoreAny = conf["inbound-ports-to-ignore"].([]interface{})
	var inboundPortsToIgnore = make([]float64, len(inboundPortsToIgnoreAny))
	for i, d := range inboundPortsToIgnoreAny {
		if num, err := strconv.ParseFloat(d.(string), 64); err == nil {
			inboundPortsToIgnore[i] = num
		}
	}
	var expectedInboundPortsToIgnore = [2]float64{4191, 4190}
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
	lastPlugin := plugins[len(plugins)-1].(map[string]any)
	err = checkLinkerdCniConf(lastPlugin)
	if err != nil {
		return err
	}
	if lastPlugin["name"] != "linkerd-cni" {
		return fmt.Errorf("linkerd-cni was not last in the plugins list")
	}

	return nil
}
