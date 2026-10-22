package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/sirupsen/logrus"
)

func TestCmdAddLogsParseConfigErrorsClearly(t *testing.T) {
	logger := logrus.StandardLogger()
	originalOut := logger.Out
	originalFormatter := logger.Formatter
	originalLevel := logger.Level

	var logs bytes.Buffer
	logger.SetOutput(&logs)
	logger.SetFormatter(&logrus.TextFormatter{
		DisableTimestamp: true,
		DisableQuote:     true,
	})
	logger.SetLevel(logrus.DebugLevel)

	defer func() {
		logger.SetOutput(originalOut)
		logger.SetFormatter(originalFormatter)
		logger.SetLevel(originalLevel)
	}()

	err := cmdAdd(&skel.CmdArgs{StdinData: []byte("{")})
	if err == nil {
		t.Fatal("expected cmdAdd to fail for invalid JSON")
	}

	output := logs.String()
	if !strings.Contains(output, "msg=error parsing config") {
		t.Fatalf("expected parse error log, got %q", output)
	}
	if !strings.Contains(output, "error=linkerd-cni: failed to parse network configuration: unexpected end of JSON input") {
		t.Fatalf("expected structured parse error field in log, got %q", output)
	}
	if strings.Contains(output, "%!e(") {
		t.Fatalf("expected formatted error message, got %q", output)
	}
}
