package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const sortMetadataVersion = 1
const sortMetadataBatchSize = 40
const sortMetadataRetryDelay = 5 * time.Minute
const sortMetadataMissingRetryDelay = 24 * time.Hour

type sortMetadataState struct {
	Version      int       `json:"version"`
	CheckedAt    time.Time `json:"checkedAt"`
	Pending      bool      `json:"pending,omitempty"`
	CoverChecked bool      `json:"coverChecked,omitempty"`
	RetryAt      time.Time `json:"retryAt,omitempty"`
	LastError    string    `json:"lastError,omitempty"`
}

type libraryRowsKey struct{}
type libraryPriorityKey struct{}
type libraryMetadataProgressKey struct{}

type libraryMetadataProgress struct {
	Running bool `json:"running"`
	Checked int  `json:"checked"`
	Total   int  `json:"total"`
	Updated int  `json:"updated"`
	Failed  int  `json:"failed"`
}

func supportsSortMetadata(drama Drama) bool {
	switch dramaProvider(drama) {
	case sourceHongguo:
		return true
	}
	return false
}

func needsSortMetadata(drama Drama) bool {
	return supportsSortMetadata(drama) && (drama.SortMetadata == nil || drama.SortMetadata.Version != sortMetadataVersion || needsHongguoCoverAddress(drama))
}

func sortMetadataRetryAt(state *sortMetadataState, now time.Time) time.Time {
	if state == nil || state.Version != 0 {
		return time.Time{}
	}
	if !state.RetryAt.IsZero() {
		return state.RetryAt
	}
	if state.CheckedAt.IsZero() {
		return time.Time{}
	}
	return state.CheckedAt.Add(sortMetadataRetryDelay)
}

func sortMetadataFailureDelay(err error) time.Duration {
	var status *httpStatusError
	if errors.As(err, &status) && (status.status == http.StatusNotFound || status.status == http.StatusGone) {
		return sortMetadataMissingRetryDelay
	}
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "无效的") || strings.Contains(text, "未返回所请求") || strings.Contains(text, "返回了其他剧集") || strings.Contains(text, "与请求剧集不符") {
		return sortMetadataMissingRetryDelay
	}
	return sortMetadataRetryDelay
}

func failedSortMetadataState(drama Drama, err error, now time.Time) *sortMetadataState {
	state := &sortMetadataState{CheckedAt: now, RetryAt: now.Add(sortMetadataFailureDelay(err)), LastError: truncate(publicError(err).Error(), 300)}
	if drama.SortMetadata != nil {
		state.CoverChecked = drama.SortMetadata.CoverChecked
	}
	return state
}

func needsHongguoCoverAddress(drama Drama) bool {
	return dramaProvider(drama) == sourceHongguo && bestDramaCover(drama) == "" &&
		(drama.SortMetadata == nil || !drama.SortMetadata.CoverChecked)
}

func selectSortMetadataBatch(dramas []Drama, source string, priority []string, now time.Time) []Drama {
	positions := make(map[string]int, len(priority))
	for index, id := range priority {
		if _, found := positions[id]; !found {
			positions[id] = index
		}
	}
	var candidates []Drama
	seen := map[string]bool{}
	for _, drama := range dramas {
		if seen[drama.ID] || !matchesSourceFilter(dramaProvider(drama), source) || !needsSortMetadata(drama) {
			continue
		}
		seen[drama.ID] = true
		if retryAt := sortMetadataRetryAt(drama.SortMetadata, now); !retryAt.IsZero() && now.Before(retryAt) {
			continue
		}
		candidates = append(candidates, drama)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, leftOK := positions[candidates[i].ID]
		right, rightOK := positions[candidates[j].ID]
		if leftOK != rightOK {
			return leftOK
		}
		if leftOK && left != right {
			return left < right
		}
		var leftTime, rightTime time.Time
		if candidates[i].SortMetadata != nil {
			leftTime = candidates[i].SortMetadata.CheckedAt
		}
		if candidates[j].SortMetadata != nil {
			rightTime = candidates[j].SortMetadata.CheckedAt
		}
		return leftTime.Before(rightTime)
	})
	if len(candidates) > sortMetadataBatchSize {
		candidates = candidates[:sortMetadataBatchSize]
	}
	return candidates
}

