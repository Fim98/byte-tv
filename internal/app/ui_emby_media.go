package app

import (
	"net/http"
	"strings"
)

func (app *UIApp) handleEmbyStream(writer http.ResponseWriter, request *http.Request) {
	if !app.mediaResources().snapshot().Enabled {
		app.handleEmbyLegacyStream(writer, request)
		return
	}
	id, chapter, ok := app.authorizeEmby(writer, request)
	if !ok {
		return
	}
	if request.Method == http.MethodHead {
		writer.Header().Set("Content-Type", "application/octet-stream")
		return
	}
	session := app.acquireEmbyPlayback(writer, request, id, chapter, "", false)
	if session == nil {
		return
	}
	defer app.releaseEmbyPlayback(session)
	if session.native != nil {
		app.serveEmbyPlaylist(writer, request, session)
		return
	}
	writer.Header().Set("Location", session.media.plan.URL)
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.WriteHeader(http.StatusFound)
}

func (app *UIApp) handleEmbyMediaAsset(writer http.ResponseWriter, request *http.Request) {
	id, chapter, ok := app.authorizeEmby(writer, request)
	if !ok {
		return
	}
	parts := strings.Split(strings.TrimPrefix(request.URL.Path, "/api/emby/media/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		http.NotFound(writer, request)
		return
	}
	session := app.acquireEmbyPlayback(writer, request, id, chapter, parts[0], false)
	if session == nil {
		return
	}
	defer app.releaseEmbyPlayback(session)
	media := session.media
	if media == nil {
		app.writeEmbyPlaybackError(writer, request, http.StatusGone, "播放格式已变化，请重新播放")
		return
	}
	active := &playbackRunContext{id: session.id, run: session.run, session: session, ctx: media.ctx}
	writer = &playbackActivityWriter{ResponseWriter: writer, app: app, run: active}
	app.serveMediaSession(writer, request, media)
}
