package staticserver

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestStartServerReportsOccupiedStaticPort(t *testing.T) {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}

	server, err := NewServer(port, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	err = server.StartServer(context.Background())
	if err == nil || !strings.Contains(err.Error(), "static server port") {
		t.Fatalf("expected occupied port error, got %v", err)
	}
}