func sortMetadataRemaining(dramas []Drama) map[string]int {
	counts := map[string]int{"": 0, "hongguo": 0}
	for _, drama := range dramas {
		if !needsSortMetadata(drama) {
			continue
		}
		source := dramaProvider(drama)
		counts[source]++
		counts[""]++
	}
	return counts
}

func reportMetadataProgress(ctx context.Context, progress libraryMetadataProgress) {
	if callback, ok := ctx.Value(libraryMetadataProgressKey{}).(func(libraryMetadataProgress)); ok {
		callback(progress)
	}
}

func (d *Downloader) backfillSortMetadata(ctx context.Context, source string) ([]Drama, map[string]error) {
	rows, _ := ctx.Value(libraryRowsKey{}).([]Drama)
	priority, _ := ctx.Value(libraryPriorityKey{}).([]string)
	batch := selectSortMetadataBatch(rows, source, priority, time.Now())
	progress := libraryMetadataProgress{Running: true, Total: len(batch)}
	reportMetadataProgress(ctx, progress)
	defer func() { progress.Running = false; reportMetadataProgress(ctx, progress) }()
	failures := map[string]error{}
	if len(batch) == 0 {
		return nil, failures
	}
	workCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	type result struct {
		patch    Drama
		previous Drama
		err      error
	}
	jobs := make(chan Drama, len(batch))
	results := make(chan result, len(batch))
	for _, drama := range batch {
		jobs <- drama
	}
	close(jobs)
	workers := d.cfg.RequestConcurrency
	if workers < 1 {
		workers = 1
	}
	if workers > 3 {
		workers = 3
	}
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for drama := range jobs {
				if workCtx.Err() != nil {
					return
				}
				itemCtx, stop := context.WithTimeout(workCtx, 40*time.Second)
				patch, err := d.fetchDramaSortMetadata(itemCtx, drama)
				stop()
				now := time.Now()
				patch.SortMetadata = &sortMetadataState{CheckedAt: now}
				if err == nil {
					patch.SortMetadata.Version = sortMetadataVersion
					patch.SortMetadata.CoverChecked = true
				} else {
					patch.SortMetadata = failedSortMetadataState(drama, err, now)
				}
				results <- result{patch: patch, previous: drama, err: err}
			}
		}()
	}
	go func() { group.Wait(); close(results) }()
	var patches []Drama
	for result := range results {
		patch := result.patch
		provider := dramaProvider(result.previous)
		if result.err != nil {
			progress.Failed++
			if failures[provider] == nil {
				failures[provider] = fmt.Errorf("部分历史资料未补齐，可稍后重试: %w", result.err)
			}
		}
		if patch.OnlineDate != "" && patch.OnlineDate != result.previous.OnlineDate || patch.Heat != "" && patch.Heat != result.previous.Heat || patch.Views != "" && patch.Views != result.previous.Views || bestDramaCover(patch) != "" && bestDramaCover(patch) != bestDramaCover(result.previous) {
			progress.Updated++
		}
		patches = append(patches, patch)
		progress.Checked++
		reportLibraryProgress(ctx, provider, []Drama{patch}, nil, false)
		reportMetadataProgress(ctx, progress)
	}
	if progress.Checked < len(batch) {
		for _, drama := range batch {
			provider := dramaProvider(drama)
			if failures[provider] == nil {
				failures[provider] = errors.New("本批补齐已暂停，剩余条目可再次手动补齐")
			}
		}
	}
	return patches, failures
}

func (d *Downloader) fetchMoreLibrary(ctx context.Context, source string) ([]Drama, error) {
	category, _ := ctx.Value(libraryCategoryKey{}).(string)
	patches, failures := d.backfillSortMetadata(ctx, source)
	if matchesSourceFilter(sourceHongguo, source) {
		items, err := d.fetchHongguoCategoryDramas(ctx, category)
		if err != nil {
			failures[sourceHongguo] = errors.Join(failures[sourceHongguo], err)
		}
		reportLibraryProgress(ctx, sourceHongguo, items, failures[sourceHongguo], true)
		patches = mergeSourceDramas(patches, items, nil, sourceHongguo)
	}
	if len(failures) > 0 {
		return patches, &libraryLoadError{failures: failures}
	}
	return patches, nil
}
