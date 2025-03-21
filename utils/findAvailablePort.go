package utils

import (
	"fmt"
	"net"
	"strconv"
)

// FindAvailablePort tries to find an available port starting from the given port
// and incrementing until it finds one
func FindAvailablePort(startPort int) (int, error) {
	// Try ports in range from startPort to startPort+1000
	for port := startPort; port < startPort+1000; port++ {
		available, err := isPortAvailable(port)
		if err != nil {
			return 0, err
		}
		if available {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available ports found in range %d-%d", startPort, startPort+1000)
}

// isPortAvailable checks if a port is available by attempting to listen on it
func isPortAvailable(port int) (bool, error) {
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		// Port is likely in use
		return false, nil
	}
	ln.Close()
	return true, nil
}
