package staticserver

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	log "github.com/sirupsen/logrus"

	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/codevideo/codevideo-cli/constants"
)

// Server serves static files from the embedded public folder.
type Server struct {
	Port           int
	serverURL      string
	startupTimeout time.Duration
	httpServer     *http.Server
	manifestServer *http.Server
}

//go:embed public
var public embed.FS

// Start creates and starts a new HTTP server that serves the embedded public folder.
func Start(ctx context.Context) (*Server, error) {

	// Create a new server instance.
	srv, err := NewServer(constants.DEFAULT_GATSBY_PORT, constants.DEFAULT_SERVER_TIMEOUT)
	if err != nil {
		return nil, fmt.Errorf("failed to create server: %w", err)
	}
	log.Printf("Starting server on port %d", srv.Port)

	// Start the embedded HTTP server.
	if err := srv.StartServer(ctx); err != nil {
		srv.Stop()
		return nil, fmt.Errorf("failed to start server: %w", err)
	}

	// Wait for the server to be ready.
	if err := srv.waitForServerReady(ctx); err != nil {
		srv.Stop()
		return nil, fmt.Errorf("server startup error: %w", err)
	}
	log.Printf("Server is ready at %s", srv.GetURL())

	return srv, nil
}

// NewServer creates a new Server instance.
func NewServer(startPort int, timeout time.Duration) (*Server, error) {
	serverURL := fmt.Sprintf("http://localhost:%d", startPort)
	return &Server{
		Port:           startPort,
		serverURL:      serverURL,
		startupTimeout: timeout,
	}, nil
}

// GetURL returns the URL of the running server.
func (s *Server) GetURL() string {
	return s.serverURL
}

// GetURLWithParams returns the URL with query parameters (e.g. for video recording pages).
func (s *Server) GetURLWithParams(uuid string) string {
	return fmt.Sprintf("%s/v3?uuid=%s", s.serverURL, uuid)
}

// StartServer starts the embedded HTTP server that serves the static files.
func (s *Server) StartServer(ctx context.Context) error {
	// Create a sub-filesystem that removes the "public" prefix.
	subFS, err := fs.Sub(public, "public")
	if err != nil {
		return fmt.Errorf("failed to create sub filesystem: %w", err)
	}

	// Create a file server using the subFS.
	fileServer := http.FileServer(http.FS(subFS))

	// Wrap the fileServer with CORS middleware.
	staticHandler := corsMiddleware(fileServer)

	// Set up the HTTP server for static assets.
	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.Port),
		Handler: staticHandler,
	}

	// Manifest server: Create a new mux dedicated to the manifest route.
	manifestMux := http.NewServeMux()
	manifestMux.HandleFunc("/get-manifest-v3", getManifestV3Handler)
	s.manifestServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", constants.DEFAULT_MANIFEST_SERVER_PORT),
		Handler: corsMiddleware(manifestMux),
	}

	// Start the servers in a separate goroutine.
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("HTTP server error: %v\n", err)
		}
	}()

	go func() {
		if err := s.manifestServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("Manifest server error: %v\n", err)
		}
	}()

	return nil
}

// Stop gracefully shuts down the HTTP server.
func (s *Server) Stop() error {
	if s.httpServer != nil {
		// Give the server a few seconds to shut down gracefully.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.httpServer.Shutdown(ctx); err != nil {
			return fmt.Errorf("failed to shutdown server: %w", err)
		}
	}
	return nil
}

// waitForServerReady polls the server URL until it's responding or times out.
func (s *Server) waitForServerReady(ctx context.Context) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	timeoutCtx, cancel := context.WithTimeout(ctx, s.startupTimeout)
	defer cancel()

	for {
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("server startup timed out after %v", s.startupTimeout)
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, "GET", s.serverURL, nil)
			if err != nil {
				continue
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				continue
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil // Server is ready.
			}
		}
	}
}

func getManifestV3Handler(w http.ResponseWriter, r *http.Request) {
	// Get the UUID from query parameters
	uuid := r.URL.Query().Get("uuid")
	log.Println("return manifest for uuid", uuid)

	// get absolute path of 'new' folder
	newAbsPath, err := filepath.Abs(constants.NEW_FOLDER)
	if err != nil {
		log.Fatalf("Error getting absolute path of %s: %v", constants.NEW_FOLDER, err)
	}

	// get absolute path of 'success' folder
	successAbsPath, err := filepath.Abs(constants.SUCCESS_FOLDER)
	if err != nil {
		log.Fatalf("Error getting absolute path of %s: %v", constants.SUCCESS_FOLDER, err)
	}

	// Try to find the file in the new folder first
	file := filepath.Join(newAbsPath, fmt.Sprintf("%s.json", uuid))
	if _, err := os.Stat(file); os.IsNotExist(err) {
		// If not found, try the success folder
		file = filepath.Join(successAbsPath, fmt.Sprintf("%s.json", uuid))
		if _, err := os.Stat(file); os.IsNotExist(err) {
			// Return not found if not found in either folder
			http.Error(w, "Manifest not found", http.StatusNotFound)
			return
		}
	}

	// Serve the file
	http.ServeFile(w, r, file)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow all origins!
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// Handle preflight requests
		if r.Method == http.MethodOptions {
			return
		}
		next.ServeHTTP(w, r)
	})
}
