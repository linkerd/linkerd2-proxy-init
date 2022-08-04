package iptablestest

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	ignoredContainerPort     = "7070"
	proxyContainerPort       = "8080"
	notTheProxyContainerPort = "9090"
)

func TestMain(m *testing.M) {
	runTests := flag.Bool("integration-tests", false, "must be provided to run the integration tests")
	flag.Parse()

	if !*runTests {
		fmt.Fprintln(os.Stderr, "integration tests not enabled: enable with -integration-tests")
		os.Exit(0)
	}

	os.Exit(m.Run())
}

func TestPodWithNoRules(t *testing.T) {
	t.Parallel()

	svcName := "svc-pod-with-no-rules"
	podIP := os.Getenv("POD_WITH_NO_RULES_IP")
	if podIP == "" {
		t.Skipf("POD_WITH_NO_RULES_IP is not set")
	}

	t.Run("succeeds connecting to pod directly through container's exposed port", func(t *testing.T) {
		expectSuccessfulGetRequest(t, makeURL(podIP, proxyContainerPort))
	})

	t.Run("fails to connect to pod directly through any port that isn't the container's exposed port", func(t *testing.T) {
		for _, port := range []string{"8088", "8888", "8988"} {
			expectCannotConnectGetRequest(t, makeURL(podIP, port))
		}
	})

	t.Run("succeeds connecting to pod via a service through container's exposed port", func(t *testing.T) {
		expectSuccessfulGetRequest(t, makeURL(svcName, proxyContainerPort))
	})

	t.Run("fails to connect to pod via a service through any port that isn't the container's exposed port", func(t *testing.T) {
		for _, port := range []string{"8088", "8888", "8988"} {
			expectCannotConnectGetRequest(t, makeURL(svcName, port))
		}
	})
}

func TestPodRedirectsAllPorts(t *testing.T) {
	t.Parallel()

	svcName := "svc-pod-redirects-all-ports"
	podIP := os.Getenv("POD_REDIRECTS_ALL_PORTS_IP")
	if podIP == "" {
		t.Skipf("POD_REDIRECTS_ALL_PORTS_IP is not set")
	}

	t.Run("succeeds connecting to pod directly through container's exposed port", func(t *testing.T) {
		expectSuccessfulGetRequest(t, makeURL(podIP, proxyContainerPort))
	})

	t.Run("succeeds connecting to pod directly through any port that isn't the container's exposed port", func(t *testing.T) {
		for _, port := range []string{"8088", "8888", "8988"} {
			expectSuccessfulGetRequest(t, makeURL(podIP, port))
		}
	})

	t.Run("succeeds connecting to pod via a service through container's exposed port", func(t *testing.T) {
		expectSuccessfulGetRequest(t, makeURL(svcName, proxyContainerPort))
	})

	t.Run("fails to connect to pod via a service through any port that isn't the container's exposed port", func(t *testing.T) {
		for _, port := range []string{"8088", "8888", "8988"} {
			expectCannotConnectGetRequest(t, makeURL(svcName, port))
		}
	})
}

func TestPodWithSomePortsRedirected(t *testing.T) {
	t.Parallel()

	podIP := os.Getenv("POD_REDIRECTS_WHITELISTED_IP")
	if podIP == "" {
		t.Skipf("POD_REDIRECTS_WHITELISTED_IP is not set")
	}

	t.Run("succeeds connecting to pod directly through container's exposed port", func(t *testing.T) {
		expectSuccessfulGetRequest(t, makeURL(podIP, proxyContainerPort))
	})

	t.Run("succeeds connecting to pod directly through ports configured to redirect", func(t *testing.T) {
		for _, port := range []string{"9090", "9099"} {
			expectSuccessfulGetRequest(t, makeURL(podIP, port))
		}
	})

	t.Run("fails to connect to pod via through any port that isn't configured to redirect", func(t *testing.T) {
		for _, port := range []string{"8088", "8888", "8988"} {
			expectCannotConnectGetRequest(t, makeURL(podIP, port))
		}
	})
}

func TestPodWithSomePortsIgnored(t *testing.T) {
	t.Parallel()

	podIP := os.Getenv("POD_DOESNT_REDIRECT_BLACKLISTED_IP")
	if podIP == "" {
		t.Skipf("POD_DOESNT_REDIRECT_BLACKLISTED_IP is not set")
	}

	t.Run("succeeds connecting to pod directly through container's exposed port", func(t *testing.T) {
		expectSuccessfulGetRequest(t, makeURL(podIP, proxyContainerPort))
	})

	t.Run("succeeds connecting to pod directly through ports configured to redirect", func(t *testing.T) {
		for _, port := range []string{"9090", "9099"} {
			expectSuccessfulGetRequest(t, makeURL(podIP, port))
		}
	})

	t.Run("doesnt redirect when through port that is ignored", func(t *testing.T) {
		rsp := expectSuccessfulGetRequest(t, makeURL(podIP, ignoredContainerPort))
		if rsp == "proxy" {
			t.Fatalf("Expected connection through ignored port to directly hit service, but hit [%s]", rsp)
		}
		if !strings.Contains(rsp, ignoredContainerPort) {
			t.Fatalf("Expected to be able to connect to %s without redirects, but got back %s", ignoredContainerPort, rsp)
		}
	})
}

