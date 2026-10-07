package app

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type playbackSegmentIndex struct {
	Name     string
	Start    float64
	Duration float64
}

type playbackOutputError struct{ cause error }

func (err *playbackOutputError) Error() string { return err.cause.Error() }
func (err *playbackOutputError) Unwrap() error { return err.cause }

type playbackGenerated struct {
	ctx       context.Context
	cancel    context.CancelFunc
	directory string
	base      string
	query     string
	playlist  string
	segments  []playbackSegmentIndex
	files     map[string]bool
	bytes     int64
	resources *playbackResources
	done      chan struct{}
	once      sync.Once
	mu        sync.RWMutex
}

func playbackGeneratedArgs(media providerMedia, input, processing string) []string {
	args := playbackInputArgs(media, input, 0)
	if processing == "audio" {
		args = append(args, "-c:v", "copy", "-c:a", "aac", "-b:a", "128k", "-ar", "48000", "-ac", "2")
	} else {
		args = append(args, "-c", "copy")
	}
	return append(args, "-avoid_negative_ts", "make_zero", "-f", "hls", "-hls_time", "2", "-hls_list_size", "0",
		"-hls_playlist_type", "vod", "-hls_segment_type", "fmp4", "-hls_fmp4_init_filename", "init.mp4",
		"-hls_flags", "temp_file", "-hls_segment_filename", "%06d.m4s", "index.m3u8")
}

func (app *UIApp) generatePlayback(ctx context.Context, media *playbackMediaSession, processing, base, query string) error {
	ffmpeg, err := app.downloader.ensureFFmpeg(ctx)
	if err != nil {
		return err
	}
	release, err := app.mediaResources().acquire(ctx, processing, false)
	if err != nil {
		return err
	}
	defer release()
	directory, err := os.MkdirTemp("", "juku-playback-media-")
	if err != nil {
		return err
	}
	jobCtx, cancel := context.WithCancel(media.ctx)
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	generated := &playbackGenerated{ctx: jobCtx, cancel: cancel, directory: directory, base: base, query: query,
		resources: app.mediaResources(), done: make(chan struct{}), files: make(map[string]bool)}
	media.generated = generated
	defer close(generated.done)
	input := media.local
	var proxy *hlsProxy
	if input == "" {
		proxy, err = app.downloader.newHLSProxy(jobCtx, media.media, media.key)
		if err != nil {
			return err
		}
		defer proxy.Close()
		proxy.acquire = func(ctx context.Context) (func(), error) { return app.mediaResources().acquire(ctx, "media", false) }
		input = proxy.root
	} else {
		input, err = filepath.Abs(input)
		if err != nil {
			return err
		}
		input = filepath.ToSlash(input)
	}
	command := ffmpegMediaCommand(jobCtx, ffmpeg, playbackGeneratedArgs(media.media, input, processing)...)

	command.Dir = directory
	log := &playbackLog{text: cappedStringWriter{limit: 64 * 1024}, ready: make(chan struct{})}
	command.Stderr = log
	if err := command.Start(); err != nil {
		return fmt.Errorf("无法启动媒体封装：%w", err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			if err != nil {
				return playbackStreamError(jobCtx, proxy, log, media.media, err, nil)
			}
			if err = generated.accountCache(); err != nil {
				return err
			}
			body, err := os.ReadFile(filepath.Join(directory, "index.m3u8"))
			if err != nil {
				return &playbackOutputError{fmt.Errorf("媒体播放清单未生成：%w", err)}
			}
			if err := generated.index(string(body)); err != nil {
				return err
			}
			media.plan.URL = generated.assetURL("index.m3u8")
			media.plan.Player, media.plan.MIME, media.plan.Processing = "hls", "application/vnd.apple.mpegurl", processing
			media.plan.Duration = generated.segments[len(generated.segments)-1].Start + generated.segments[len(generated.segments)-1].Duration
			media.plan.SeekEnd = media.plan.Duration
			return nil
		case <-ticker.C:
			if err := generated.accountCache(); err != nil {
				cancel()
				<-done
				return err
			}
		case <-ctx.Done():
			cancel()
			<-done
			return ctx.Err()
		}
	}
}

func (generated *playbackGenerated) accountCache() error {
	entries, err := os.ReadDir(generated.directory)
	if err != nil {
		return err
	}
	var size int64
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > 64<<20 {
			return errors.New("媒体分片超出缓存限制")
		}
		size += info.Size()
	}
	resources := generated.resources
	resources.mu.Lock()
	defer resources.mu.Unlock()
	if resources.cacheBytes+size-generated.bytes > int64(resources.settings.CacheMB)<<20 {
		return errors.New("媒体临时缓存已达上限，请关闭其他播放后重试")
	}
	resources.cacheBytes += size - generated.bytes
	generated.bytes = size
	return nil
}

