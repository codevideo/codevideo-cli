package staticserver

import (
	"context"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
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

// The runners open /v3?uuid=... and the page then loads its assets from _next,
// which Go's embed skips unless the directive says all:. Serve the embedded site
// the way StartServer does and fetch everything the page asks for.
func TestEmbeddedSiteServesTheRenderPageAndItsAssets(t *testing.T) {
	subFS, err := fs.Sub(public, "public")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.FileServer(http.FS(subFS)))
	defer server.Close()

	get := func(path string) string {
		t.Helper()
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: %s", path, response.Status)
		}
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}

	get("/") // waitForServerReady polls the root.
	page := get("/v3?uuid=test")
	if !strings.Contains(page, `id="__next"`) {
		t.Fatalf("/v3 did not return the render page: %.200s", page)
	}
	assets := regexp.MustCompile(`/_next/static/[^"\\]+\.(?:js|css)`).FindAllString(page, -1)
	if len(assets) == 0 {
		t.Fatal("the render page references no _next assets")
	}
	for _, asset := range assets {
		get(asset)
	}
}
