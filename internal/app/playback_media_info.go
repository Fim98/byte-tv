package app

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type playbackMediaInfo struct {
	Video    string
	Audio    string
	MIME     string
	Duration float64
	MP4      bool
	Range    bool
}

func playbackMoovInfo(body []byte) playbackMediaInfo {
	info := playbackMediaInfo{MP4: true}
	mvhd := playbackMP4Child(body, "mvhd")
	if len(mvhd) >= 20 && mvhd[0] == 0 {
		scale := binary.BigEndian.Uint32(mvhd[12:16])
		if scale > 0 {
			info.Duration = float64(binary.BigEndian.Uint32(mvhd[16:20])) / float64(scale)
		}
	} else if len(mvhd) >= 32 && mvhd[0] == 1 {
		scale := binary.BigEndian.Uint32(mvhd[20:24])
		if scale > 0 {
			info.Duration = float64(binary.BigEndian.Uint64(mvhd[24:32])) / float64(scale)
		}
	}
	tracks, valid := playbackMP4Boxes(body)
	if !valid {
		return playbackMediaInfo{}
	}
	var codecs []string
	for _, track := range tracks {
		if track.kind != "trak" {
			continue
		}
		stsd := playbackMP4Child(track.body, "mdia", "minf", "stbl", "stsd")
		if len(stsd) < 8 {
			continue
		}
		entries, ok := playbackMP4Boxes(stsd[8:])
		if !ok || len(entries) != 1 {
			continue
		}
		entry := entries[0]
		kind := entry.kind
		if kind == "encv" && len(entry.body) >= 78 {
			kind = string(playbackMP4Child(entry.body[78:], "sinf", "frma"))
		}
		if kind == "enca" && len(entry.body) >= 28 {
			kind = string(playbackMP4Child(entry.body[28:], "sinf", "frma"))
		}
		switch kind {
		case "avc1", "avc3":
			if len(entry.body) < 78 {
				continue
			}
			avcc := playbackMP4Child(entry.body[78:], "avcC")
			if len(avcc) < 4 {
				continue
			}
			info.Video = "h264"
			codecs = append(codecs, fmt.Sprintf("%s.%02X%02X%02X", kind, avcc[1], avcc[2], avcc[3]))
		case "hvc1", "hev1":
			info.Video = "hevc"
			codecs = append(codecs, kind)
		case "av01":
			info.Video = "av1"
			codecs = append(codecs, "av01")
		case "vp09":
			info.Video = "vp9"
			codecs = append(codecs, "vp09")
		case "mp4a":
			if len(entry.body) >= 28 && playbackAACLC(playbackMP4Child(entry.body[28:], "esds")) {
				info.Audio = "aac"
				codecs = append(codecs, "mp4a.40.2")
			} else {
				info.Audio = "unknown"
			}
		case "ac-3":
			info.Audio = "ac3"
			codecs = append(codecs, kind)
		case "ec-3":
			info.Audio = "eac3"
			codecs = append(codecs, kind)
		case "Opus":
			info.Audio = "opus"
			codecs = append(codecs, "opus")
		}
	}
	if info.Video != "" && info.Audio != "unknown" {
		info.MIME = `video/mp4; codecs="` + strings.Join(codecs, ", ") + `"`
	}
	return info
}

func (app *UIApp) inspectPlaybackMedia(ctx context.Context, media *playbackMediaSession) {
	if media.plan.Player == "hls" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var file *os.File
	if media.local != "" {
		var err error
		file, err = os.Open(media.local)
		if err != nil {
			return
		}
		defer file.Close()
	}
	read := func(offset, length int64) ([]byte, error) {
		if offset < 0 || length < 1 || length > 2<<20 {
			return nil, errors.New("媒体索引读取范围无效")
		}
		if file != nil {
			body := make([]byte, length)
			_, err := file.ReadAt(body, offset)
			media.info.Range = true
			return body, err
		}
		release, err := app.mediaResources().acquire(ctx, "media", false)
		if err != nil {
			return nil, err
		}
		defer release()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, media.media.URL, nil)
		if err != nil {
			return nil, err
		}
		request.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, offset+length-1))
		request.Header.Set("Accept-Encoding", "identity")
		request.Header.Set("User-Agent", userAgent)
		request.Header.Set("Referer", media.media.Referer)
		response, err := (&http.Client{Transport: app.downloader.client.Transport}).Do(request)
		if err != nil {
			return nil, err
		}
		defer response.Body.Close()
		if response.StatusCode == http.StatusPartialContent {
			if !strings.HasPrefix(response.Header.Get("Content-Range"), "bytes "+strconv.FormatInt(offset, 10)+"-") {
				return nil, errors.New("媒体 Range 响应无效")
			}
			media.info.Range = true
		} else if response.StatusCode != http.StatusOK || offset != 0 {
			return nil, errors.New("媒体不支持索引范围读取")
		}
		body := make([]byte, length)
		_, err = io.ReadFull(response.Body, body)
		return body, err
	}
	var offset int64
	for boxes := 0; boxes < 16; boxes++ {
		header, err := read(offset, 16)
		if err != nil {
			return
		}
		size, headerSize := uint64(binary.BigEndian.Uint32(header)), uint64(8)
		if size == 1 {
			size, headerSize = binary.BigEndian.Uint64(header[8:16]), 16
		}
		if size < headerSize || size > 1<<40 {
			return
		}
		if string(header[4:8]) == "ftyp" {
			media.info.MP4 = true
		}
		if string(header[4:8]) == "moov" {
			body, err := read(offset+int64(headerSize), int64(size-headerSize))
			if err != nil {
				return
			}
			info := playbackMoovInfo(body)
			info.Range = media.info.Range
			media.info = info
			if info.Duration > 0 && info.Duration < 24*60*60 {
				media.plan.Duration, media.plan.SeekEnd = info.Duration, info.Duration
			}
			if info.MIME != "" {
				media.plan.MIME = info.MIME
			}
			return
		}
		offset += int64(size)
	}
}