func (generated *playbackGenerated) assetURL(name string) string {
	address := generated.base + name
	if generated.query != "" {
		address += "?" + generated.query
	}
	return address
}

func (generated *playbackGenerated) index(playlist string) (err error) {
	defer func() {
		if err != nil {
			err = &playbackOutputError{err}
		}
	}()
	if len(playlist) > 2<<20 || !strings.HasPrefix(playlist, "#EXTM3U") || !strings.Contains(playlist, "#EXT-X-ENDLIST") {
		return errors.New("媒体封装尚未完成，不能生成完整播放索引")
	}
	lines := strings.Split(playlist, "\n")
	var start, duration, longest float64
	validName := func(name string) bool {
		return name != "" && filepath.Base(name) == name && !strings.ContainsAny(name, "\\?\x00") && (strings.HasSuffix(name, ".mp4") || strings.HasSuffix(name, ".m4s"))
	}
	for i, line := range lines {
		if strings.HasPrefix(line, "#EXTINF:") {
			value, _, _ := strings.Cut(strings.TrimPrefix(line, "#EXTINF:"), ",")
			var err error
			duration, err = strconv.ParseFloat(value, 64)
			if err != nil || math.IsNaN(duration) || math.IsInf(duration, 0) || duration <= 0 || duration > 3600 {
				return errors.New("媒体分片时长无效")
			}
			longest = math.Max(longest, duration)
		} else if strings.HasPrefix(line, "#EXT-X-MAP:") {
			matches := hlsURIAttribute.FindStringSubmatch(line)
			if len(matches) != 2 || !validName(matches[1]) {
				return errors.New("媒体初始化片段无效")
			}
			generated.files[matches[1]] = true
			lines[i] = strings.Replace(line, matches[0], `URI="`+generated.assetURL(matches[1])+`"`, 1)
		} else if line != "" && !strings.HasPrefix(line, "#") {
			if duration <= 0 || !validName(line) || len(generated.segments) >= 16384 || start+duration > 24*60*60 {
				return errors.New("媒体分片索引无效")
			}
			generated.files[line] = true
			generated.segments = append(generated.segments, playbackSegmentIndex{Name: line, Start: start, Duration: duration})
			start += duration
			duration = 0
			lines[i] = generated.assetURL(line)
		}
	}
	if len(generated.segments) == 0 || duration != 0 {
		return errors.New("媒体播放索引没有完整分片")
	}
	for i, line := range lines {
		if strings.HasPrefix(line, "#EXT-X-TARGETDURATION:") {
			lines[i] = "#EXT-X-TARGETDURATION:" + strconv.Itoa(int(math.Ceil(longest)))
		}
	}
	for name := range generated.files {
		info, err := os.Stat(filepath.Join(generated.directory, name))
		if err != nil {
			return fmt.Errorf("媒体分片未完整生成（%s）：%w", name, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("媒体分片未完整生成（%s 不是普通文件）", name)
		}
		if info.Size() <= 0 {
			return fmt.Errorf("媒体分片未完整生成（%s 为空文件）", name)
		}
	}
	generated.playlist = strings.Join(lines, "\n")
	return nil
}

func (generated *playbackGenerated) Close() {
	generated.once.Do(func() {
		generated.cancel()
		go func() {
			<-generated.done
			generated.mu.Lock()
			defer generated.mu.Unlock()
			_ = os.RemoveAll(generated.directory)
			generated.resources.mu.Lock()
			generated.resources.cacheBytes -= generated.bytes
			generated.resources.mu.Unlock()
		}()
	})
}

func (generated *playbackGenerated) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	generated.mu.RLock()
	defer generated.mu.RUnlock()
	if generated.ctx.Err() != nil {
		writer.WriteHeader(http.StatusGone)
		return
	}
	name := strings.TrimPrefix(request.URL.Path, generated.base)
	if name == "index.m3u8" {
		writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		http.ServeContent(writer, request, name, time.Time{}, strings.NewReader(generated.playlist))
		return
	}
	if !generated.files[name] {
		http.NotFound(writer, request)
		return
	}
	file, err := os.Open(filepath.Join(generated.directory, name))
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	defer file.Close()
	writer.Header().Set("Content-Type", "video/mp4")
	http.ServeContent(writer, request, name, time.Time{}, file)
}
