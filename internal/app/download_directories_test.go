package app

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestDownloadGroupingKeepsExistingDirectoriesAcrossChanges(t *testing.T) {
	cfg := defaultConfig()
	cfg.OutputDir = t.TempDir()
	d := &Downloader{cfg: cfg}
	old := Drama{ID: historyFixtureDramaID, Source: sourceHongguo, Title: "同名剧"}
	flat, err := d.downloadDramaDirectory(old, old.Title, "")
	if err != nil || flat != filepath.Join(cfg.OutputDir, old.Title) {
		t.Fatal("default layout changed", flat, err)
	}
	sentinel := filepath.Join(flat, "001.mp4")
	if err := os.WriteFile(sentinel, []byte("original episode"), 0600); err != nil {
		t.Fatal(err)
	}
	grouped := true
	d.downloadGrouping = &grouped
	old.Title = "更新后的剧名"
	if same, err := d.downloadDramaDirectory(old, old.Title, ""); err != nil || same != flat {
		t.Fatal("enabling grouping or renaming moved an existing drama", same, err)
	}
	var groupedPaths []string
	for index, id := range []string{"hongguo:7000000000000000002", "hongguo:7000000000000000003"} {
		drama := Drama{ID: id, Source: sourceHongguo, Title: "同名剧"}
		directory, err := d.downloadDramaDirectory(drama, drama.Title, "")
		want := filepath.Join(cfg.OutputDir, "红果", drama.Title)
		if index > 0 {
			want += "_" + hashShort(drama.ID)
		}
		if err != nil || directory != want {
			t.Fatal("new drama did not use its source directory", directory, err)
		}
		groupedPaths = append(groupedPaths, directory)
	}
	if groupedPaths[0] == groupedPaths[1] {
		t.Fatal("same title from different drama IDs collided")
	}

	d = &Downloader{cfg: cfg}
	if same, err := d.downloadDramaDirectory(old, old.Title, ""); err != nil || same != flat {
		t.Fatal("restart lost the original flat directory", same, err)
	}
	for index, id := range []string{"hongguo:7000000000000000002", "hongguo:7000000000000000003"} {
		drama := Drama{ID: id, Source: sourceHongguo}
		if same, err := d.downloadDramaDirectory(drama, "再次改名", ""); err != nil || same != groupedPaths[index] {
			t.Fatal("disabling grouping moved an existing grouped drama", same, err)
		}
	}
	if body, _ := os.ReadFile(sentinel); string(body) != "original episode" {
		t.Fatal("directory changes touched existing media")
	}
}

func TestDownloadGroupingRejectsSourceSymlinksAndDramaCollision(t *testing.T) {
	for _, kind := range []string{"symlink", "existing-drama"} {
		t.Run(kind, func(t *testing.T) {
			cfg := defaultConfig()
			cfg.OutputDir, cfg.GroupBySource = t.TempDir(), true
			drama := Drama{ID: historyFixtureDramaID, Source: sourceHongguo}
			group := filepath.Join(cfg.OutputDir, dramaSourceFolder(drama))
			outside := t.TempDir()
			if kind == "symlink" {
				if err := os.Symlink(outside, group); err != nil {
					t.Skip("symlinks unavailable")
				}
			} else {
				if ok, err := claimDownloadDirectory(group, "older-drama", false); err != nil || !ok {
					t.Fatal(err)
				}
			}
			if _, err := (&Downloader{cfg: cfg}).downloadDramaDirectory(drama, "新剧", ""); err == nil {
				t.Fatal("unsafe source parent accepted")
			}
			if entries, _ := os.ReadDir(outside); len(entries) != 0 {
				t.Fatal("wrote through source symlink")
			}
			if _, err := os.Stat(filepath.Join(group, "新剧")); !os.IsNotExist(err) {
				t.Fatal("created new drama inside an existing drama")
			}
		})
	}
}

