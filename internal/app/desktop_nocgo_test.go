package app

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCGODisabledBuildServesHTTP(t *testing.T) {
	assertBuiltUIServesHTTP(t, []string{"CGO_ENABLED=0"}, true)
}

func TestPlainBuildWithoutWailsTagServesHTTP(t *testing.T) {
	assertBuiltUIServesHTTP(t, nil, false)
}

func assertBuiltUIServesHTTP(t *testing.T, extraEnv []string, forbidTagError bool) {
	t.Helper()
	root := filepath.Clean(filepath.Join("..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "juku")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = root
	build.Env = append(os.Environ(), extraEnv...)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build %s: %v\n%s", strings.Join(extraEnv, " "), err, output)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()

	cmd := exec.Command(bin, "-data-dir", t.TempDir(), "-open=false", "-listen", addr, "-ui=true", "-http=false")
	cmd.Dir = root
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&syncWriter{mu: &mu, buf: &buf}, stdout)
		close(done)
	}()
	logText := func() string {
		mu.Lock()
		defer mu.Unlock()
		return buf.String()
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-done
		_ = cmd.Wait()
	}()

	client := &http.Client{Timeout: 2 * time.Second}
	pageURL := "http://" + addr + "/"
	deadline := time.Now().Add(45 * time.Second)
	var page string
	var last error
	for time.Now().Before(deadline) {
		select {
		case <-done:
			t.Fatalf("process exited before HTTP UI\n%s", logText())
		default:
		}
		response, err := client.Get(pageURL)
		if err != nil {
			last = err
			time.Sleep(200 * time.Millisecond)
			continue
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		page = string(body)
		if response.StatusCode == http.StatusOK && strings.Contains(page, "果果剧库") {
			last = nil
			break
		}
		last = fmt.Errorf("status %d", response.StatusCode)
		time.Sleep(200 * time.Millisecond)
	}
	if last != nil || !strings.Contains(page, "果果剧库") {
		t.Fatalf("HTTP UI did not start: %v\n%s", last, logText())
	}
	if forbidTagError && strings.Contains(logText(), "will not build without the correct build tags") {
		t.Fatalf("CGO_ENABLED=0 binary called wails.Run\n%s", logText())
	}
	if strings.Contains(logText(), "will not build without the correct build tags") && !strings.Contains(logText(), "管理界面已启动") {
		t.Fatalf("wails build-tag error replaced the HTTP UI\n%s", logText())
	}
}

type syncWriter struct {
	mu  *sync.Mutex
	buf *bytes.Buffer
}

func (writer syncWriter) Write(p []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.buf.Write(p)
}
