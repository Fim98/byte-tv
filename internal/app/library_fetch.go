package app

import (
	"context"
	"errors"
	"fmt"
	"time"
)

func (d *Downloader) fetchAllDramas(ctx context.Context, sourceFilter string) ([]Drama, error) {
	if sourceFilter != "" && canonicalProviderSource(sourceFilter) != sourceHongguo {
		return nil, errors.New("此版本仅支持红果站源")
	}
	if more, _ := ctx.Value(libraryMoreKey{}).(bool); more {
		return d.fetchMoreLibrary(ctx, sourceFilter)
	}
	category, _ := ctx.Value(libraryCategoryKey{}).(string)
	fmt.Println("正在获取红果剧库列表...")
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	dramas, err := d.fetchHongguoCategoryDramas(ctx, category)
	dramas = onlyHongguoDramas(dramas)
	if len(dramas) == 0 && err == nil && !hongguoCatalogInitialized(d.hongguoCatalogSnapshot()) {
		err = errors.New("未返回可识别的视频数据")
	}
	reportLibraryProgress(ctx, sourceHongguo, dramas, err, true)
	fmt.Printf(" 剧库获取完成：%d 部\n", len(dramas))
	if err != nil {
		fmt.Printf(" 红果获取失败: %v\n", publicError(err))
		return dramas, &libraryLoadError{failures: map[string]error{sourceHongguo: err}}
	}
	return dramas, nil
}

func (d *Downloader) GetDramaChapters(ctx context.Context, seriesID string) (string, []Chapter, error) {
	_, sourceID, valid := splitProviderDramaID(seriesID)
	if !valid {
		return "", nil, errors.New("仅支持红果剧集 ID，可填写 hongguo:数字ID 或数字ID")
	}
	title, chapters, err := d.fetchHongguoChapters(ctx, sourceID)
	return title, uniqueChapters(chapters), err
}

func uniqueChapters(chapters []Chapter) []Chapter {
	seen := map[string]bool{}
	var out []Chapter
	for i, ch := range chapters {
		key := ch.ID
		if key == "" {
			key = ch.VideoURL
		}
		if key == "" {
			key = fmt.Sprintf("idx_%d", i)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, ch)
	}
	return out
}

func (d *Downloader) BuildDramaTasks(ctx context.Context, drama Drama) ([]Task, error) {
	return d.buildDramaTasksInDirectory(ctx, drama, "")
}

func (d *Downloader) buildDramaTasksInDirectory(ctx context.Context, drama Drama, existingDirectory string) ([]Task, error) {
	var valid bool
	drama, valid = normalizeHongguoDrama(drama)
	if !valid {
		return nil, errors.New("此版本仅支持红果剧集")
	}
	title, chapters, err := d.GetDramaChapters(ctx, drama.ID)
	if err != nil {
		return nil, err
	}
	if len(chapters) == 0 {
		return nil, nil
	}
	drama = d.hongguoCachedDrama(drama)
	dramaDir, err := d.downloadDramaDirectory(drama, title, existingDirectory)
	if err != nil {
		return nil, err
	}
	var tasks []Task
	for i, ch := range chapters {
		ep := ch.EpisodeString(i + 1)
		epNum := padEpisode(ep)
		fileName := safeFilename(epNum + ".mp4")
		outPath, err := safeJoin(dramaDir, fileName)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, Task{DramaID: drama.ID, DramaTitle: title, Chapter: ch, Index: i + 1, Total: len(chapters), OutPath: outPath, ReleaseStatus: dramaReleaseStatus(drama)})
	}
	return tasks, nil
}
