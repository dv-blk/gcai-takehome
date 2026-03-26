package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// BridgeServer manages an isolated Node.js bridge + OpenCode instance.
// It spawns the bridge as a subprocess and handles cleanup on Stop().
type BridgeServer struct {
	cmd     *exec.Cmd
	port    int
	baseURL string
	tempDir string // Empty when using external OpenCode server
	logFunc func(format string, args ...interface{})
}

// StartBridge starts a Node.js bridge subprocess.
//
// If OPENCODE_SERVER_URL env var is set, the bridge connects to an external
// OpenCode server (e.g., smanx/opencode container) instead of spawning its own.
// In this mode, auth.json check and XDG isolation are skipped.
//
// If OPENCODE_SERVER_URL is not set, the bridge spawns its own OpenCode server
// internally, using auth.json from the default location with XDG isolation.
//
// logFunc is called with each line of server output (pass log.Printf or t.Logf).
func StartBridge(ctx context.Context, logFunc func(format string, args ...interface{})) (*BridgeServer, error) {
	// Check if we're connecting to an external OpenCode server
	externalOpencodeURL := os.Getenv("OPENCODE_SERVER_URL")
	useExternalOpencode := externalOpencodeURL != ""

	// Check prerequisites
	if !useExternalOpencode {
		// Only need opencode binary if we're spawning our own server
		if !IsOpenCodeInstalled() {
			return nil, fmt.Errorf("opencode not found in PATH")
		}
	}
	if !IsNodeInstalled() {
		return nil, fmt.Errorf("node not found in PATH")
	}

	// Ensure node_modules exists
	bridgeDir := findBridgeDir()
	nodeModules := filepath.Join(bridgeDir, "node_modules")
	if _, err := os.Stat(nodeModules); os.IsNotExist(err) {
		logFunc("Running npm install in %s...", bridgeDir)
		npmCmd := exec.CommandContext(ctx, "npm", "install")
		npmCmd.Dir = bridgeDir
		if out, err := npmCmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("npm install failed: %w\n%s", err, string(out))
		}
	}

	var tempDir string
	var cmdEnv []string

	if useExternalOpencode {
		// External mode: no XDG isolation needed, bridge connects to external server
		logFunc("Using external OpenCode server at %s", externalOpencodeURL)
		cmdEnv = os.Environ()
	} else {
		// Embedded mode: create isolated XDG directories and copy auth.json
		defaultAuthPath := GetDefaultAuthPath()
		if _, err := os.Stat(defaultAuthPath); os.IsNotExist(err) {
			return nil, fmt.Errorf("auth.json not found at %s - please authenticate with opencode first", defaultAuthPath)
		}

		var err error
		tempDir, err = os.MkdirTemp("", "opencode-test-*")
		if err != nil {
			return nil, fmt.Errorf("failed to create temp dir: %w", err)
		}

		dataDir := filepath.Join(tempDir, "opencode")
		configDir := filepath.Join(tempDir, "config", "opencode")

		if err := os.MkdirAll(dataDir, 0755); err != nil {
			os.RemoveAll(tempDir)
			return nil, fmt.Errorf("failed to create data dir: %w", err)
		}
		if err := os.MkdirAll(configDir, 0755); err != nil {
			os.RemoveAll(tempDir)
			return nil, fmt.Errorf("failed to create config dir: %w", err)
		}

		// Copy auth.json from default location to isolated data dir
		if err := copyFile(defaultAuthPath, filepath.Join(dataDir, "auth.json")); err != nil {
			os.RemoveAll(tempDir)
			return nil, fmt.Errorf("failed to copy auth.json: %w", err)
		}

		cmdEnv = append(os.Environ(),
			fmt.Sprintf("XDG_DATA_HOME=%s", tempDir),
			fmt.Sprintf("XDG_CONFIG_HOME=%s", filepath.Join(tempDir, "config")),
			fmt.Sprintf("XDG_STATE_HOME=%s", filepath.Join(tempDir, "state")),
			fmt.Sprintf("XDG_CACHE_HOME=%s", filepath.Join(tempDir, "cache")),
		)
	}

	// Find available port for the bridge
	bridgePort, err := findAvailablePort()
	if err != nil {
		if tempDir != "" {
			os.RemoveAll(tempDir)
		}
		return nil, fmt.Errorf("failed to find available port: %w", err)
	}

	// Build bridge command arguments
	bridgeScript := filepath.Join(bridgeDir, "bridge.mjs")
	bridgeArgs := []string{
		bridgeScript,
		fmt.Sprintf("--port=%d", bridgePort),
		"--log-level=WARN",
	}
	if useExternalOpencode {
		bridgeArgs = append(bridgeArgs, fmt.Sprintf("--opencode-url=%s", externalOpencodeURL))
	}

	// Start the Node.js bridge
	cmd := exec.CommandContext(ctx, "node", bridgeArgs...)
	cmd.Dir = bridgeDir
	cmd.Env = cmdEnv

	// Capture stdout (bridge ready signal) and stderr (logs)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		if tempDir != "" {
			os.RemoveAll(tempDir)
		}
		return nil, fmt.Errorf("failed to start bridge: %w", err)
	}

	// Stream stderr (bridge + opencode logs) to logFunc in background
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			if logFunc != nil {
				logFunc("[bridge] %s", scanner.Text())
			}
		}
	}()

	// Wait for "bridge listening on http://..." on stdout
	baseURL, err := waitForBridgeReady(ctx, stdout, 60*time.Second)
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		if tempDir != "" {
			os.RemoveAll(tempDir)
		}
		return nil, fmt.Errorf("bridge failed to start: %w", err)
	}

	// Verify bridge is responding
	if err := waitForHTTP(ctx, baseURL+"/health", 30*time.Second); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		if tempDir != "" {
			os.RemoveAll(tempDir)
		}
		return nil, fmt.Errorf("bridge health check failed: %w", err)
	}

	return &BridgeServer{
		cmd:     cmd,
		port:    bridgePort,
		baseURL: baseURL,
		tempDir: tempDir,
		logFunc: logFunc,
	}, nil
}

