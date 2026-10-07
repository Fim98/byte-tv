package app

import (
	"context"
	"errors"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type playbackClientCapabilities struct {
	MP4       bool     `json:"mp4"`
	NativeHLS bool     `json:"nativeHls"`
	HlsJS     bool     `json:"hlsjs"`
	Video     []string `json:"video"`
	Audio     []string `json:"audio"`
}

func playbackCodecAllowed(choices []string, codec string) bool {
	if len(choices) == 0 || codec == "" {
		return true
	}
	for _, choice := range choices {
		if choice == codec {
			return true
		}
	}
	return false
}

var playbackProbeVideo = regexp.MustCompile(`Video: ([a-zA-Z0-9_]+)`)
var playbackProbeAudio = regexp.MustCompile(`Audio: ([a-zA-Z0-9_]+)`)

func (app *UIApp) probePlaybackCodecs(ctx context.Context, media *playbackMediaSession) error {
	if media.info.Video != "" && media.info.Audio != "unknown" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	ffmpeg, err := app.downloader.ensureFFmpeg(ctx)
	if err != nil {
		return err
	}
	release, err := app.mediaResources().acquire(ctx, "remux", false)
	if err != nil {
		return err
	}
	defer release()
	input := media.local
	var proxy *hlsProxy
	if input == "" {
		proxy, err = app.downloader.newHLSProxy(ctx, media.media, media.key)
		if err != nil {
			return err
		}
		defer proxy.Close()
		proxy.acquire = func(ctx context.Context) (func(), error) { return app.mediaResources().acquire(ctx, "media", false) }
		input = proxy.root
	}
	args := playbackInputArgs(media.media, input, 0)
	for index, value := range args {
		if value == "-i" {
			args = append(args[:index], append([]string{"-probesize", "2097152", "-analyzeduration", "2000000"}, args[index:]...)...)
			break
		}
	}
	args = append(args, "-c", "copy", "-t", "0", "-f", "null", "-")
	command := ffmpegMediaCommand(ctx, ffmpeg, args...)
	log := &playbackLog{text: cappedStringWriter{limit: 64 * 1024}, ready: make(chan struct{})}
	command.Stderr = log
	err = command.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	duration, body := log.snapshot()
	if value := playbackProbeVideo.FindStringSubmatch(body); len(value) == 2 {
		media.info.Video = value[1]
	}
	if value := playbackProbeAudio.FindStringSubmatch(body); len(value) == 2 {
		media.info.Audio = value[1]
	}
	if proxy != nil && proxy.Err() != nil || media.info.Video == "" && err != nil {
		return playbackStreamError(ctx, proxy, log, media.media, err, nil)
	}
	if media.plan.Duration <= 0 && duration > 0 {
		media.plan.Duration, media.plan.SeekEnd = duration, duration
	}
	return nil
}

func (app *UIApp) planMedia(ctx context.Context, media *playbackMediaSession, client playbackClientCapabilities, mode, source, origin, base, query string) error {
	app.attachMediaGateway(media, base, query)
	app.inspectPlaybackMedia(ctx, media)
	if mode == "legacy" {
		media.plan.Player, media.plan.Processing, media.plan.Reason = "legacy", "video", "client_decode_failed"
		return nil
	}
	convert := len(media.media.CENCKey) > 0 || mode == "compatible" || mode == "audio"
	if !playbackCodecAllowed(client.Video, media.info.Video) {
		convert = true
	}
	if !playbackCodecAllowed(client.Audio, media.info.Audio) {
		convert = true
	}
	if convert {
		if err := app.probePlaybackCodecs(ctx, media); err != nil {
			return err
		}
		if !playbackCodecAllowed(client.Video, media.info.Video) || media.info.Video == "" {
			media.plan.Player, media.plan.Processing, media.plan.Reason = "legacy", "video", "video_codec_unsupported"
			return nil
		}
		processing := "remux"
		media.plan.Reason = "container_compatibility"
		if len(media.media.CENCKey) > 0 {
			media.plan.Reason = "server_decryption_required"
		}
		if mode == "audio" || !playbackCodecAllowed(client.Audio, media.info.Audio) {
			processing, media.plan.Reason = "audio", "audio_codec_unsupported"
		}
		if !client.NativeHLS && !client.HlsJS {
			media.plan.Player, media.plan.Processing, media.plan.Reason = "legacy", "video", "hls_player_unavailable"
			return nil
		}
		if err := app.generatePlayback(ctx, media, processing, base, query); err != nil {
			var outputErr *playbackOutputError
			if errors.As(err, &outputErr) && ctx.Err() == nil {
				media.generated.Close()
				media.generated = nil
				media.plan.Player, media.plan.Processing, media.plan.Reason = "legacy", "video", "generated_output_incomplete"
				app.downloader.recordDiagnostic(diagnosticEvent{Level: "warn", Event: "playback.fallback", Source: source, Message: app.redactError(err)})
				return nil
			}
			return err
		}
	} else if media.plan.Player == "hls" && !client.NativeHLS && !client.HlsJS || media.plan.Player == "mp4" && !client.MP4 {
		media.plan.Player, media.plan.Processing, media.plan.Reason = "legacy", "video", "original_player_unavailable"
		return nil
	}
	if mode == "auto" && media.plan.Processing == "original" && app.allowPlaybackDirect(ctx, media, source, origin) {
		media.plan.Delivery, media.plan.Reason = "redirect", "direct_probe_passed"
	} else if mode == "proxy" {
		media.plan.Reason = "client_direct_access_failed"
	}
	return nil
}

func (app *UIApp) handlePlaybackPlan(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Session string                     `json:"session"`
		Episode int                        `json:"episode"`
		Start   float64                    `json:"start"`
		Quality int                        `json:"quality"`
		Version uint64                     `json:"version"`
		Mode    string                     `json:"mode"`
		Client  playbackClientCapabilities `json:"client"`
	}
	if !readPlaybackRequest(writer, request, &input) {
		return
	}
	if input.Episode < 1 || math.IsNaN(input.Start) || math.IsInf(input.Start, 0) || input.Start < 0 || input.Start > 24*60*60 || input.Quality < 0 || input.Quality > 4320 || len(input.Client.Video) > 16 || len(input.Client.Audio) > 16 {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "集数、播放位置或媒体能力无效"})
		return
	}
	if input.Mode == "" {
		input.Mode = "auto"
	}
	if input.Mode != "auto" && input.Mode != "proxy" && input.Mode != "compatible" && input.Mode != "audio" && input.Mode != "legacy" {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "播放模式无效"})
		return
	}
	if !app.requirePlaybackOwner(writer, request, input.Session) {
		return
	}
	if !app.mediaResources().snapshot().Enabled {
		app.playbackMu.Lock()
		session := app.playbacks[input.Session]
		merged := session != nil && input.Episode <= len(session.downloadIDs) && strings.HasPrefix(session.downloadIDs[input.Episode-1], mergedPlaybackPrefix)
		app.playbackMu.Unlock()
		if !merged {
			writeJSON(writer, http.StatusOK, playbackMediaPlan{Player: "legacy", Reason: "optimization_disabled"})
			return
		}
	}
	run, status, err := app.beginPlayback(context.WithoutCancel(request.Context()), input.Session, input.Episode, input.Start, input.Quality, input.Version, false)
	if err != nil {
		writeJSON(writer, status, map[string]string{"error": err.Error()})
		return
	}
	ready := false
	var media *playbackMediaSession
	defer func() {
		if !ready {
			run.stop()
			if media != nil {
				media.Close()
			}
		}
	}()
	ctx, cancel := context.WithTimeout(run.ctx, 90*time.Second)
	defer cancel()
	stop := context.AfterFunc(request.Context(), cancel)
	defer stop()
	media, err = app.resolveMediaSession(run.ctx, ctx, run.task, run.downloadID, input.Quality, run.cache)
	if err == nil {
		base := "/api/ui/playback/media/" + run.id + "/" + strconv.FormatUint(run.run, 10) + "/"
		err = app.planMedia(ctx, media, input.Client, input.Mode, sourceFromDramaID(run.task.DramaID), request.Header.Get("Origin"), base, "")
	}
	if err != nil {
		app.finishPlaybackRun(run, err, request.Context().Err() != nil)
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "准备播放失败：" + app.redactError(err)})
		return
	}
	media.plan.Run = run.run
	if media.plan.Player == "legacy" {
		writeJSON(writer, http.StatusOK, media.plan)
		return
	}
	app.playbackMu.Lock()
	if app.playbacks[run.id] == run.session && run.session.run == run.run && ctx.Err() == nil {
		run.session.media = media
		run.session.cancel = func() { run.stop(); media.Close() }
		ready = true
	}
	app.playbackMu.Unlock()
	if !ready {
		writeJSON(writer, http.StatusConflict, map[string]string{"error": "播放请求已更新"})
		return
	}
	app.playbackRunReady(run, media.plan.Duration)
	app.downloader.recordDiagnostic(diagnosticEvent{Level: "info", Event: "playback.plan", Source: sourceFromDramaID(run.task.DramaID), DramaID: run.task.DramaID,
		Episode: input.Episode, Run: run.run, Quality: input.Quality, Message: strings.Join([]string{media.plan.Delivery, media.plan.Processing, media.plan.Player, media.plan.Reason}, " / ")})
	writeJSON(writer, http.StatusOK, media.plan)
}
