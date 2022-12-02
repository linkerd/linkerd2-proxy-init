// Copyright 2017 CNI authors
// Modifications copyright (c) Linkerd authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This file was inspired by:
// 1) https://github.com/istio/cni/blob/c63a509539b5ed165a6617548c31b686f13c2133/cmd/istio-cni/main.go

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	cniv1 "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/cni/pkg/version"
	iptables "github.com/linkerd/linkerd2-proxy-init/pkg/linkerd-iptables"
	"github.com/linkerd/linkerd2-proxy-init/proxy-init/cmd"

	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// TrueAsString the boolean true formatted as a string
const TrueAsString = "true"

// ProxyInit is the configuration for the proxy-init binary
type ProxyInit struct {
	IncomingProxyPort     int      `json:"incoming-proxy-port"`
	OutgoingProxyPort     int      `json:"outgoing-proxy-port"`
	ProxyUID              int      `json:"proxy-uid"`
	PortsToRedirect       []int    `json:"ports-to-redirect"`
	InboundPortsToIgnore  []string `json:"inbound-ports-to-ignore"`
	OutboundPortsToIgnore []string `json:"outbound-ports-to-ignore"`
	Simulate              bool     `json:"simulate"`
	UseWaitFlag           bool     `json:"use-wait-flag"`
}

// Kubernetes a K8s specific struct to hold config
type Kubernetes struct {
	K8sAPIRoot string `json:"k8s_api_root"`
	Kubeconfig string `json:"kubeconfig"`
}

// K8sArgs is the valid CNI_ARGS used for Kubernetes
// The field names need to match exact keys in kubelet args for unmarshalling
type K8sArgs struct {
	types.CommonArgs
	K8sPodName      types.UnmarshallableString
	K8sPodNamespace types.UnmarshallableString
}

// PluginConf is whatever JSON is passed via stdin.
type PluginConf struct {
	types.NetConf

	// This is the previous result, when called in the context of a chained
	// plugin. We will just pass any prevResult through.
	RawPrevResult *map[string]interface{} `json:"prevResult"`
	PrevResult    *cniv1.Result           `json:"-"`

	LogLevel   string     `json:"log_level"`
	ProxyInit  ProxyInit  `json:"linkerd"`
	Kubernetes Kubernetes `json:"kubernetes"`
}

func main() {
	skel.PluginMain(cmdAdd, cmdCheck, cmdDel, version.All, "")
}

func configureLogging(logLevel string) {
	switch strings.ToLower(logLevel) {
	case "debug":
		logrus.SetLevel(logrus.DebugLevel)
	case "info":
		logrus.SetLevel(logrus.InfoLevel)
	default:
		logrus.SetLevel(logrus.WarnLevel)
	}

	// Must log to Stderr because the CNI runtime uses Stdout as its state
	logrus.SetOutput(os.Stderr)
}

// parseConfig parses the supplied configuration (and prevResult) from stdin.
func parseConfig(stdin []byte) (*PluginConf, error) {
	conf := PluginConf{}

	logrus.Debugf("linkerd-cni: stdin to plugin: %v", string(stdin)) //nolint:gosec
	if err := json.Unmarshal(stdin, &conf); err != nil {
		return nil, fmt.Errorf("linkerd-cni: failed to parse network configuration: %w", err)
	}

	if conf.RawPrevResult != nil {
		resultBytes, err := json.Marshal(conf.RawPrevResult)
		if err != nil {
			return nil, fmt.Errorf("linkerd-cni: could not serialize prevResult: %w", err)
		}

		res, err := version.NewResult(conf.CNIVersion, resultBytes)
		if err != nil {
			return nil, fmt.Errorf("linkerd-cni: could not parse prevResult: %w", err)
		}
		conf.RawPrevResult = nil
		conf.PrevResult, err = cniv1.NewResultFromResult(res)
		if err != nil {
			return nil, fmt.Errorf("linkerd-cni: could not convert result to version 1.0: %w", err)
		}
		logrus.Debugf("linkerd-cni: prevResult: %v", conf.PrevResult)
	}

	return &conf, nil
}

