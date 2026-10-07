package app

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (d *Downloader) downloadMediaWithProgress(ctx context.Context, task Task, callback func(DownloadProgress)) error {
	if ok, size := existingGood(task.OutPath, d.cfg.SkipBytes); ok {
		if callback != nil {
			callback(DownloadProgress{Percent: 100, DownloadedBytes: size, TotalBytes: size, Phase: "completed"})
		}
		return nil
	}
	if callback != nil {
		callback(DownloadProgress{Phase: "preparing"})
	}
	partPath := strings.TrimSuffix(task.OutPath, filepath.Ext(task.OutPath)) + ".part.mp4"
	if err := os.MkdirAll(filepath.Dir(task.OutPath), 0o755); err != nil {
		return err
	}
	progress := newDownloadProgressState(partPath, task.OutPath, task.Chapter.MediaSize, callback)
	progress.report("resolving", true)
	retries := d.cfg.Retries
	if retries <= 0 {
		retries = 3
	}
	var lastErr error
	for attempt := 1; attempt <= retries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if attempt > 1 {
			_ = os.Remove(partPath)
			progress = newDownloadProgressState(partPath, task.OutPath, task.Chapter.MediaSize, callback)
			progress.report("retrying", true)
			select {
			case <-time.After(time.Duration(attempt*2) * time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		progress.report("resolving", true)
		media, err := d.resolveProviderMedia(ctx, task)
		if err != nil {
			lastErr = publicError(err)
			continue
		}
		media, err = d.selectDownloadQuality(ctx, task, media)
		if err != nil {
			lastErr = publicError(err)
			continue
		}
		if len(media.CENCKey) != 0 && (len(media.CENCKey) != 16 || media.Playlist != "") {
			return errors.New("CENC 媒体的密钥或格式无效")
		}
		if handled, directErr := d.downloadDirectProviderMedia(ctx, task, media, nil, progress); handled {
			if directErr == nil {
				return nil
			}
			lastErr = directErr
			continue
		}
		ffmpeg, err := d.ensureFFmpeg(ctx)
		if err != nil {
			return err
		}
		_ = os.Remove(partPath)
		progress.setMediaTotal(media.Duration)
		proxy, err := d.newHLSProxy(ctx, media, nil)
		if err != nil {
			return err
		}
		if media.Playlist != "" {
			if cache, cacheErr := prepareDownloadMediaCache(partPath+".segments", media, nil); cacheErr == nil {
				proxy.cache = cache
			} else {
				proxy.Close()
				lastErr = cacheErr
				continue
			}
		}
		inputURL := proxy.root
		origin, _ := url.Parse(media.Referer)
		headers := fmt.Sprintf("User-Agent: %s\r\nReferer: %s\r\n", userAgent, media.Referer)
		if origin != nil && origin.Host != "" {
			headers += "Origin: " + origin.Scheme + "://" + origin.Host + "\r\n"
		}
		cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		args := []string{
			"-hide_banner", "-loglevel", "error", "-nostats",
			"-rw_timeout", "20000000",
			"-protocol_whitelist", "http,https,tcp,tls,crypto,httpproxy",
			"-headers", headers,
		}
		if media.Playlist != "" {
			args = append(args, "-allowed_extensions", "ALL")
		}
		if len(media.CENCKey) > 0 {
			args = append(args, "-decryption_key", hex.EncodeToString(media.CENCKey))
		}
		args = append(args,
			"-i", inputURL,
			"-c", "copy",
			"-progress", "pipe:1",
			"-f", "mp4",
			"-y", partPath,
		)
		cmd := ffmpegMediaCommand(cmdCtx, ffmpeg, args...)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			if proxy != nil {
				proxy.Close()
			}
			cancel()
			return err
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			if proxy != nil {
				proxy.Close()
			}
			cancel()
			return err
		}
		if err := cmd.Start(); err != nil {
			if proxy != nil {
				proxy.Close()
			}
			cancel()
			lastErr = err
			continue
		}
		progress.report("downloading", true)
		stderrDone := make(chan string, 1)
		go func() {
			var b cappedStringWriter
			b.limit = 64 * 1024
			_, _ = io.Copy(&b, stderr)
			stderrDone <- b.String()
		}()
		progressDone := make(chan struct{})
		go func() {
			defer close(progressDone)
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				key, val, ok := strings.Cut(scanner.Text(), "=")
				if ok && (key == "out_time_us" || key == "out_time_ms") {
					if micros, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64); err == nil && micros >= 0 {
						progress.setMediaElapsed(time.Duration(micros) * time.Microsecond)
						progress.report("downloading", false)
					}
				}
			}
		}()
		tickerDone := make(chan struct{})
		go func() {
			defer close(tickerDone)
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					progress.report("downloading", false)
				case <-progressDone:
					return
				}
			}
		}()
		waitErr := cmd.Wait()
		<-progressDone
		<-tickerDone
		stderrText := <-stderrDone
		commandErr := cmdCtx.Err()
		cancel()
		var proxyErr error
		if proxy != nil {
			proxyErr = proxy.Err()
			proxy.Close()
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if commandErr != nil {
			lastErr = commandErr
			_ = os.Remove(partPath)
			continue
		}
		if proxyErr != nil {
			lastErr = proxyErr
			continue
		}
		if waitErr != nil {
			lastErr = fmt.Errorf("ffmpeg failed: %v %s", waitErr, truncate(stderrText, 1000))
			_ = os.Remove(partPath)
			continue
		}
		st, err := os.Stat(partPath)
		if err != nil {
			lastErr = err
			continue
		}
		if st.Size() < 100*1024 {
			lastErr = fmt.Errorf("downloaded file too small: %d bytes", st.Size())
			_ = os.Remove(partPath)
			continue
		}
		if err := os.Rename(partPath, task.OutPath); err != nil {
			lastErr = err
			continue
		}
		_ = os.RemoveAll(partPath + ".segments")
		progress.report("completed", true)
		return nil
	}
	return lastErr
}
