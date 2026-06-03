package cni

import (
	"os"
	"path"
)

var (
	cniBinDir             = envVar{key: "DEST_CNI_BIN_DIR", defaultVal: "/opt/cni/bin"}
	cniConfigDir          = envVar{key: "DEST_CNI_NET_DIR", defaultVal: "/etc/cni/net.d"}
	cniNetworkConfigFile  = envVar{key: "CNI_NETWORK_CONFIG_FILE", defaultVal: ""}
	containerCNIBinDir    = envVar{key: "CONTAINER_CNI_BIN_DIR", defaultVal: "/opt/cni/bin"}
	containerMountPrefix  = envVar{key: "CONTAINER_MOUNT_PREFIX", defaultVal: "/host"}
	kubeCAFile            = envVar{key: "KUBE_CA_FILE", defaultVal: ""}
	kubeConfigFilenameVar = envVar{key: "KUBECONFIG_FILE_NAME", defaultVal: "ZZZ-linkerd-cni-kubeconfig"}
	skipTLSVerify         = envVar{key: "SKIP_TLS_VERIFY", defaultVal: "false"}
	svcHost               = envVar{key: "KUBERNETES_SERVICE_HOST", defaultVal: ""}
	svcPort               = envVar{key: "KUBERNETES_SERVICE_PORT", defaultVal: ""}
	svcProtocol           = envVar{key: "KUBERNETES_SERVICE_PROTOCOL", defaultVal: "https"}
)

// hostCNIBin returns the host directory into which the linkerd cni plugin (binary)
// is installed.
// example: /host/etc/cni/net.d
func hostCNIBin() string {
	return path.Join(containerMountPrefix.get(), cniBinDir.get())
}

// hostCNIConfig returns host directory at which cni configuration resides.
// example:  /host/etc/cni/net.d
func hostCNIConfig() string {
	return path.Join(containerMountPrefix.get(), cniConfigDir.get())
}

// kubeConfigFilename return the host directory at which the kube configuration
// file resides.
func kubeConfigFilename() string {
	return path.Join(containerMountPrefix.get(), cniConfigDir.get(),
		kubeConfigFilenameVar.get())
}

// envVar combines an environment variable name (key) and a default value.
type envVar struct {
	key        string
	defaultVal string
}

func (ev envVar) get() string {
	val := os.Getenv(ev.key)
	if val == "" {
		return ev.defaultVal
	}
	return val
}
