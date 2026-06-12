package cni

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"text/template"

	"github.com/sirupsen/logrus"
)

const (
	// k8sConfigTemplate is a go template used to reconfigure the kubeconfig
	// file for the linkerd plugin.
	k8sConfigTemplate = `# kubeconfig file for linkerd.io/cni-plugin
apiVersion: v1
kind: Config
clusters:
- name: local
  cluster:
    server: {{ .ServiceProtocol }}://{{ .ServiceHost }}:{{ .ServicePort }}
{{- if .SkipTLSVerify }}    insecure-skip-tls-verify: true{{ end }}
{{ if .CertificateAuthorityData }}    certificate-authority-data: {{ .CertificateAuthorityData }}{{ end }}
users:
- name: linkerd-cni
  user:
    token: {{ .AuthToken }}
contexts:
- name: linkerd-cni-context
  context:
    cluster: local
    user: linkerd-cni
current-context: linkerd-cni-context`
	// writeFilePerm are applied to all config files on write.
	writeFilePerm     = 0644
	cniKeyName        = "name"
	cniKeyPlugins     = "plugins"
	cniKeyType        = "type"
	cniKeyVersion     = "cniVersion"
	cniValTypeLinkerd = "linkerd-cni"
)

var (
	errInvalidCNIPlugin      = errors.New("cannot extract plugin from configuration")
	errNoCNIPlugins          = errors.New("cannot determine plugins from existing cni configuration")
	errNoConfigurationSource = errors.New("cannot build configuration from any known source")
)

// source implements io.Reader and is used to build a configuration document.
type source interface {
	// read the source and return data or nil and an error
	read() ([]byte, error)
	// name is used for observability.
	name() string
}

// configureCNI reads CNI configuration from each source; using the first one it
// can find to build a configuration document.
//
// It replaces variables in the document and returns the configuration and nil
// or an error.
func (i *installer) configureCNI(sources []source) ([]byte, error) {
	variables := [][2][]byte{
		{[]byte("__KUBECONFIG_FILEPATH__"), []byte(pluginKubeConfigFilename())},
	}
	var data []byte
	var err error
	for _, source := range sources {
		data, err = source.read()
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"source": source.name(),
				"err":    err,
			}).Error("cannot read from source")
			continue
		}
		for _, variable := range variables {
			data = bytes.ReplaceAll(data, variable[0], variable[1])
		}
		if len(data) > 0 {
			return data, nil
		}
	}
	return nil, errNoConfigurationSource
}

// k8sConfigData is fed into the k8sConfigTemplate (above) used to reconfigure
// the kubeconfig file for the linkerd plugin.
type k8sConfigData struct {
	// authToken is the authentication token used when invoking k8s API for the
	// linkerd user.
	AuthToken string
	// CertificateAuthorityData is a base64 encoding of certificate authority
	// data.
	CertificateAuthorityData string
	// SkipTLSVerify sets tls config to be insecure.
	SkipTLSVerify bool
	// ServiceHost is part of the cluster server URL.
	ServiceHost string
	// ServicePort is part of the cluster server URL.
	ServicePort string
	// ServiceProtocol is part of the cluster server URL.
	ServiceProtocol string
}

// reconfigureK8s populates k8sConfigData with values from the environment and
// the service account token.
//
// The data is passed to the static text template (k8sConfigTemplate) and
// atomically written to dstConfigFilename.
//
// It returns an error in cases where template data are missing, an i/o error
// occurs, or if the template cannot be executed.
func (i *installer) reconfigureK8s(dstConfigFilename string,
	srcTokenFilename string) error {
	var err error
	tokenData, err := os.ReadFile(path.Clean(srcTokenFilename))
	if err != nil {
		return err
	}
	if len(tokenData) < 1 {
		return fmt.Errorf("token-data is zero-length")
	}
	tokenData = bytes.TrimSpace(tokenData)
	configData := &k8sConfigData{
		AuthToken:       string(tokenData),
		SkipTLSVerify:   skipTLSVerify.get() == "true",
		ServiceHost:     svcHost.get(),
		ServicePort:     svcPort.get(),
		ServiceProtocol: svcProtocol.get(),
	}
	var certFilename string
	if kubeCAFile.get() == "" {
		certFilename = path.Join(path.Dir(srcTokenFilename), "ca.crt")
	} else {
		certFilename = kubeCAFile.get()
	}
	certData, err := os.ReadFile(path.Clean(certFilename))
	if err != nil {
		return err
	}
	if len(certData) < 1 {
		return fmt.Errorf("certificate authority data is zero-length")
	}
	certData = bytes.TrimSpace(certData)
	configData.CertificateAuthorityData = base64C.EncodeToString(certData)
	if len(configData.ServiceHost) < 1 {
		return fmt.Errorf("service-host is zero length")
	}
	if len(configData.ServicePort) < 1 {
		return fmt.Errorf("service-port is zero length")
	}
	if len(configData.ServiceProtocol) < 1 {
		return fmt.Errorf("service-protocol is zero length")
	}
	t, err := template.New("k8sConfigTemplate").Parse(k8sConfigTemplate)
	if err != nil {
		// test code should ensure this never happens
		return err
	}
	dstTmpFile := path.Join(path.Dir(dstConfigFilename),
		fmt.Sprintf("%s.install", path.Base(dstConfigFilename)))
	dstW, err := os.OpenFile(path.Clean(dstTmpFile),
		os.O_CREATE|os.O_TRUNC|os.O_WRONLY, writeFilePerm)
	if err != nil {
		return err
	}
	if err = t.Execute(dstW, configData); err != nil {
		_ = dstW.Close()
		return err
	}
	if err = dstW.Sync(); err != nil {
		_ = dstW.Close()
		return err
	}
	if err = dstW.Close(); err != nil {
		return err
	}
	err = os.Rename(dstTmpFile, dstConfigFilename)
	if err != nil {
		return err
	}
	i.appendEntry(&k8sFile{dstConfigFilename})
	return nil
}

