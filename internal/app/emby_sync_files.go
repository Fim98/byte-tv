package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type embyFolderManifest struct {
	Version      int               `json:"version"`
	DramaID      string            `json:"dramaId"`
	Chapters     []embyChapter     `json:"chapters"`
	Merged       *embyMergedRecord `json:"merged,omitempty"`
	PosterFile   string            `json:"posterFile,omitempty"`
	PosterHash   string            `json:"posterHash,omitempty"`
	PosterSource string            `json:"posterSource,omitempty"`
}

func validEmbyFolder(folder string) bool {
	if folder == "" {
		return true
	}
	parts := strings.Split(folder, "/")
	if len(parts) > 2 || len(parts) == 2 && !isSourceFolder(parts[0]) {
		return false
	}
	for _, part := range parts {
		if part == "" || len(part) > 240 || part == "." || part == ".." || strings.ContainsAny(part, "\\:\x00\r\n") || filepath.Base(part) != part {
			return false
		}
	}
	return true
}

func isSourceFolder(folder string) bool {
	if folder == "其他来源" {
		return true
	}
	for _, choice := range accountSourceChoices {
		if folder == safeFilename(choice.Name) {
			return true
		}
	}
	return false
}

func embyDirectoryParent(root, folder string, create bool) (string, error) {
	if !validEmbyFolder(folder) || folder == "" {
		return "", errors.New("Emby 剧集目录无效")
	}
	parts := strings.Split(folder, "/")
	if len(parts) == 1 {
		return root, nil
	}
	parent := filepath.Join(root, parts[0])
	if create {
		return parent, ensureEmbyDirectory(parent)
	}
	info, err := os.Lstat(parent)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("Emby 站源目录不是普通文件夹")
	}
	return parent, nil
}

func findManagedEmbyFolder(root string, drama Drama) (string, error) {
	name := embyFolderName(drama)
	suffix := name[strings.LastIndex(name, " ["):]
	var found string
	var scan func(string, string) error
	scan = func(directory, prefix string) error {
		entries, err := os.ReadDir(directory)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			if prefix == "" && isSourceFolder(entry.Name()) {
				if err := scan(filepath.Join(directory, entry.Name()), entry.Name()+"/"); err != nil {
					return err
				}
			}
			folder := prefix + entry.Name()
			if !strings.HasSuffix(entry.Name(), suffix) || !validEmbyFolder(folder) {
				continue
			}
			var manifest embyFolderManifest
			if err := readEmbyJSON(filepath.Join(directory, entry.Name(), ".juku-emby.json"), &manifest, 8<<20); err != nil || manifest.Version != 1 || manifest.DramaID != drama.ID {
				continue
			}
			if found != "" && found != folder {
				return errors.New("发现本剧的多个 Emby 目录，已停止生成重复媒体")
			}
			found = folder
		}
		return nil
	}
	err := scan(root, "")
	return found, err
}

func ensureEmbyDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return os.Mkdir(path, 0755)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("Emby 生成目录不是普通文件夹，已停止写入")
	}
	return nil
}

