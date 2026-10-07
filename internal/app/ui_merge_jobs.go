package app

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type uiMergeJob struct {
	dramaID string
	tasks   []*UITask
	remove  bool
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	result  uiMergeResult
	running bool
}

func (a *UIApp) enqueueMergesLocked(ids []string, deleteEpisodes bool) ([]*uiMergeJob, error) {
	if a.mergeStopped {
		return nil, fmt.Errorf("服务正在停止，无法提交合并")
	}
	if len(a.mergeJobs)+len(ids) > 256 {
		return nil, fmt.Errorf("合并队列已满，请等待当前任务完成后再提交")
	}
	if a.mergeJobs == nil {
		a.mergeJobs = make(map[string]*uiMergeJob)
	}
	if a.merges == nil {
		a.merges = make(map[string]*UIMergeState)
	}
	byDrama := make(map[string][]*UITask)
	for _, taskID := range a.taskOrder {
		if task := a.tasks[taskID]; task != nil && task.Status == uiStatusSuccess && !task.RemoveRequested {
			byDrama[task.DramaID] = append(byDrama[task.DramaID], cloneUITask(task))
		}
	}
	previous := cloneMergeStates(a.merges)
	var jobs, added []*uiMergeJob
	for _, id := range ids {
		job := a.mergeJobs[id]
		if job == nil {
			ctx, cancel := context.WithCancel(context.Background())
			job = &uiMergeJob{dramaID: id, tasks: byDrama[id], remove: deleteEpisodes, ctx: ctx, cancel: cancel, done: make(chan struct{})}
			a.mergeJobs[id] = job
			added = append(added, job)
			title := ""
			if len(job.tasks) > 0 {
				title = job.tasks[0].DramaTitle
			}
			a.merges[id] = &UIMergeState{DramaID: id, DramaTitle: title, Status: "queued", Detail: "已提交后台合并，可关闭或刷新页面", DeleteEpisodes: deleteEpisodes, UpdatedAt: time.Now()}
		}
		jobs = append(jobs, job)
	}
	if err := a.saveStateLocked(); err != nil {
		for _, job := range added {
			delete(a.mergeJobs, job.dramaID)
			job.cancel()
		}
		a.merges = previous
		return nil, fmt.Errorf("保存合并任务失败：%w", err)
	}
	if len(added) > 0 {
		go a.runMergeJobs(added)
	}
	return jobs, nil
}

func (a *UIApp) runMergeJobs(jobs []*uiMergeJob) {
	a.mergeMu.Lock()
	defer a.mergeMu.Unlock()
	for _, job := range jobs {
		a.mu.Lock()
		if a.mergeJobs[job.dramaID] != job {
			a.mu.Unlock()
			continue
		}
		job.running = true
		a.mu.Unlock()
		if job.ctx.Err() == nil {
			job.result = a.mergeDrama(job.ctx, job.dramaID, job.tasks, job.remove)
		} else {
			job.result = uiMergeResult{DramaID: job.dramaID, Error: "合并已取消，原分集已保留"}
			a.setMergeState(job.result, "failed", 0, false, job.remove)
		}
		job.cancel()
		a.mu.Lock()
		delete(a.mergeJobs, job.dramaID)
		close(job.done)
		a.mu.Unlock()
	}
}

func (a *UIApp) stopMergeJobs() {
	a.mu.Lock()
	a.mergeStopped = true
	var jobs []*uiMergeJob
	for _, job := range a.mergeJobs {
		job.cancel()
		jobs = append(jobs, job)
	}
	a.mu.Unlock()
	for _, job := range jobs {
		<-job.done
	}
}

func (a *UIApp) handleMergeCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !requireDownload(w, r) {
		return
	}
	ids, ok := readDramaIDsRequest(w, r)
	if !ok || !a.requireDramaSources(w, r, ids) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	canceled := 0
	for _, id := range ids {
		if job := a.mergeJobs[id]; job != nil {
			job.cancel()
			if !job.running {
				job.result = uiMergeResult{DramaID: id, Error: "合并已取消，原分集已保留"}
				if state := a.merges[id]; state != nil {
					state.Status, state.Detail, state.Error = "failed", "", job.result.Error
				}
				delete(a.mergeJobs, id)
				close(job.done)
			}
			canceled++
		}
	}
	_ = a.saveStateLocked()
	writeJSON(w, http.StatusOK, map[string]int{"canceled": canceled})
}
