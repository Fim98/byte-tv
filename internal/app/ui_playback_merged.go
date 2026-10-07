package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const mergedPlaybackPrefix = "merged:"

func (app *UIApp) mergedPlaybackTaskLocked(id string) (Task, string, error) {
	dramaID, ok := strings.CutPrefix(id, mergedPlaybackPrefix)
	if !ok || dramaID == "" {
		return Task{}, "", errors.New("合并播放记录无效")
	}
	state := app.merges[dramaID]
	if state == nil || state.Status != "success" {
		return Task{}, "", errors.New("本剧尚无合并成功的文件")
	}
	path := completedPlaybackPath(&UITask{Status: uiStatusSuccess, Path: state.OutputPath})
	if path == "" {
		return Task{}, "", errors.New("本地合并文件不存在或已移动，请重新合并")
	}
	label := fmt.Sprintf("全集（第%d–%d集）", state.StartEpisode, state.EndEpisode)
	episode, _ := json.Marshal(label)
	task := Task{DramaID: dramaID, DramaTitle: state.DramaTitle, Index: 1, Total: 1, OutPath: path,
		Chapter: Chapter{ID: fmt.Sprintf("%s:%d-%d", id, state.StartEpisode, state.EndEpisode), Source: sourceFromDramaID(dramaID), Title: label, CurrentEpisode: episode}}
	return task, path, nil
}
