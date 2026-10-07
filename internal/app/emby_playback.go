package app

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/url"
	"time"
)

func (app *UIApp) activePlaybackSessionsLocked() int {
	count := 0
	for _, session := range app.playbacks {
		if !session.emby || session.mediaWaiters > 0 {
			count++
		}
	}
	return count
}

func (app *UIApp) trimEmbyPlaybacksLocked(reserve int) []context.CancelFunc {
	limit := app.mediaResources().snapshot().MaxSessions - reserve
	var cancellations []context.CancelFunc
	for {
		count := 0
		var oldest *playbackSession
		for _, session := range app.playbacks {
			if !session.emby || session.native == nil && (session.media == nil || session.media.generated == nil) {
				continue
			}
			count++
			if session.mediaWaiters == 0 && session.state != "opening" && (oldest == nil || session.expires.Before(oldest.expires)) {
				oldest = session
			}
		}
		if count <= limit || oldest == nil {
			return cancellations
		}
		if oldest.cancel != nil {
			cancellations = append(cancellations, oldest.cancel)
		}
		if oldest.media != nil {
			oldest.embyProcessing = oldest.media.plan.Processing
		}
		oldest.cancel, oldest.native, oldest.media = nil, nil, nil
		oldest.mediaReady, oldest.mediaError, oldest.state = nil, nil, "ready"
	}
}

func (app *UIApp) releaseEmbyPlayback(session *playbackSession) {
	app.playbackMu.Lock()
	session.mediaWaiters--
	var cancellations []context.CancelFunc
	if app.playbacks[session.id] == session && session.mediaWaiters == 0 {
		if session.state == "opening" || session.mediaError != nil {
			delete(app.playbacks, session.id)
			session.timer.Stop()
			if session.cancel != nil {
				cancellations = append(cancellations, session.cancel)
			}
		} else {
			app.touchPlaybackLocked(session)
		}
	}
	cancellations = append(cancellations, app.trimEmbyPlaybacksLocked(0)...)
	app.playbackMu.Unlock()
	for _, cancel := range cancellations {
		cancel()
	}
}

func (app *UIApp) writeEmbyPlaybackError(writer http.ResponseWriter, request *http.Request, status int, message string) {
	if status == http.StatusTooManyRequests {
		writer.Header().Set("Retry-After", "2")
	}
	id := request.URL.Query().Get("id")
	app.downloader.recordDiagnostic(diagnosticEvent{Event: "emby.playback_failed", HTTPStatus: status,
		Source: sourceFromDramaID(id), DramaID: id, Message: message})
	writeJSON(writer, status, map[string]string{"error": message})
}

