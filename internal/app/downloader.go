package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

type Downloader struct {
	cfg                 Config
	client              *http.Client
	providerMu          sync.Mutex
	providerHosts       map[string]string
	hongguoOnce         sync.Once
	hongguo             *hongguoAppClient
	rankings            rankingCache
	limiter             *requestLimiter
	proxyRouter         *proxyRouter
	ffmpegMu            sync.Mutex
	ffmpegInstaller     *ffmpegInstaller
	diagnostics         *diagnosticLog
	directoryMu         sync.Mutex
	downloadDirectories map[string]string
	downloadGrouping    *bool
}

func NewDownloader(cfg Config) *Downloader {
	if cfg.OutputDir == "" {
		cfg.OutputDir = defaultOutputDir()
	}
	if cfg.FFmpeg == "" {
		cfg.FFmpeg = "ffmpeg"
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 2
	}
	if cfg.RequestConcurrency <= 0 {
		cfg.RequestConcurrency = 2
	}
	if cfg.RequestIntervalMS <= 0 {
		cfg.RequestIntervalMS = 500
	}
	loadRuntimeSettings(&cfg)
	if absolute, err := filepath.Abs(cfg.dataDirectory()); err == nil {
		cfg.dataDir = absolute
	}
	cfg.outputDirSetting = cfg.OutputDir
	if absolute, err := filepath.Abs(cfg.OutputDir); err == nil {
		cfg.OutputDir = absolute
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: cfg.InsecureTLS}
	transport.MaxIdleConnsPerHost = 8
	transport.ResponseHeaderTimeout = 20 * time.Second
	router := &proxyRouter{}
	router.configure(cfg.ProxyURL)
	transport.Proxy = router.proxy
	return &Downloader{cfg: cfg, client: &http.Client{Transport: newImageTransport(transport, transport), Timeout: 45 * time.Second}, providerHosts: map[string]string{}, limiter: newRequestLimiter(cfg.RequestConcurrency, time.Duration(cfg.RequestIntervalMS)*time.Millisecond), proxyRouter: router, diagnostics: newDiagnosticLog(cfg.dataDirectory())}
}

func (d *Downloader) DownloadEpisode(ctx context.Context, task Task) error {
	return d.DownloadEpisodeWithProgress(ctx, task, nil)
}

func (d *Downloader) DownloadEpisodeWithProgress(ctx context.Context, task Task, callback func(DownloadProgress)) (resultErr error) {
	defer func() {
		if ctx.Err() == nil {
			d.recordTaskFailure("download.failed", task, 0, resultErr)
		}
	}()
	if !isHongguoTask(task) {
		return errors.New("此版本仅支持红果剧集")
	}
	return d.downloadMediaWithProgress(ctx, task, callback)
}

func DownloadTasks(ctx context.Context, d *Downloader, tasks []Task) []Result {
	if len(tasks) == 0 {
		return nil
	}
	jobs := make(chan Task)
	results := make(chan Result)
	var done int64
	workers := d.cfg.Concurrency
	if workers > len(tasks) {
		workers = len(tasks)
	}
	var wg sync.WaitGroup
	for i := 1; i <= workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for task := range jobs {
				cur := atomic.AddInt64(&done, 1)
				fmt.Printf(" [线程%d] [%d/%d] 《%s》 第%d/%d 集\n", id, cur, len(tasks), task.DramaTitle, task.Index, task.Total)
				err := d.DownloadEpisode(ctx, task)
				if err != nil {
					results <- Result{Task: task, OK: false, Err: err.Error()}
				} else {
					results <- Result{Task: task, OK: true}
				}
			}
		}(i)
	}
	go func() {
		for _, task := range tasks {
			jobs <- task
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()
	var out []Result
	for r := range results {
		out = append(out, r)
	}
	return out
}