// reconfigureCNI reads the config file and merges configuration sourced from
// either the environment at CNI_NETWORK_CONFIG or from a file indicated by the
// environment at CNI_NETWORK_CONFIG_FILE.
//
// It returns nil or if an i/o error occurs reading/writing a file, or if the
// plugin configuration cannot be merged.
//
// It tracks previous configurations using a hash of the config file in the
// installer type.
func (i *installer) reconfigureCNI(configFilename string) error {
	data, err := os.ReadFile(path.Clean(configFilename))
	if err != nil {
		if os.IsNotExist(err) {
			logrus.WithField("filename", configFilename).
				Warn("skipping file that does not exist")
			return nil
		}
		return err
	}
	if i.fileHashSet[configFilename] == hashEncode(data) {
		logrus.WithFields(logrus.Fields{
			"filename": configFilename,
			"hash":     i.fileHashSet[configFilename],
		}).Debug("skipping unchanged file")
		return nil
	}
	configuration, err := i.configureCNI(i.sources)
	if err != nil {
		return err
	}
	merged, err := inject(data, configuration)
	if err != nil {
		return err
	}
	// cni configuration w/ multiple plugins uses a different suffix
	var previousConfigFilename string
	if strings.HasSuffix(configFilename, ".conf") {
		previousConfigFilename = configFilename
		// 99-cni-foo.conf -> 99-cni-foo.conflist
		configFilename = fmt.Sprintf("%slist", configFilename)
	}
	tmpFilename := path.Clean(path.Join(path.Dir(configFilename),
		fmt.Sprintf("%s.install", path.Base(configFilename))))
	err = os.WriteFile(tmpFilename, merged, writeFilePerm)
	if err != nil {
		return err
	}
	err = os.Rename(tmpFilename, configFilename)
	if err != nil {
		return err
	}
	i.appendEntry(&cniFile{configFilename})
	i.fileHashSet[configFilename] = hashEncode(merged)
	logrus.WithFields(logrus.Fields{
		"filename": configFilename,
		"hash":     i.fileHashSet[configFilename],
	}).Debug("reconfigured cni")
	if previousConfigFilename != "" {
		return os.Remove(previousConfigFilename)
	}
	return nil
}

// inject the linkerd plugin configuration into the existing configuration
// bytes. Look for the 'type' key at  the top of the configuration map
// indicating whether or not the configuration is for a single plugin.  Upgrade
// it to a plugin list.
//
// If the 'type' key does not exists, ensure our configuration appended to the
// tail of the plugin list.
func inject(existing, linkerd []byte) ([]byte, error) {
	var existingVal map[string]any
	err := json.Unmarshal(existing, &existingVal)
	if err != nil {
		return nil, err
	}
	var linkerdVal map[string]any
	err = json.Unmarshal(linkerd, &linkerdVal)
	if err != nil {
		return nil, err
	}

	var resultVal map[string]any
	// 'type' at the root of existing val indicates the configuration is a
	// single plugin (vs a list)
	if _, ok := existingVal[cniKeyType]; ok {
		delete(existingVal, cniKeyVersion)
		resultVal = map[string]any{
			cniKeyName:    "k8s-pod-network",
			cniKeyVersion: "0.3.1",
			cniKeyPlugins: []map[string]any{
				existingVal,
				linkerdVal,
			},
		}
	} else {
		// find linkerd in the list and remove it if its there
		plugins, ok := existingVal[cniKeyPlugins].([]any)
		if !ok {
			return nil, errNoCNIPlugins
		}
		linkerdAt := -1
		for i := 0; i < len(plugins); i++ {
			plugin, ok := plugins[i].(map[string]any)
			if !ok {
				return nil, errInvalidCNIPlugin
			}
			if pluginType, ok := plugin[cniKeyType]; ok && pluginType == cniValTypeLinkerd {
				linkerdAt = i
				break
			}
		}
		// remove linkerd from the list
		if linkerdAt >= 0 {
			plugins = append(plugins[:linkerdAt], plugins[linkerdAt+1:]...)
		}
		// append it to the list
		plugins = append(plugins, linkerdVal)
		existingVal[cniKeyPlugins] = plugins
		resultVal = existingVal
	}
	return json.MarshalIndent(resultVal, "", "  ")
}
