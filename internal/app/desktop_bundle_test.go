package app

import (
	"bytes"
	"encoding/json"
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

func TestAppBundleOpenedOutsideProjectLoadsLibrary(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root: %v", err)
	}
	built := filepath.Join(t.TempDir(), "juku-built")
	build := exec.Command("go", "build", "-o", built, ".")
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}

	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module juku\n\ngo 1.24.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	title := "从应用包启动能看到的短剧"
	if err := writeLibraryCache(filepath.Join(project, "data"), libraryCache{Dramas: []Drama{
		{ID: "hongguo:7000000000000000099", Source: sourceHongguo, Title: title, TotalEpisode: 3},
	}, LoadedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	appDir := filepath.Join(project, "build", "bin", "果果剧库.app", "Contents", "MacOS")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(appDir, "juku")
	if err := copyFile(built, binary, 0o755); err != nil {
		t.Fatal(err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()

	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-open=false", "-listen", addr, "-ui=true", "-http=false")
	cmd.Dir = outside
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var logs bytes.Buffer
	go func() {
		_, _ = io.Copy(&syncWriter{mu: &mu, buf: &logs}, output)
	}()
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()
	logText := func() string {
		mu.Lock()
		defer mu.Unlock()
		return logs.String()
	}

	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(45 * time.Second)
	var body []byte
	var last error
	for time.Now().Before(deadline) {
		response, err := client.Get("http://" + addr + "/api/desktop/catalog")
		if err != nil {
			last = err
			time.Sleep(200 * time.Millisecond)
			continue
		}
		body, _ = io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode == http.StatusOK && strings.Contains(string(body), title) {
			last = nil
			break
		}
		last = fmt.Errorf("status %d body %s", response.StatusCode, body)
		time.Sleep(200 * time.Millisecond)
	}
	if last != nil {
		t.Fatalf("opened app did not list the project library: %v\n%s", last, logText())
	}
	var catalog struct {
		Data []struct {
			Title string `json:"title"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Data) != 1 || catalog.Data[0].Title != title {
		t.Fatalf("catalog = %#v", catalog.Data)
	}
}

func copyFile(from, to string, mode os.FileMode) error {
	body, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	return os.WriteFile(to, body, mode)
}
