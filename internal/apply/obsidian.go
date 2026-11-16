package apply

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"git.subcult.tv/subculture-collective/pult/internal/registry"
)

// planObsidianVaults keeps one app and transaction identity while planning
// each vault independently, including ownership of its appearance and assets.
// Config names the read-only registry; only the discovered vaults are edited.
func (o Options) planObsidianVaults(e registry.Entry, th *Theme) (AppPlan, error) {
	ap := AppPlan{ID: e.ID, Entry: e, Config: o.configPath(e), Version: o.version(e)}
	paths, stale, err := obsidianConfigs(ap.Config)
	if err != nil {
		return ap, err
	}
	if stale > 0 {
		ap.Notes = append(ap.Notes, fmt.Sprintf("%d unavailable or non-absolute vault paths left alone", stale))
	}
	active := 0
	for _, path := range paths {
		vault := e
		vault.Config = registry.Config{Paths: map[registry.Platform][]string{o.Platform: {path}}}
		child, err := o.planApp(vault, th)
		if err != nil {
			return ap, fmt.Errorf("vault %s: %w", filepath.Dir(filepath.Dir(path)), err)
		}
		if child.Skip != "" {
			ap.Notes = append(ap.Notes, path+": "+child.Skip)
			continue
		}
		active++
		ap.Integration = child.Integration
		ap.Edits = append(ap.Edits, child.Edits...)
		ap.Diffs = append(ap.Diffs, child.Diffs...)
		ap.Notes = append(ap.Notes, child.Notes...)
		if len(paths) == 1 {
			ap.Owner = child.Owner
		}
	}
	if active == 0 {
		ap.Skip = "no eligible registered Obsidian vaults; open a vault with the default .obsidian directory first"
	} else {
		ap.Notes = append(ap.Notes, fmt.Sprintf("%d registered vault(s); ownership checked for each config and theme asset", active))
	}
	return ap, nil
}

func obsidianConfigs(registryPath string) ([]string, int, error) {
	src, err := readFile(registryPath)
	if err != nil {
		return nil, 0, err
	}
	if len(bytes.TrimSpace(src)) == 0 {
		return nil, 0, nil
	}
	var data struct {
		Vaults map[string]struct {
			Path string `json:"path"`
		} `json:"vaults"`
	}
	if err := json.Unmarshal(bytes.TrimPrefix(src, []byte("\ufeff")), &data); err != nil {
		return nil, 0, fmt.Errorf("read Obsidian vault registry: %w", err)
	}
	var candidates, paths []string
	for _, vault := range data.Vaults {
		candidates = append(candidates, vault.Path)
	}
	slices.Sort(candidates)
	seen, stale := map[string]bool{}, 0
	for _, path := range candidates {
		if !filepath.IsAbs(path) {
			stale++
			continue
		}
		dir := filepath.Join(path, ".obsidian")
		info, err := os.Stat(dir)
		if os.IsNotExist(err) || (err == nil && !info.IsDir()) {
			stale++
			continue
		}
		if err != nil {
			return nil, stale, fmt.Errorf("read vault configuration %s: %w", dir, err)
		}
		canonical, err := filepath.EvalSymlinks(dir)
		if os.IsNotExist(err) {
			stale++
			continue
		}
		if err != nil {
			return nil, stale, err
		}
		if !seen[canonical] {
			paths = append(paths, filepath.Join(dir, "appearance.json"))
			seen[canonical] = true
		}
	}
	return paths, stale, nil
}
