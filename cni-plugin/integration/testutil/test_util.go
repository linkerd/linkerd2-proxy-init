package testutil

import (
	"encoding/json"
	"fmt"
	"os"
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

// Based on a configuration directory path, and a CNI conflist file name,
// determine whether 'linkerd-cni' has appended itself to the existing plugin,
// and if it has been configured properly
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

	// if conflist["cniVersion"] != "1.0.0" {
	// return fmt.Errorf("expected cniVersion 1.0.0, instead saw %s", conflistFile)
	// }

	plugins := conflist["plugins"].([]interface{})
	lastPlugin := plugins[len(plugins)-1].(map[string]any)
	if lastPlugin["name"] != "linkerd-cni" {
		return fmt.Errorf("linkerd-cni was not last in the plugins list")
	}

	return nil
}