// BaseURL returns the bridge's base URL
func (bs *BridgeServer) BaseURL() string {
	return bs.baseURL
}

// Port returns the bridge's port
func (bs *BridgeServer) Port() int {
	return bs.port
}

// Stop stops the bridge (and its managed OpenCode server) and cleans up
func (bs *BridgeServer) Stop() {
	if bs.cmd != nil && bs.cmd.Process != nil {
		bs.cmd.Process.Kill()
		bs.cmd.Wait()
	}
	if bs.tempDir != "" {
		os.RemoveAll(bs.tempDir)
	}
}

// waitForBridgeReady reads stdout until "bridge listening on ..." appears
func waitForBridgeReady(ctx context.Context, stdout io.Reader, timeout time.Duration) (string, error) {
	done := make(chan string, 1)
	errCh := make(chan error, 1)

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "bridge listening on ") {
				url := strings.TrimPrefix(line, "bridge listening on ")
				done <- strings.TrimSpace(url)
				return
			}
		}
		if err := scanner.Err(); err != nil {
			errCh <- err
		} else {
			errCh <- fmt.Errorf("bridge stdout closed without ready signal")
		}
	}()

	select {
	case url := <-done:
		return url, nil
	case err := <-errCh:
		return "", err
	case <-time.After(timeout):
		return "", fmt.Errorf("timeout waiting for bridge to start after %v", timeout)
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// waitForHTTP polls a URL until it returns a non-5xx response
func waitForHTTP(ctx context.Context, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
		}

		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("URL %s did not respond within %v", url, timeout)
}

// findBridgeDir locates the opencode-bridge directory relative to this source file
func findBridgeDir() string {
	// Try relative to working directory first
	candidates := []string{
		"opencode-bridge",
		"../opencode-bridge",
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(c, "bridge.mjs")); err == nil {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}
	// Fallback: assume working directory is project root
	return filepath.Join(".", "opencode-bridge")
}

// findAvailablePort finds an available TCP port
func findAvailablePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, srcInfo.Mode())
}

// IsOpenCodeInstalled checks if opencode is available in PATH
func IsOpenCodeInstalled() bool {
	_, err := exec.LookPath("opencode")
	return err == nil
}

// IsNodeInstalled checks if node is available in PATH
func IsNodeInstalled() bool {
	_, err := exec.LookPath("node")
	return err == nil
}

// GetDefaultAuthPath returns the default path to auth.json
func GetDefaultAuthPath() string {
	return filepath.Join(os.Getenv("HOME"), ".local", "share", "opencode", "auth.json")
}

// HasCopilotAuth checks if Copilot authentication is configured
func HasCopilotAuth() bool {
	authPath := GetDefaultAuthPath()
	data, err := os.ReadFile(authPath)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "github-copilot")
}