// cmdAdd is called by the CNI runtime for ADD requests
// nolint:gosec
func cmdAdd(args *skel.CmdArgs) error {
	logrus.Debug("linkerd-cni: cmdAdd, parsing config")
	conf, err := parseConfig(args.StdinData)
	if err != nil {
		return err
	}
	configureLogging(conf.LogLevel)

	if conf.PrevResult != nil {
		logrus.WithFields(logrus.Fields{
			"version":    conf.CNIVersion,
			"prevResult": conf.PrevResult,
		}).Debug("linkerd-cni: cmdAdd, config parsed")
	} else {
		logrus.WithFields(logrus.Fields{
			"version": conf.CNIVersion,
		}).Debug("linkerd-cni: cmdAdd, config parsed")
	}

	// Determine if running under k8s by checking the CNI args
	k8sArgs := K8sArgs{}
	args.Args = strings.Replace(args.Args, "K8S_POD_NAMESPACE", "K8sPodNamespace", 1)
	args.Args = strings.Replace(args.Args, "K8S_POD_NAME", "K8sPodName", 1)
	if err := types.LoadArgs(args.Args, &k8sArgs); err != nil {
		return err
	}

	namespace := string(k8sArgs.K8sPodNamespace)
	podName := string(k8sArgs.K8sPodName)
	logEntry := logrus.WithFields(logrus.Fields{
		"ContainerID": args.ContainerID,
		"Pod":         podName,
		"Namespace":   namespace,
	})

	if namespace != "" && podName != "" {
		ctx := context.Background()

		configLoadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: conf.Kubernetes.Kubeconfig}
		configOverrides := &clientcmd.ConfigOverrides{CurrentContext: "linkerd-cni-context"}

		config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(configLoadingRules, configOverrides).ClientConfig()
		if err != nil {
			return err
		}

		client, err := kubernetes.NewForConfig(config)
		if err != nil {
			return err
		}

		pod, err := client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		k8sProxyContainerName := "linkerd-proxy"
		containsLinkerdProxy := false
		for _, container := range pod.Spec.Containers {
			// TODO(stevej): hardcoded. k8s.ProxyContainerName originates in values.yaml?
			if container.Name == k8sProxyContainerName {
				containsLinkerdProxy = true
				break
			}
		}

		k8sInitContainerName := "linkerd-init"
		containsInitContainer := false
		for _, container := range pod.Spec.InitContainers {
			// TODO(stevej): hardcoded. k8s.InitContainerName originates in values.yaml?
			if container.Name == k8sInitContainerName {
				containsInitContainer = true
				break
			}
		}

		if containsLinkerdProxy && !containsInitContainer {
			logEntry.Debug("linkerd-cni: setting up iptables firewall")
			options := cmd.RootOptions{
				IncomingProxyPort:     conf.ProxyInit.IncomingProxyPort,
				OutgoingProxyPort:     conf.ProxyInit.OutgoingProxyPort,
				ProxyUserID:           conf.ProxyInit.ProxyUID,
				PortsToRedirect:       conf.ProxyInit.PortsToRedirect,
				InboundPortsToIgnore:  conf.ProxyInit.InboundPortsToIgnore,
				OutboundPortsToIgnore: conf.ProxyInit.OutboundPortsToIgnore,
				SimulateOnly:          conf.ProxyInit.Simulate,
				NetNs:                 args.Netns,
				UseWaitFlag:           conf.ProxyInit.UseWaitFlag,
				FirewallBinPath:       "iptables",
				FirewallSaveBinPath:   "iptables-save",
			}

			// TODO(stevej): hardcoded. This originates in values.yaml?
			k8sProxyIgnoreOutboundPortsAnnotation := TrueAsString
			// Check if there are any overridden ports to be skipped
			outboundSkipOverride, err := getAnnotationOverride(ctx, client, pod, k8sProxyIgnoreOutboundPortsAnnotation)
			if err != nil {
				logEntry.Errorf("linkerd-cni: could not retrieve overridden annotations: %s", err)
				return err
			}

			if outboundSkipOverride != "" {
				logEntry.Debugf("linkerd-cni: overriding OutboundPortsToIgnore to %s", outboundSkipOverride)
				options.OutboundPortsToIgnore = strings.Split(outboundSkipOverride, ",")
			}

			k8sProxyIgnoreInboundPortsAnnotation := TrueAsString
			inboundSkipOverride, err := getAnnotationOverride(ctx, client, pod, k8sProxyIgnoreInboundPortsAnnotation)
			if err != nil {
				logEntry.Errorf("linkerd-cni: could not retrieve overridden annotations: %s", err)
				return err
			}

			if inboundSkipOverride != "" {
				logEntry.Debugf("linkerd-cni: overriding InboundPortsToIgnore to %s", inboundSkipOverride)
				options.InboundPortsToIgnore = strings.Split(inboundSkipOverride, ",")
			}

			// TODO(stevej) hardcoded. This originates in values.yaml?
			k8sProxyUIDAnnotation := TrueAsString
			// Override ProxyUID from annotations.
			proxyUIDOverride, err := getAnnotationOverride(ctx, client, pod, k8sProxyUIDAnnotation)
			if err != nil {
				logEntry.Errorf("linkerd-cni: could not retrieve overridden annotations: %s", err)
				return err
			}

			if proxyUIDOverride != "" {
				logEntry.Debugf("linkerd-cni: overriding ProxyUID to %s", proxyUIDOverride)

				parsed, err := strconv.Atoi(proxyUIDOverride)
				if err != nil {
					logEntry.Errorf("linkerd-cni: could not parse ProxyUID to integer: %s", err)
					return err
				}

				options.ProxyUserID = parsed
			}

			k8sControllerComponentLabel := "controller-component"
			if pod.GetLabels()[k8sControllerComponentLabel] != "" {
				// Skip 443 outbound port if its a control plane component
				logEntry.Debug("linkerd-cni: adding 443 to OutboundPortsToIgnore as its a control plane component")
				options.OutboundPortsToIgnore = append(options.OutboundPortsToIgnore, "443")
			}

			firewallConfiguration, err := cmd.BuildFirewallConfiguration(&options)
			if err != nil {
				logEntry.Errorf("linkerd-cni: could not create a Firewall Configuration from the options: %v", options)
				return err
			}

			err = iptables.ConfigureFirewall(*firewallConfiguration)
			if err != nil {
				logEntry.Errorf("linkerd-cni: could not configure firewall: %s", err)
				return err
			}
		} else {
			if containsInitContainer {
				logEntry.Debug("linkerd-cni: linkerd-init initContainer is present, skipping.")
			} else {
				logEntry.Debug("linkerd-cni: linkerd-proxy is not present, skipping.")
			}
		}
	} else {
		logEntry.Debug("linkerd-cni: no Kubernetes namespace or pod name found, skipping.")
	}

	logrus.Debug("linkerd-cni: plugin is finished")
	if conf.PrevResult != nil {
		// Pass through the prevResult for the next plugin
		return types.PrintResult(conf.PrevResult, conf.CNIVersion)
	}

	logrus.Debug("linkerd-cni: no previous result to pass through, assume stand-alone run, send ok")

	return types.PrintResult(&cniv1.Result{CNIVersion: cniv1.ImplementedSpecVersion}, conf.CNIVersion)
}

func cmdCheck(args *skel.CmdArgs) error {
	logrus.Debug("linkerd-cni: cmdCheck not implemented")
	return nil
}

// cmdDel is called for DELETE requests
func cmdDel(args *skel.CmdArgs) error {
	logrus.Debug("linkerd-cni: cmdDel not implemented")
	return nil
}

func getAnnotationOverride(ctx context.Context, api *kubernetes.Clientset, pod *v1.Pod, key string) (string, error) {
	// Check if the annotation is present on the pod
	if override := pod.GetObjectMeta().GetAnnotations()[key]; override != "" {
		return override, nil
	}

	// Check if the annotation is present on the namespace
	ns, err := api.CoreV1().Namespaces().Get(ctx, pod.GetObjectMeta().GetNamespace(), metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	if override := ns.GetObjectMeta().GetAnnotations()[key]; override != "" {
		return override, nil
	}

	return "", nil
}