func (app *UIApp) acquireEmbyPlayback(writer http.ResponseWriter, request *http.Request, id, chapter, sessionID string, legacy bool) *playbackSession {
	owner := request.URL.Query().Get("account")
	app.playbackMu.Lock()
	var session *playbackSession
	if sessionID != "" {
		session = app.playbacks[sessionID]
		if session == nil || !session.emby || session.viewer != nil || session.accountID != owner || session.dramaID != id || len(session.tasks) != 1 || session.tasks[0].Chapter.ID != chapter {
			app.playbackMu.Unlock()
			app.writeEmbyPlaybackError(writer, request, http.StatusGone, "播放已过期，请重新播放")
			return nil
		}
	} else {
		for _, candidate := range app.playbacks {
			if candidate.emby && candidate.embyLegacy == legacy && candidate.accountID == owner && candidate.dramaID == id && len(candidate.tasks) == 1 && candidate.tasks[0].Chapter.ID == chapter && candidate.mediaError == nil {
				session = candidate
				break
			}
		}
	}
	if (session == nil || session.mediaWaiters == 0) && app.activePlaybackSessionsLocked() >= app.mediaResources().snapshot().MaxSessions {
		app.playbackMu.Unlock()
		app.writeEmbyPlaybackError(writer, request, http.StatusTooManyRequests, "同时播放数量已达上限，请稍后再试")
		return nil
	}
	if session == nil {
		sessionID = randomHex(24)
		if sessionID == "" {
			app.playbackMu.Unlock()
			app.writeEmbyPlaybackError(writer, request, http.StatusInternalServerError, "无法创建播放会话")
			return nil
		}
		session = &playbackSession{id: sessionID, accountID: owner, dramaID: id, emby: true, embyLegacy: legacy, run: 1,
			tasks: []Task{{DramaID: id, Chapter: Chapter{ID: chapter}}}}
		if app.playbacks == nil {
			app.playbacks = make(map[string]*playbackSession)
		}
		app.playbacks[sessionID] = session
		session.timer = time.AfterFunc(playbackIdleTimeout, func() { app.expirePlayback(sessionID) })
	}
	session.mediaWaiters++
	app.touchPlaybackLocked(session)
	var cancellations []context.CancelFunc
	if session.mediaWaiters == 1 {
		failed := sessionID == "" && session.media != nil && (session.media.failed.Load() || session.media.ctx.Err() != nil || session.media.proxy != nil && session.media.proxy.Err() != nil)
		if session.native != nil {
			state, err := session.native.state()
			failed = failed || err != nil || state == "stopped"
		}
		if failed {
			if session.cancel != nil {
				cancellations = append(cancellations, session.cancel)
			}
			session.cancel, session.native, session.media, session.mediaReady, session.mediaError = nil, nil, nil, nil, nil
		}
	}
	var parent context.Context
	if session.mediaReady == nil {
		cancellations = append(cancellations, app.trimEmbyPlaybacksLocked(1)...)
		parent, session.cancel = context.WithCancel(context.Background())
		session.mediaReady, session.state = make(chan struct{}), "opening"
	}
	ready := session.mediaReady
	app.playbackMu.Unlock()
	for _, cancel := range cancellations {
		cancel()
	}
	if parent != nil {
		query := url.Values{"id": {id}, "chapter": {chapter}, "key": {request.URL.Query().Get("key")}}
		if owner != "" {
			query.Set("account", owner)
		}
		go app.prepareEmbyPlayback(parent, session, ready, query.Encode(), request.Header.Get("Origin"))
	}
	select {
	case <-ready:
	case <-request.Context().Done():
		app.releaseEmbyPlayback(session)
		return nil
	}
	app.playbackMu.Lock()
	err := session.mediaError
	if err == nil && (app.playbacks[session.id] != session || session.media == nil && session.native == nil) {
		err = errors.New("播放会话已结束")
	}
	app.playbackMu.Unlock()
	if err != nil {
		app.releaseEmbyPlayback(session)
		app.writeEmbyPlaybackError(writer, request, http.StatusBadGateway, "准备 Emby 播放失败："+app.redactError(err))
		return nil
	}
	return session
}

func (app *UIApp) prepareEmbyPlayback(parent context.Context, session *playbackSession, ready chan struct{}, query, origin string) {
	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	task, downloadID, err := app.embyTask(ctx, session.dramaID, session.tasks[0].Chapter.ID)
	var media *playbackMediaSession
	var native *playbackNative
	var duration float64
	if err == nil && !session.embyLegacy {
		media, err = app.resolveMediaSession(parent, ctx, task, downloadID, 0)
		if err == nil {
			base := "/api/emby/media/" + session.id + "/"
			mode := "auto"
			if session.embyProcessing == "remux" {
				mode = "compatible"
			} else if session.embyProcessing == "audio" {
				mode = "audio"
			}
			err = app.planMedia(ctx, media, playbackClientCapabilities{MP4: true, NativeHLS: true}, mode, sourceFromDramaID(task.DramaID), origin, base, query)
			if err == nil {
				duration = media.plan.Duration
				if media.plan.Player == "legacy" {
					media.Close()
					media = nil
				}
			}
		}
	}
	if err == nil && media == nil {
		native = newPlaybackNative(app, parent, task, downloadID, 0, 0, false)
		native.start()
		_, err = native.segment(ctx, 0)
		if err == nil {
			_, duration, _ = native.metadata()
			if duration <= 0 || duration > 24*60*60 || math.IsNaN(duration) || math.IsInf(duration, 0) {
				err = errors.New("未取得有效播放时长")
			}
		}
	}
	app.playbackMu.Lock()
	if err == nil && (app.playbacks[session.id] != session || session.mediaReady != ready || ctx.Err() != nil) {
		err = context.Canceled
	}
	if err == nil {
		previous := session.cancel
		session.cancel = func() {
			previous()
			if media != nil {
				media.Close()
			}
			if native != nil {
				native.Close()
			}
		}
		session.tasks, session.media, session.native, session.duration, session.state = []Task{task}, media, native, duration, "streaming"
		if media != nil {
			media.plan.Run = session.run
		}
		app.touchPlaybackLocked(session)
	} else {
		session.mediaError, session.state = err, "failed"
	}
	close(ready)
	app.playbackMu.Unlock()
	if err != nil {
		if media != nil {
			media.Close()
		}
		if native != nil {
			native.Close()
		}
	}
}
