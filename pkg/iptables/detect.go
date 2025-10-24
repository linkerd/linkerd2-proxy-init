// Copyright (c) Linkerd authors
//
// Portions of the iptables backend detection code are derived from:
// https://github.com/projectcalico/calico/blob/master/felix/environment/feature_detect_linux.go
// Copyright (c) 2018-2025 Tigera, Inc. All rights reserved.
// Licensed under the Apache License, Version 2.0

package iptables

import (
	"bytes"
	"os/exec"
	"strings"

	log "github.com/sirupsen/logrus"
)

func countRulesInIptableOutput(in []byte) int {
	count := 0
	for _, x := range bytes.Split(in, []byte("\n")) {
		if len(x) >= 1 && x[0] == '-' {
			count++
		}
	}
	return count
}

// hasKubernetesChains tries to find in the output of the binary if the Kubernetes
// chains exists
func hasKubernetesChains(output []byte) bool {
	return strings.Contains(string(output), "KUBE-IPTABLES-HINT") || strings.Contains(string(output), "KUBE-KUBELET-CANARY")
}

// DetectBackend attempts to detect the iptables backend (nft or legacy) and the
// appropriate iptables commands to use. If the detected backend does not match
// the specifiedBackend, a warning is logged but the specifiedBackend is used.
// The specifiedBackend can be "nft", "legacy", "plain", or "auto" (to use the
// detected backend).
//
// Based on the backend selected, the appropriate iptables command paths are set
// in the FirewallConfiguration.
//
// This logic is adapted from the Calico CNI plugin:
// https://github.com/projectcalico/calico/blob/master/felix/environment/feature_detect_linux.go
func DetectBackend(fc *FirewallConfiguration, lookPath func(file string) (string, error), ipv6 bool, specifiedBackend string) {
	prefix := "iptables"
	if ipv6 {
		prefix = "ip6tables"
	}

	nftSave := FindBestBinary(lookPath, prefix, "nft", "save")
	lgcySave := FindBestBinary(lookPath, prefix, "legacy", "save")
	nftCmd := strings.TrimSuffix(nftSave, "-save")
	lgcyCmd := strings.TrimSuffix(lgcySave, "-save")

	// Check kubelet canary chains in the mangle table for nft first.
	nftMangle, _ := executeCommand(*fc, exec.Command(nftSave, "-t", "mangle"))

	var detectedBackend string
	if hasKubernetesChains(nftMangle) {
		detectedBackend = "nft"
	} else {
		// Check legacy mangle next.
		lgcyMangle, _ := executeCommand(*fc, exec.Command(lgcySave, "-t", "mangle"))

		if hasKubernetesChains(lgcyMangle) {
			detectedBackend = "legacy"
		} else {
			// Fall back to comparing total rule counts between full legacy vs nft saves.
			lgcyAll, _ := executeCommand(*fc, exec.Command(lgcySave))
			nftAll, _ := executeCommand(*fc, exec.Command(nftSave))

			legacyLines := countRulesInIptableOutput(lgcyAll)
			nftLines := countRulesInIptableOutput(nftAll)
			if legacyLines >= nftLines {
				detectedBackend = "legacy" // default to legacy when tied or more legacy rules
			} else {
				detectedBackend = "nft"
			}
		}
	}

	// Decide which backend to use, honoring a specific request if provided.
	requested := strings.ToLower(specifiedBackend)
	backendToUse := requested
	if requested == "auto" {
		log.WithField("detectedBackend", detectedBackend).Debug("Detected Iptables backend")
		backendToUse = detectedBackend
	} else if requested != detectedBackend {
		log.WithFields(log.Fields{
			"detectedBackend":  detectedBackend,
			"specifiedBackend": requested,
		}).Warn("Iptables backend specified does not match the detected backend; honoring specified backend")
	} else {
		log.WithField("specifiedBackend", specifiedBackend).Debug("Iptables backend specified matches detected backend")
	}

	switch backendToUse {
	case "legacy":
		fc.BinPath = lgcyCmd
		fc.SaveBinPath = lgcySave
	case "nft":
		fc.BinPath = nftCmd
		fc.SaveBinPath = nftSave
	case "plain":
		fc.BinPath = prefix
		fc.SaveBinPath = prefix + "-save"
	}

	log.WithFields(log.Fields{
		"binPath":     fc.BinPath,
		"saveBinPath": fc.SaveBinPath,
	}).Debug("Using iptables commands")
}

// FindBestBinary tries to find an iptables binary for the specific variant (legacy/nftables mode) and returns the name
// of the binary.  Falls back on iptables-restore/iptables-save if the specific variant isn't available.
// Panics if no binary can be found.
func FindBestBinary(lookPath func(file string) (string, error), prefix, backendMode, saveOrRestore string) string {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	candidates := []string{
		prefix + "-" + backendMode + "-" + saveOrRestore,
		prefix + "-" + saveOrRestore,
	}

	logCxt := log.WithFields(log.Fields{
		"prefix":        prefix,
		"backendMode":   backendMode,
		"saveOrRestore": saveOrRestore,
		"candidates":    candidates,
	})

	for _, candidate := range candidates {
		_, err := lookPath(candidate)
		if err == nil {
			logCxt.WithField("command", candidate).Debug("Looked up iptables command")
			return candidate
		}
	}

	logCxt.Panic("Failed to find iptables command")
	return ""
}