func TestPodMakesOutboundConnection(t *testing.T) {
	t.Parallel()

	proxyPodName := "pod-doesnt-redirect-blacklisted"
	proxyPodIP := os.Getenv("POD_DOESNT_REDIRECT_BLACKLISTED_IP")
	if proxyPodIP == "" {
		t.Skipf("POD_DOESNT_REDIRECT_BLACKLISTED_IP is not set")
	}

	targetPodName := "pod-with-no-rules"
	targetPodIP := os.Getenv("POD_WITH_NO_RULES_IP")
	if targetPodIP == "" {
		t.Skipf("POD_WITH_NO_RULES_IP is not set")
	}

	t.Run("connecting to another pod from non-proxy container gets redirected to proxy", func(t *testing.T) {
		rsp := makeCallFromContainerToAnother(t, proxyPodIP, ignoredContainerPort, targetPodIP, ignoredContainerPort)
		expected := fmt.Sprintf("me:[%s:%s]downstream:[proxy]", proxyPodName, ignoredContainerPort)
		if !strings.Contains(rsp, expected) {
			t.Fatalf("Expected response to be redirected to the proxy, expected %s but it was %s", expected, rsp)
		}
	})

	t.Run("connecting to another pod from proxy container does not get redirected to proxy", func(t *testing.T) {
		rsp := makeCallFromContainerToAnother(t, proxyPodIP, proxyContainerPort, targetPodIP, notTheProxyContainerPort)
		expected := fmt.Sprintf("me:[proxy]downstream:[%s:%s]", targetPodName, notTheProxyContainerPort)
		if !strings.Contains(rsp, expected) {
			t.Fatalf("Expected response not to be redirected to the proxy, expected %s but it was %s", expected, rsp)
		}
	})

	t.Run("connecting to loopback from non-proxy container does not get redirected to proxy", func(t *testing.T) {
		rsp := makeCallFromContainerToAnother(t, proxyPodIP, ignoredContainerPort, "127.0.0.1", notTheProxyContainerPort)
		expected := fmt.Sprintf("me:[%s:%s]downstream:[%s:%s]", proxyPodName, ignoredContainerPort, proxyPodName, notTheProxyContainerPort)
		if !strings.Contains(rsp, expected) {
			t.Fatalf("Expected response not to be redirected to the proxy, expected %s but it was %s", expected, rsp)
		}
	})
}

func TestPodWithSomeSubnetsIgnored(t *testing.T) {
	t.Parallel()

	podIP := os.Getenv("POD_IGNORES_SUBNETS_IP")
	if podIP == "" {
		t.Skipf("POD_IGNORES_SUBNETS_IP is not set")
	}

	t.Run("connecting to a not-a-proxy-container should bypass proxy container", func(t *testing.T) {
		rsp := expectSuccessfulGetRequest(t, makeURL(podIP, notTheProxyContainerPort))
		expected := fmt.Sprintf("pod-ignores-subnets:%s", notTheProxyContainerPort)
		if !strings.Contains(rsp, expected) {
			t.Fatalf("Expected response to be bypassed, expected %s but it was %s", expected, rsp)
		}
	})

	t.Run("connecting directly to the proxy container pod should still work", func(t *testing.T) {
		rsp := expectSuccessfulGetRequest(t, makeURL(podIP, proxyContainerPort))
		expected := "proxy"
		if !strings.Contains(rsp, expected) {
			t.Fatalf("Expected response from the proxy container, expected %s but it was %s", expected, rsp)
		}
	})
}

// === Helpers ===

func makeCallFromContainerToAnother(t *testing.T, viaHost, viaPort, targetHost, targetPort string) string {
	t.Helper()
	targetURL := makeURL(targetHost, targetPort)
	url := fmt.Sprintf("%scall?url=%s", makeURL(viaHost, viaPort), url.QueryEscape(targetURL))
	return expectSuccessfulGetRequest(t, url)
}

func makeURL(host, port string) string {
	return fmt.Sprintf("http://%s/", net.JoinHostPort(host, port))
}

func client() *http.Client {
	d := &net.Dialer{
		Timeout: 1 * time.Second,
	}
	return &http.Client{
		Transport: &http.Transport{
			Dial: d.Dial,
		},
	}
}

func expectCannotConnectGetRequest(t *testing.T, url string) {
	t.Helper()
	t.Logf("Expecting failed GET to %s\n", url)
	resp, err := client().Get(url)
	if err == nil {
		t.Fatalf("Expected error when connecting to %s, got:\n%+v", url, resp)
	}
}

func expectSuccessfulGetRequest(t *testing.T, url string) string {
	t.Helper()
	t.Logf("Expecting successful GET to %s\n", url)
	resp, err := client().Get(url)
	if err != nil {
		t.Fatalf("failed to send HTTP GET to %s:\n%v", url, err)
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed reading GET response from %s:\n%v", url, err)
	}
	response := string(body)
	t.Logf("Response from %s: %s", url, response)
	return response
}