func syncEmbyDramaFiles(ctx context.Context, settings embySyncSettings, drama Drama, chapters []Chapter, folder string, key []byte, owner string, merged ...*embyMergedRecord) (string, int, int, error) {
	if !validEmbyFolder(folder) {
		return folder, 0, 0, errors.New("Emby 剧集目录无效")
	}
	if err := os.MkdirAll(settings.OutputDir, 0755); err != nil {
		return folder, 0, 0, err
	}
	root, err := filepath.EvalSymlinks(settings.OutputDir)
	if err != nil {
		return folder, 0, 0, err
	}
	if folder == "" {
		folder, err = findManagedEmbyFolder(root, drama)
		if err != nil {
			return folder, 0, 0, err
		}
		if folder == "" {
			folder = embyFolderName(drama)
			if settings.GroupBySource {
				folder = dramaSourceFolder(drama) + "/" + folder
			}
		}
	}
	parent, err := embyDirectoryParent(root, folder, true)
	if err != nil {
		return folder, 0, 0, err
	}
	target := filepath.Join(root, folder)
	work := target
	var previous []embyChapter
	manifest := embyFolderManifest{Version: 1, DramaID: drama.ID}
	staged := false
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		work, err = os.MkdirTemp(parent, ".juku-emby-")
		if err != nil {
			return folder, 0, 0, err
		}
		staged = true
		defer os.RemoveAll(work)
		if err = os.Chmod(work, 0755); err != nil {
			return folder, 0, 0, err
		}
	} else {
		if err != nil {
			return folder, 0, 0, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return folder, 0, 0, errors.New("Emby 剧集目录不是普通文件夹")
		}
		manifestPath := filepath.Join(work, ".juku-emby.json")
		info, err := os.Lstat(manifestPath)
		if err != nil || !info.Mode().IsRegular() {
			return folder, 0, 0, errors.New("同名目录不属于自动同步，原内容已保留；请使用单独的 Emby 输出目录")
		}
		if err = readEmbyJSON(manifestPath, &manifest, 8<<20); err != nil {
			return folder, 0, 0, err
		}
		if manifest.Version != 1 || manifest.DramaID != drama.ID {
			return folder, 0, 0, errors.New("Emby 目录所属剧集不匹配，原内容已保留")
		}
		previous = manifest.Chapters
	}
	if len(merged) > 0 && merged[0] != nil {
		manifest.Merged = merged[0]
	}
	if manifest.Merged != nil && !manifest.Merged.valid() {
		return folder, 0, 0, errors.New("Emby 合并版记录无效，原文件已保留")
	}
	if len(chapters) == 0 && manifest.Merged == nil {
		return folder, 0, 0, errors.New("站源未返回分集，已有 Emby 文件已保留，稍后将重试")
	}
	var records []embyChapter
	if len(chapters) > 0 || len(previous) > 0 || manifest.Merged == nil {
		records, err = embyChapterRecords(drama.ID, chapters, previous)
	}
	if err != nil {
		return folder, 0, 0, err
	}
	for _, season := range []struct {
		name    string
		enabled bool
	}{{"Season 01", len(records) > 0}, {"Season 00", manifest.Merged != nil}} {
		if season.enabled {
			if err = ensureEmbyDirectory(filepath.Join(work, season.name)); err != nil {
				return folder, 0, 0, err
			}
		}
	}
	written := 0
	err = writeEmbyFiles(drama, records, settings.BaseURL, key, owner, func(name string, body []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		changed, err := writeEmbyFile(filepath.Join(work, filepath.FromSlash(name)), body, 0644)
		if changed {
			written++
		}
		return err
	}, manifest.Merged)
	if err == nil {
		manifest.Chapters = records
		body, marshalErr := json.MarshalIndent(manifest, "", "  ")
		err = marshalErr
		if err == nil {
			_, err = writeEmbyFile(filepath.Join(work, ".juku-emby.json"), append(body, '\n'), 0600)
		}
	}
	if err == nil {
		err = ctx.Err()
	}
	if staged {
		if err == nil {
			err = os.Rename(work, target)
		}
		if err != nil {
			written = 0
		}
	}
	return folder, written, len(records), err
}

func writeEmbyFile(path string, body []byte, mode os.FileMode) (bool, error) {
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() || info.Size() > 8<<20 {
			return false, errors.New("Emby 目标文件不是普通文件或文件过大，已停止写入")
		}
		old, err := os.ReadFile(path)
		if err != nil {
			return false, err
		}
		if bytes.Equal(old, body) {
			return false, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".juku-emby-write-")
	if err != nil {
		return false, err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(mode); err == nil {
		_, err = file.Write(body)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return false, err
	}
	if err = os.Rename(file.Name(), path); err != nil {
		return false, fmt.Errorf("Emby 文件替换失败：%w", err)
	}
	return true, nil
}
