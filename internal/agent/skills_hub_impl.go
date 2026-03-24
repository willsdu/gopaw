package agent

// skills_hub_impl.go：Hub 搜索 JSON 解析、各平台 bundle 拉取（ClawHub / GitHub / skills.sh /
// LobeHub / SkillsMP）、bundle 规范化与 installSkillFromHub 主流程。

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/willisdu/gopaw/internal/app/routers"
)

const (
	lobehubMaxZipEntries = 256
	lobehubMaxZipBytes   = 5 * 1024 * 1024
)

func hubFetchJSON(ctx context.Context, cfg hubHTTPConfig, rawURL string) (any, error) {
	b, err := hubHTTPFetch(ctx, cfg, rawURL, "application/json", 0)
	if err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	return v, nil
}

func hubFetchText(ctx context.Context, cfg hubHTTPConfig, rawURL string) (string, error) {
	b, err := hubHTTPFetch(ctx, cfg, rawURL, "text/plain, text/markdown, */*", 0)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func normSearchItems(data any) []map[string]any {
	if arr, ok := data.([]any); ok {
		var out []map[string]any
		for _, x := range arr {
			if m, ok := x.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	m, ok := data.(map[string]any)
	if !ok {
		return nil
	}
	for _, key := range []string{"items", "skills", "results", "data"} {
		if v, ok := m[key].([]any); ok {
			var out []map[string]any
			for _, x := range v {
				if mm, ok := x.(map[string]any); ok {
					out = append(out, mm)
				}
			}
			return out
		}
	}
	if _, ok := m["slug"]; ok {
		if _, ok2 := m["name"]; ok2 {
			return []map[string]any{m}
		}
	}
	return nil
}

func hubSearchSkills(ctx context.Context, q string, limit int) ([]routers.HubSkillSpec, error) {
	cfg := loadHubHTTPConfig()
	if limit <= 0 {
		limit = 20
	}
	u := joinHubURL(cfg.BaseURL, cfg.SearchPath)
	params := url.Values{}
	params.Set("q", q)
	params.Set("limit", strconv.Itoa(limit))
	full := u + "?" + params.Encode()
	data, err := hubFetchJSON(ctx, cfg, full)
	if err != nil {
		return nil, err
	}
	items := normSearchItems(data)
	out := make([]routers.HubSkillSpec, 0, len(items))
	for _, item := range items {
		slug := strings.TrimSpace(fmt.Sprint(item["slug"]))
		if slug == "" {
			slug = strings.TrimSpace(fmt.Sprint(item["name"]))
		}
		if slug == "" {
			continue
		}
		name := fmt.Sprint(item["name"])
		if strings.TrimSpace(name) == "" {
			name = fmt.Sprint(item["displayName"])
		}
		if strings.TrimSpace(name) == "" {
			name = slug
		}
		desc := strings.TrimSpace(fmt.Sprint(item["description"]))
		if desc == "" {
			desc = strings.TrimSpace(fmt.Sprint(item["summary"]))
		}
		out = append(out, routers.HubSkillSpec{
			Slug:        slug,
			Name:        name,
			Description: desc,
			Version:     fmt.Sprint(item["version"]),
			SourceURL:   fmt.Sprint(item["url"]),
		})
	}
	return out, nil
}

func isHTTPURL(s string) bool {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

func splitURLPath(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

func extractSkillsShSpec(raw string) (owner, repo, skill string, ok bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", "", false
	}
	host := strings.ToLower(u.Hostname())
	if host != "skills.sh" && host != "www.skills.sh" {
		return "", "", "", false
	}
	parts := splitURLPath(u.Path)
	if len(parts) < 3 {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

func extractGitHubSpec(raw string) (owner, repo, branch, pathHint string, ok bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", "", "", false
	}
	host := strings.ToLower(u.Hostname())
	if host != "github.com" && host != "www.github.com" {
		return "", "", "", "", false
	}
	parts := splitURLPath(u.Path)
	if len(parts) < 2 {
		return "", "", "", "", false
	}
	owner, repo = parts[0], parts[1]
	if len(parts) >= 4 && (parts[2] == "tree" || parts[2] == "blob") {
		branch = parts[3]
		if len(parts) > 4 {
			pathHint = strings.Join(parts[4:], "/")
		}
	} else if len(parts) > 2 {
		pathHint = strings.Join(parts[2:], "/")
	}
	return owner, repo, branch, pathHint, true
}

func extractLobehubID(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	parts := splitURLPath(u.Path)
	if len(parts) == 0 {
		return ""
	}
	if host == "lobehub.com" || host == "www.lobehub.com" {
		for i, p := range parts {
			if p == "skills" && i+1 < len(parts) {
				return parts[i+1]
			}
		}
		return ""
	}
	if host == "market.lobehub.com" && len(parts) >= 5 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "skills" && parts[4] == "download" {
		return parts[3]
	}
	return ""
}

func extractSkillsMPSlug(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host != "skillsmp.com" && host != "www.skillsmp.com" {
		return ""
	}
	parts := splitURLPath(u.Path)
	for i, p := range parts {
		if p == "skills" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func extractClawhubSlugFromURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if !strings.Contains(host, "clawhub.ai") {
		return ""
	}
	parts := splitURLPath(u.Path)
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func resolveClawhubSlug(bundleURL string) string {
	if s := extractClawhubSlugFromURL(bundleURL); s != "" {
		return s
	}
	return ""
}

func githubRepoAPI(owner, repo, subpath string) string {
	subpath = strings.TrimPrefix(subpath, "/")
	base := fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo)
	if subpath == "" {
		return base
	}
	return base + "/" + subpath
}

func githubGetJSON(ctx context.Context, cfg hubHTTPConfig, apiURL string) (any, error) {
	return hubFetchJSON(ctx, cfg, apiURL)
}

func githubDefaultBranch(ctx context.Context, cfg hubHTTPConfig, owner, repo string) string {
	data, err := githubGetJSON(ctx, cfg, githubRepoAPI(owner, repo, ""))
	if err != nil {
		return "main"
	}
	m, ok := data.(map[string]any)
	if !ok {
		return "main"
	}
	b, _ := m["default_branch"].(string)
	if strings.TrimSpace(b) == "" {
		return "main"
	}
	return b
}

func githubReadFileEntry(ctx context.Context, cfg hubHTTPConfig, entry map[string]any) (string, error) {
	if du, ok := entry["download_url"].(string); ok && du != "" {
		b, err := hubHTTPFetch(ctx, cfg, du, "*/*", 0)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	if c, ok := entry["content"].(string); ok && c != "" {
		c = strings.ReplaceAll(c, "\n", "")
		raw, err := base64.StdEncoding.DecodeString(c)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
	return "", fmt.Errorf("unable to read github file entry")
}

func githubGetContent(ctx context.Context, cfg hubHTTPConfig, owner, repo, pth, ref string) (any, error) {
	api := githubRepoAPI(owner, repo, "contents/"+url.PathEscape(pth))
	if ref != "" {
		api += "?ref=" + url.QueryEscape(ref)
	}
	return githubGetJSON(ctx, cfg, api)
}

func fetchSkillMDFromGitHubRoots(ctx context.Context, cfg hubHTTPConfig, owner, repo string, branchCandidates []string, roots []string) (content string, usedRoot string, branch string, err error) {
	for _, br := range branchCandidates {
		if br == "" {
			continue
		}
		for _, root := range roots {
			skillPath := path.Join(root, "SKILL.md")
			skillPath = strings.TrimPrefix(skillPath, "/")
			data, err := githubGetContent(ctx, cfg, owner, repo, skillPath, br)
			if err != nil {
				continue
			}
			m, ok := data.(map[string]any)
			if !ok {
				continue
			}
			if fmt.Sprint(m["type"]) != "file" {
				continue
			}
			txt, err := githubReadFileEntry(ctx, cfg, m)
			if err != nil || strings.TrimSpace(txt) == "" {
				continue
			}
			return txt, root, br, nil
		}
	}
	return "", "", "", fmt.Errorf("SKILL.md not found for repo %s/%s", owner, repo)
}

func collectGitHubSubdirFiles(ctx context.Context, cfg hubHTTPConfig, owner, repo, ref, root, sub string, maxFiles int) map[string]string {
	out := map[string]string{}
	prefix := path.Join(root, sub)
	prefix = strings.TrimPrefix(strings.ReplaceAll(prefix, "\\", "/"), "/")
	queue := []string{prefix}
	count := 0
	for len(queue) > 0 && count < maxFiles {
		dir := queue[0]
		queue = queue[1:]
		data, err := githubGetContent(ctx, cfg, owner, repo, dir, ref)
		if err != nil {
			continue
		}
		arr, ok := data.([]any)
		if !ok {
			continue
		}
		for _, x := range arr {
			ent, ok := x.(map[string]any)
			if !ok {
				continue
			}
			p := fmt.Sprint(ent["path"])
			typ := fmt.Sprint(ent["type"])
			if typ == "dir" {
				queue = append(queue, p)
				continue
			}
			if typ != "file" {
				continue
			}
			rel := strings.TrimPrefix(p, prefix+"/")
			if !strings.HasPrefix(rel, "references/") && !strings.HasPrefix(rel, "scripts/") {
				continue
			}
			txt, err := githubReadFileEntry(ctx, cfg, ent)
			if err != nil {
				continue
			}
			out[path.Join(sub, rel)] = txt
			count++
		}
	}
	return out
}

func fetchBundleFromGitHubRepo(ctx context.Context, cfg hubHTTPConfig, owner, repo, skillHint, requestedVersion string) (any, string, error) {
	defaultBr := githubDefaultBranch(ctx, cfg, owner, repo)
	var branchCandidates []string
	if strings.TrimSpace(requestedVersion) != "" {
		branchCandidates = append(branchCandidates, strings.TrimSpace(requestedVersion))
	} else {
		branchCandidates = append(branchCandidates, defaultBr, "main", "master")
	}
	hint := strings.Trim(strings.TrimSpace(skillHint), "/")
	if strings.HasSuffix(strings.ToLower(hint), "/skill.md") {
		hint = strings.TrimSuffix(hint, "/SKILL.md")
		hint = strings.TrimSuffix(hint, "/skill.md")
	} else if strings.EqualFold(path.Base(hint), "SKILL.md") {
		hint = path.Dir(hint)
		if hint == "." {
			hint = ""
		}
	}
	var roots []string
	if hint != "" {
		roots = []string{path.Join("skills", hint), hint, ""}
	} else {
		roots = []string{""}
	}
	// 去重并规范
	uniq := []string{}
	seen := map[string]struct{}{}
	for _, r := range roots {
		r = strings.TrimSuffix(r, "/")
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		uniq = append(uniq, r)
	}
	content, usedRoot, branch, err := fetchSkillMDFromGitHubRoots(ctx, cfg, owner, repo, branchCandidates, uniq)
	if err != nil {
		return nil, "", err
	}
	files := map[string]any{"SKILL.md": content}
	for _, sub := range []string{"references", "scripts"} {
		for k, v := range collectGitHubSubdirFiles(ctx, cfg, owner, repo, branch, usedRoot, sub, 200) {
			files[k] = v
		}
	}
	skillName := path.Base(strings.TrimSuffix(usedRoot, "/"))
	if skillName == "" || skillName == "." {
		skillName = repo
	}
	if hint != "" {
		skillName = path.Base(hint)
	}
	return map[string]any{"name": skillName, "files": files}, fmt.Sprintf("https://github.com/%s/%s", owner, repo), nil
}

func fetchBundleFromGitHubURL(ctx context.Context, cfg hubHTTPConfig, bundleURL, version string) (any, string, error) {
	owner, repo, br, ph, ok := extractGitHubSpec(bundleURL)
	if !ok {
		return nil, "", fmt.Errorf("invalid github url")
	}
	ref := strings.TrimSpace(version)
	if ref == "" {
		ref = br
	}
	return fetchBundleFromGitHubRepo(ctx, cfg, owner, repo, ph, ref)
}

func fetchBundleFromSkillsSh(ctx context.Context, cfg hubHTTPConfig, bundleURL, version string) (any, string, error) {
	o, r, sk, ok := extractSkillsShSpec(bundleURL)
	if !ok {
		return nil, "", fmt.Errorf("invalid skills.sh url")
	}
	// skills.sh 解析出的第三段为技能名；与 Python 一致尝试 skills/<name>、<name>、仓库根。
	return fetchBundleFromGitHubRepo(ctx, cfg, o, r, sk, version)
}

func parseSkillsMPSlug(slug string) (owner, repo, skillHint string) {
	slug = strings.TrimSpace(slug)
	if strings.HasSuffix(slug, "-skill-md") {
		slug = slug[:len(slug)-len("-skill-md")]
	}
	tokens := strings.Split(slug, "-")
	if len(tokens) < 3 {
		return "", "", ""
	}
	owner = tokens[0]
	// 保守拆分：repo 取第二段，其余为 skill_hint
	repo = tokens[1]
	skillHint = strings.Join(tokens[2:], "-")
	return owner, repo, skillHint
}

func fetchBundleFromSkillsMP(ctx context.Context, cfg hubHTTPConfig, bundleURL, version string) (any, string, error) {
	slug := extractSkillsMPSlug(bundleURL)
	if slug == "" {
		return nil, "", fmt.Errorf("invalid skillsmp url")
	}
	owner, repo, hint := parseSkillsMPSlug(slug)
	if owner == "" || repo == "" {
		return nil, "", fmt.Errorf("could not parse skillsmp slug")
	}
	return fetchBundleFromGitHubRepo(ctx, cfg, owner, repo, hint, version)
}

func fetchBundleFromLobehub(ctx context.Context, cfg hubHTTPConfig, identifier, bundleURL, version string) (any, string, error) {
	dl := "https://market.lobehub.com/api/v1/skills/" + url.PathEscape(identifier) + "/download"
	params := ""
	if strings.TrimSpace(version) != "" {
		params = "?version=" + url.QueryEscape(strings.TrimSpace(version))
	}
	rawURL := dl + params
	b, err := hubHTTPFetch(ctx, cfg, rawURL, "application/zip, application/octet-stream, */*", lobehubMaxZipBytes+1)
	if err != nil {
		return nil, "", err
	}
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, "", fmt.Errorf("lobehub zip: %w", err)
	}
	files := map[string]string{}
	n := 0
	var total int64
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		n++
		if n > lobehubMaxZipEntries {
			return nil, "", fmt.Errorf("lobehub zip too many entries")
		}
		total += int64(f.UncompressedSize64)
		if total > lobehubMaxZipBytes {
			return nil, "", fmt.Errorf("lobehub zip too large")
		}
		parts := splitURLPath(strings.ReplaceAll(f.Name, "\\", "/"))
		if !lobehubKeepPath(parts) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(rc, lobehubMaxZipBytes))
		rc.Close()
		if err != nil {
			continue
		}
		if bytes.IndexByte(raw, 0) >= 0 {
			continue
		}
		files[strings.Join(parts, "/")] = string(raw)
	}
	if _, ok := files["SKILL.md"]; !ok {
		return nil, "", fmt.Errorf("lobehub package missing SKILL.md")
	}
	name := nameFromSkillMD(files["SKILL.md"])
	if name == "" {
		name = identifier
	}
	return map[string]any{"name": name, "files": files}, bundleURL, nil
}

func lobehubKeepPath(parts []string) bool {
	if len(parts) == 0 {
		return false
	}
	if len(parts) == 1 && parts[0] == "SKILL.md" {
		return true
	}
	if parts[0] == "references" && len(parts) > 1 {
		return true
	}
	if parts[0] == "scripts" && len(parts) > 1 {
		return true
	}
	return len(parts) == 1
}

func bundleHasContent(data any) bool {
	m, ok := data.(map[string]any)
	if !ok {
		return false
	}
	for _, k := range []string{"content", "skill_md", "skillMd"} {
		if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
			return true
		}
	}
	if files, ok := m["files"].(map[string]any); ok {
		if s, ok := files["SKILL.md"].(string); ok && strings.TrimSpace(s) != "" {
			return true
		}
	}
	return false
}

func extractVersionHint(detail map[string]any, requested string) string {
	if strings.TrimSpace(requested) != "" {
		return strings.TrimSpace(requested)
	}
	if lv, ok := detail["latestVersion"].(map[string]any); ok {
		if v, ok := lv["version"].(string); ok && v != "" {
			return v
		}
	}
	if sk, ok := detail["skill"].(map[string]any); ok {
		if tags, ok := sk["tags"].(map[string]any); ok {
			if t, ok := tags["latest"].(string); ok && t != "" {
				return t
			}
		}
	}
	return ""
}

func hydrateClawhubPayload(ctx context.Context, cfg hubHTTPConfig, data any, slug, requestedVersion string) (any, error) {
	if bundleHasContent(data) {
		return data, nil
	}
	root, ok := data.(map[string]any)
	if !ok {
		return data, nil
	}
	skill, ok := root["skill"].(map[string]any)
	if !ok {
		return data, nil
	}
	skillSlug := strings.TrimSpace(fmt.Sprint(skill["slug"]))
	if skillSlug == "" {
		skillSlug = slug
	}
	if skillSlug == "" {
		return data, nil
	}
	var versionData any = root
	versionObj, _ := root["version"].(map[string]any)
	var filesMeta []any
	if versionObj != nil {
		filesMeta, _ = versionObj["files"].([]any)
	}
	if !isListOfMaps(filesMeta) {
		vhint := extractVersionHint(root, requestedVersion)
		if vhint == "" {
			return data, nil
		}
		vpath := strings.ReplaceAll(cfg.VersionPath, "{slug}", url.PathEscape(skillSlug))
		vpath = strings.ReplaceAll(vpath, "{version}", url.PathEscape(vhint))
		u := joinHubURL(cfg.BaseURL, vpath)
		var err error
		versionData, err = hubFetchJSON(ctx, cfg, u)
		if err != nil {
			return data, nil
		}
		vd, _ := versionData.(map[string]any)
		if vd == nil {
			return data, nil
		}
		versionObj, _ = vd["version"].(map[string]any)
		if versionObj != nil {
			filesMeta, _ = versionObj["files"].([]any)
		}
	}
	if !isListOfMaps(filesMeta) {
		return data, nil
	}
	versionStr := ""
	if versionObj != nil {
		versionStr = strings.TrimSpace(fmt.Sprint(versionObj["version"]))
	}
	if versionStr == "" {
		versionStr = requestedVersion
	}
	fpath := strings.ReplaceAll(cfg.FilePath, "{slug}", url.PathEscape(skillSlug))
	fileBase := joinHubURL(cfg.BaseURL, fpath)
	files := map[string]string{}
	for _, it := range filesMeta {
		item, ok := it.(map[string]any)
		if !ok {
			continue
		}
		p := strings.TrimSpace(fmt.Sprint(item["path"]))
		if p == "" {
			continue
		}
		q := url.Values{}
		q.Set("path", p)
		if versionStr != "" {
			q.Set("version", versionStr)
		}
		txt, err := hubFetchText(ctx, cfg, fileBase+"?"+q.Encode())
		if err != nil {
			continue
		}
		files[p] = txt
	}
	if files["SKILL.md"] == "" {
		return data, nil
	}
	display := fmt.Sprint(skill["displayName"])
	if strings.TrimSpace(display) == "" {
		display = skillSlug
	}
	return map[string]any{"name": display, "files": files}, nil
}

func isListOfMaps(v any) bool {
	arr, ok := v.([]any)
	if !ok {
		return false
	}
	for _, x := range arr {
		if _, ok := x.(map[string]any); !ok {
			return false
		}
	}
	return len(arr) > 0
}

func fetchBundleFromClawhubSlug(ctx context.Context, cfg hubHTTPConfig, slug, version string) (any, string, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return nil, "", fmt.Errorf("slug is required for clawhub install")
	}
	detailPath := strings.ReplaceAll(cfg.DetailPath, "{slug}", url.PathEscape(slug))
	u := joinHubURL(cfg.BaseURL, detailPath)
	data, err := hubFetchJSON(ctx, cfg, u)
	if err != nil {
		return nil, "", err
	}
	hydrated, err := hydrateClawhubPayload(ctx, cfg, data, slug, version)
	if err != nil {
		return nil, "", err
	}
	return hydrated, u, nil
}

func safePathParts(p string) []string {
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, "\\", "/")
	if p == "" || strings.HasPrefix(p, "/") {
		return nil
	}
	parts := splitURLPath(p)
	for _, part := range parts {
		if part == "." || part == ".." {
			return nil
		}
	}
	return parts
}

func treeInsert(tree map[string]any, parts []string, content string) {
	node := tree
	for _, part := range parts[:len(parts)-1] {
		child, ok := node[part].(map[string]any)
		if !ok {
			child = map[string]any{}
			node[part] = child
		}
		node = child
	}
	node[parts[len(parts)-1]] = content
}

func filesToTrees(files map[string]string) (refs map[string]any, scripts map[string]any) {
	refs = map[string]any{}
	scripts = map[string]any{}
	for rel, content := range files {
		parts := safePathParts(rel)
		if len(parts) == 0 {
			continue
		}
		if parts[0] == "references" && len(parts) > 1 {
			treeInsert(refs, parts[1:], content)
		} else if parts[0] == "scripts" && len(parts) > 1 {
			treeInsert(scripts, parts[1:], content)
		}
	}
	return refs, scripts
}

func sanitizeTree(tree any) map[string]any {
	m, ok := tree.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	out := map[string]any{}
	for k, v := range m {
		if k == "." || k == ".." || strings.ContainsAny(k, `/\`) {
			continue
		}
		switch t := v.(type) {
		case map[string]any:
			out[k] = sanitizeTree(t)
		case string:
			out[k] = t
		}
	}
	return out
}

func normalizeHubBundle(data any) (name, content string, refs, scripts, extra map[string]any, err error) {
	payload := data
	if root, ok := data.(map[string]any); ok {
		if sk, ok := root["skill"].(map[string]any); ok {
			payload = sk
		}
	}
	m, ok := payload.(map[string]any)
	if !ok {
		return "", "", nil, nil, nil, fmt.Errorf("hub bundle is not a JSON object")
	}
	content = ""
	for _, k := range []string{"content", "skill_md", "skillMd"} {
		if s, ok := m[k].(string); ok {
			content = s
			break
		}
	}
	refs = sanitizeTree(m["references"])
	scripts = sanitizeTree(m["scripts"])
	extra = map[string]any{}

	if files, ok := m["files"].(map[string]any); ok {
		flat := map[string]string{}
		for k, v := range files {
			if s, ok := v.(string); ok {
				flat[k] = s
			}
		}
		r2, s2 := filesToTrees(flat)
		if len(refs) == 0 {
			refs = r2
		}
		if len(scripts) == 0 {
			scripts = s2
		}
		for rel, fileContent := range flat {
			if rel == "SKILL.md" {
				if content == "" {
					content = fileContent
				}
				continue
			}
			parts := safePathParts(rel)
			if len(parts) == 0 {
				continue
			}
			if parts[0] == "references" || parts[0] == "scripts" {
				continue
			}
			treeInsert(extra, parts, fileContent)
		}
	}
	if strings.TrimSpace(content) == "" {
		return "", "", nil, nil, nil, fmt.Errorf("hub bundle missing SKILL.md content")
	}
	name = strings.TrimSpace(fmt.Sprint(m["name"]))
	if name == "" {
		name = nameFromSkillMD(content)
	}
	if name == "" {
		return "", "", nil, nil, nil, fmt.Errorf("hub bundle missing skill name")
	}
	return name, content, refs, scripts, extra, nil
}

func nameFromSkillMD(content string) string {
	parts := strings.SplitN(content, "---", 3)
	if len(parts) < 3 {
		return ""
	}
	fm := parts[1]
	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			v := strings.TrimSpace(strings.TrimPrefix(line, "name:"))
			return strings.Trim(v, `"'`)
		}
	}
	return ""
}

var reSafeName = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func safeFallbackName(raw string) string {
	out := strings.Trim(reSafeName.ReplaceAllString(raw, "-"), "-_")
	if out == "" {
		return "imported-skill"
	}
	return out
}

func normalizeSkillKey(text string) string {
	re := regexp.MustCompile(`[^a-z0-9]+`)
	return strings.Trim(re.ReplaceAllString(strings.ToLower(text), "-"), "-")
}

func sanitizeSkillDirName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "imported-skill"
	}
	if strings.ContainsAny(name, `/\`) {
		s := normalizeSkillKey(name)
		if s == "" {
			return safeFallbackName(name)
		}
		return s
	}
	return name
}

func installSkillFromHub(ctx context.Context, mgr *ManagerService, req routers.HubInstallRequest) (routers.HubInstallResult, error) {
	bundleURL := strings.TrimSpace(req.BundleURL)
	if !isHTTPURL(bundleURL) {
		return routers.HubInstallResult{}, &routers.BadRequestError{Msg: "bundle_url must be a valid http(s) URL"}
	}
	cfg := loadHubHTTPConfig()
	var data any
	var sourceURL string
	var err error

	if _, _, _, ok := extractSkillsShSpec(bundleURL); ok {
		data, sourceURL, err = fetchBundleFromSkillsSh(ctx, cfg, bundleURL, req.Version)
	} else if _, _, _, _, ok := extractGitHubSpec(bundleURL); ok {
		data, sourceURL, err = fetchBundleFromGitHubURL(ctx, cfg, bundleURL, req.Version)
	} else if id := extractLobehubID(bundleURL); id != "" {
		data, sourceURL, err = fetchBundleFromLobehub(ctx, cfg, id, bundleURL, req.Version)
	} else if extractSkillsMPSlug(bundleURL) != "" {
		data, sourceURL, err = fetchBundleFromSkillsMP(ctx, cfg, bundleURL, req.Version)
	} else if slug := resolveClawhubSlug(bundleURL); slug != "" {
		data, sourceURL, err = fetchBundleFromClawhubSlug(ctx, cfg, slug, req.Version)
	} else {
		data, err = hubFetchJSON(ctx, cfg, bundleURL)
		sourceURL = bundleURL
	}
	if err != nil {
		return routers.HubInstallResult{}, &routers.UpstreamError{Msg: err.Error()}
	}
	name, content, refs, scripts, extra, err := normalizeHubBundle(data)
	if err != nil {
		return routers.HubInstallResult{}, &routers.BadRequestError{Msg: err.Error()}
	}
	sanitized := sanitizeSkillDirName(name)
	created, cerr := mgr.CreateSkill(ctx, routers.CreateSkillRequest{
		Name:       sanitized,
		Content:    content,
		References: refs,
		Scripts:    scripts,
		ExtraFiles: extra,
		Overwrite:  req.Overwrite,
	})
	if cerr != nil {
		return routers.HubInstallResult{}, &routers.UpstreamError{Msg: cerr.Error()}
	}
	if !created {
		return routers.HubInstallResult{}, &routers.BadRequestError{Msg: fmt.Sprintf("Failed to create skill '%s'. Try overwrite=true if it already exists.", sanitized)}
	}
	enabled := false
	if req.Enable {
		enabled, _ = mgr.EnableSkill(ctx, sanitized)
	}
	return routers.HubInstallResult{Name: sanitized, Enabled: enabled, SourceURL: sourceURL}, nil
}
