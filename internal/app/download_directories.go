package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func dramaSourceFolder(drama Drama) string {
	source := accountSourceGroup(firstNonEmpty(drama.Source, sourceFromDramaID(drama.ID)))
	if source == "" {
		source = accountDramaSource(drama.ID)
	}
	for _, choice := range accountSourceChoices {
		if choice.ID == source {
			return safeFilename(choice.Name)
		}
	}
	return "其他来源"
}

func readDownloadDirectoryID(directory string) (string, error) {
	path := filepath.Join(directory, ".drama-id")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 1024 {
		return "", errors.New("下载目录标识不是有效的普通文件")
	}
	body, err := os.ReadFile(path)
	return strings.TrimSpace(string(body)), err
}

func scanDownloadDirectories(root string) (map[string]string, error) {
	directories := make(map[string]string)
	groups := map[string]bool{}
	for _, choice := range accountSourceChoices {
		groups[safeFilename(choice.Name)] = true
	}
	var scan func(string, bool) error
	scan = func(parent string, grouped bool) error {
		entries, err := os.ReadDir(parent)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			directory := filepath.Join(parent, entry.Name())
			id, err := readDownloadDirectoryID(directory)
			if err != nil {

				continue
			}
			if id != "" {
				if sourceFromDramaID(id) != sourceHongguo {
					continue
				}
				if previous, exists := directories[id]; exists && previous != directory {
					directories[id] = ""
				} else if !exists {
					directories[id] = directory
				}
			} else if !grouped && groups[entry.Name()] {
				if err := scan(directory, true); err != nil {
					return err
				}
			}
		}
		return nil
	}
	err := scan(root, false)
	return directories, err
}

func claimDownloadDirectory(directory, id string, known bool) (bool, error) {
	if err := os.MkdirAll(directory, 0755); err != nil {
		return false, err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("下载剧集目录不是普通文件夹")
	}
	owner, err := readDownloadDirectoryID(directory)
	if err != nil || owner != "" {
		return owner == id, err
	}
	if !known {
		entries, err := os.ReadDir(directory)
		if err != nil {
			return false, err
		}
		if len(entries) > 0 {
			return false, nil
		}
	}
	file, err := os.OpenFile(filepath.Join(directory, ".drama-id"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if errors.Is(err, os.ErrExist) {
		owner, readErr := readDownloadDirectoryID(directory)
		return owner == id, readErr
	}
	if err != nil {
		return false, err
	}
	_, err = file.WriteString(id)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err == nil, err
}

func downloadSourceDirectory(root, source string) (string, error) {
	if err := os.MkdirAll(root, 0755); err != nil {
		return "", err
	}
	directory := filepath.Join(root, source)
	if err := os.Mkdir(directory, 0755); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("下载站源目录不是普通文件夹")
	}
	if _, err = os.Lstat(filepath.Join(directory, ".drama-id")); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("站源目录“%s”与已有剧集目录同名，请更换新下载的根目录；原剧集仍使用原目录", source)
	}
	return directory, nil
}

func (d *Downloader) downloadDramaDirectory(drama Drama, title, existing string) (string, error) {
	var valid bool
	drama, valid = normalizeHongguoDrama(drama)
	if !valid {
		return "", errors.New("此版本仅支持红果剧集")
	}
	d.directoryMu.Lock()
	defer d.directoryMu.Unlock()
	if drama.ID == "" || len(drama.ID) > 512 || strings.ContainsAny(drama.ID, "\x00\r\n") {
		return "", errors.New("下载剧集标识无效")
	}
	if d.downloadDirectories == nil {
		var err error
		d.downloadDirectories, err = scanDownloadDirectories(d.cfg.OutputDir)
		if err != nil {
			d.downloadDirectories = nil
			return "", err
		}
	}
	if existing == "" {
		var found bool
		existing, found = d.downloadDirectories[drama.ID]
		if found && existing == "" {
			return "", errors.New("发现本剧的多个已有目录，请保留下载任务以确定更新位置")
		}
	}
	if existing != "" {
		ok, err := claimDownloadDirectory(existing, drama.ID, true)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", errors.New("已有下载目录属于其他剧集，原文件已保留")
		}
		d.downloadDirectories[drama.ID] = existing
		return existing, nil
	}
	root := d.cfg.OutputDir
	grouped := d.cfg.GroupBySource
	if d.downloadGrouping != nil {
		grouped = *d.downloadGrouping
	}
	if grouped {
		var err error
		root, err = downloadSourceDirectory(root, dramaSourceFolder(drama))
		if err != nil {
			return "", err
		}
	}
	for _, name := range []string{title, title + "_" + hashShort(drama.ID)} {
		directory, err := safeJoin(root, name)
		if err != nil {
			return "", err
		}
		ok, err := claimDownloadDirectory(directory, drama.ID, false)
		if err != nil {
			return "", err
		}
		if ok {
			d.downloadDirectories[drama.ID] = directory
			return directory, nil
		}
	}
	return "", errors.New("下载目录存在同名内容，原文件已保留")
}

func (app *UIApp) dramaDirectoryLocked(id string) string {
	if directory := app.dramaDirectories[id]; directory != "" {
		return directory
	}
	for _, taskID := range app.taskOrder {
		if task := app.tasks[taskID]; task != nil && task.DramaID == id && !isChapterPlaceholderTask(task) && task.Path != "" {
			return filepath.Dir(task.Path)
		}
	}
	if merged := app.merges[id]; merged != nil && merged.OutputPath != "" {
		return filepath.Dir(merged.OutputPath)
	}
	return ""
}
