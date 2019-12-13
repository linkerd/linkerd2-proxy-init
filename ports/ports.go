package ports

import (
	"fmt"
	"strconv"
	"strings"
)

// PortRange defines the upper- and lower-bounds for a range of ports.
type PortRange struct {
	LowerBound int
	UpperBound int
}

// IsValid checks the provided to determine whether or not the port candidate
// is a valid TCP port number. Valid TCP ports range from 0 to 65535.
func IsValid(port int) bool {
	return port >= 0 && port <= 65535
}

// ParsePort parses and verifies the validity of the port candidate.
func ParsePort(port string) (parsed int, err error) {
	i, err := strconv.Atoi(port)
	if err != nil || !IsValid(i) {
		return -1, fmt.Errorf("\"%s\" is not a valid TCP port", port)
	}
	return i, nil
}

// ParsePortRange parses and checks the provided range candidate to ensure it is a valid TCP port range.
func ParsePortRange(portRange string) (parsed PortRange, err error) {
	bounds := strings.Split(portRange, "-")
	if len(bounds) != 2 {
		return PortRange{}, fmt.Errorf("ranges expected as <lower>-<upper>")
	}
	lower, err := strconv.Atoi(bounds[0])
	if err != nil || !IsValid(lower) {
		return PortRange{}, fmt.Errorf("\"%s\" is not a valid lower-bound", bounds[0])
	}
	upper, err := strconv.Atoi(bounds[1])
	if err != nil || !IsValid(upper) {
		return PortRange{}, fmt.Errorf("\"%s\" is not a valid upper-bound", bounds[1])
	}
	if upper < lower {
		return PortRange{}, fmt.Errorf("\"%s\": upper-bound must be greater than or equal to lower-bound", portRange)
	}
	return PortRange{LowerBound: lower, UpperBound: upper}, nil
}