func TestDownloadDirectoryOwnershipAndConcurrentAllocation(t *testing.T) {
	cfg := defaultConfig()
	cfg.OutputDir, cfg.GroupBySource = t.TempDir(), true
	d := &Downloader{cfg: cfg}
	drama := Drama{ID: historyFixtureDramaID, Source: sourceHongguo}
	results := make(chan string, 12)
	var workers sync.WaitGroup
	for i := 0; i < cap(results); i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			directory, err := d.downloadDramaDirectory(drama, "并发同一剧", "")
			if err != nil {
				t.Error(err)
			}
			results <- directory
		}()
	}
	workers.Wait()
	close(results)
	want := filepath.Join(cfg.OutputDir, "红果", "并发同一剧")
	for directory := range results {
		if directory != want {
			t.Fatal("concurrent enqueue allocated different directories", directory)
		}
	}
	duplicate := filepath.Join(cfg.OutputDir, "重复副本")
	if ok, err := claimDownloadDirectory(duplicate, drama.ID, false); err != nil || !ok {
		t.Fatal(err)
	}
	if _, err := (&Downloader{cfg: cfg}).downloadDramaDirectory(drama, "第三份", ""); err == nil || !strings.Contains(err.Error(), "多个") {
		t.Fatal("ambiguous ownership created a third copy", err)
	}
	if same, err := (&Downloader{cfg: cfg}).downloadDramaDirectory(drama, "第三份", want); err != nil || same != want {
		t.Fatal("persisted task location did not resolve ambiguity", same, err)
	}
}

func TestSourceGroupingConfigAndTasklessDirectorySurviveRestart(t *testing.T) {
	cfg := defaultConfig()
	if cfg.GroupBySource || documentFromConfig(cfg).Download.GroupBySource {
		t.Fatal("grouping must be opt-in")
	}
	directory := t.TempDir()
	enabled := true
	settings := runtimeSettings{Concurrency: 2, RequestConcurrency: 2, RequestIntervalMS: 500, GroupBySource: &enabled}
	if err := saveRuntimeSettings(directory, settings); err != nil {
		t.Fatal(err)
	}
	settings.GroupBySource = nil
	settings.Concurrency = 3
	if err := saveRuntimeSettings(directory, settings); err != nil {
		t.Fatal(err)
	}
	document, err := readConfigDocument(runtimeSettingsPath(directory))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := document.config()
	if err != nil || !loaded.GroupBySource || loaded.Concurrency != 3 {
		t.Fatal("older settings request reset grouping", err)
	}
	savedPath := filepath.Join(t.TempDir(), "原目录")
	app := &UIApp{cfg: cfg, statePath: filepath.Join(directory, "ui-state.json"),
		dramaDirectories: map[string]string{historyFixtureDramaID: savedPath}}
	if err := app.saveStateLocked(); err != nil {
		t.Fatal(err)
	}
	restarted := &UIApp{cfg: cfg, statePath: app.statePath, tasks: map[string]*UITask{}}
	restarted.loadState()
	if got := restarted.dramaDirectoryLocked(historyFixtureDramaID); got != savedPath || len(restarted.tasks) != 0 {
		t.Fatal("cleaning task records lost the stable download directory", got)
	}
}

func TestDownloadDirectoryRejectsUnsupportedSources(t *testing.T) {
	cfg := defaultConfig()
	cfg.OutputDir, cfg.GroupBySource = t.TempDir(), true
	d := &Downloader{cfg: cfg}
	for _, drama := range []Drama{
		{ID: "huangdou:7000000000000000002", Source: "huangdou"},
		{ID: historyFixtureDramaID, Source: "huangdou"},
		{ID: "huangguo_video:123", Source: sourceHongguo},
	} {
		if _, err := d.downloadDramaDirectory(drama, "不支持的来源", ""); err == nil {
			t.Fatal("accepted a non-Hongguo drama", drama.ID)
		}
	}
	if entries, err := os.ReadDir(cfg.OutputDir); err != nil || len(entries) != 0 {
		t.Fatal("unsupported source created download directories", err)
	}
}
