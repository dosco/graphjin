// Package localtool provides file capabilities for a host-selected run workspace.
// The host must keep credentials and privileged bridge state outside this root.
package localtool

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/open-neko/openneko/apps/ax-harness/internal/agent"
)

// Read limits match Hermes read_file: 100,000 characters, 2,000 lines and
// 2,000 characters per line for one read.
const (
	maxFile      = 10 << 20
	maxWrite     = 1 << 20
	maxReadChars = 100_000
	maxReadLines = 2_000
	maxLineChars = 2_000
)

type Files struct {
	root  *os.Root
	gate  sync.RWMutex
	mu    sync.Mutex
	reads map[string]string
}

func OpenFiles(dir string) (*Files, error) {
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("workspace path must be absolute")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &Files{root: root, reads: make(map[string]string)}, nil
}

func (f *Files) Close() error { return f.root.Close() }

func (f *Files) Capabilities() []agent.Capability {
	return []agent.Capability{
		{Name: "file_read", Version: "1", Origin: "workspace", Effect: "read", Description: "Read a text file in the run workspace and receive its version. One read returns at most 2,000 lines and 100,000 characters; use offset and limit for later lines.", InputSchema: json.RawMessage(`{"type":"object","required":["path"],"properties":{"path":{"type":"string","minLength":1,"maxLength":1024},"offset":{"type":"integer","minimum":1},"limit":{"type":"integer","minimum":1,"maximum":2000}},"additionalProperties":false}`), Call: f.read},
		{Name: "file_edit", Version: "2", Origin: "workspace", Effect: "durable", Description: "Replace old_string with new_string in a file read in this run. old_string must match exactly once unless replace_all is true. Fails if the file changed since the last read or edit.", InputSchema: json.RawMessage(`{"type":"object","required":["path","old_string","new_string"],"properties":{"path":{"type":"string","minLength":1,"maxLength":1024},"old_string":{"type":"string","minLength":1,"maxLength":1048576},"new_string":{"type":"string","maxLength":1048576},"replace_all":{"type":"boolean"}},"additionalProperties":false}`), Call: f.edit},
		{Name: "file_write", Version: "1", Origin: "workspace", Effect: "durable", Description: "Create a new text file in the run workspace; refuse to replace an existing path.", InputSchema: json.RawMessage(`{"type":"object","required":["path","content"],"properties":{"path":{"type":"string","minLength":1,"maxLength":1024},"content":{"type":"string","maxLength":1048576}},"additionalProperties":false}`), Call: f.write},
		{Name: "file_search", Version: "2", Origin: "workspace", Effect: "read", Description: searchDescription("the run workspace"), InputSchema: searchSchema, Call: f.search},
	}
}

// UploadCapabilities expose a separate staged upload root without granting
// mutations to the original files.
func (f *Files) UploadCapabilities() []agent.Capability {
	all := f.Capabilities()
	read, search := all[0], all[3]
	read.Name, read.Origin = "upload_read", "uploads"
	read.Description = "Read a text file by path relative to this run's staged uploads directory. One read returns at most 2,000 lines and 100,000 characters; use offset and limit for later lines."
	search.Name, search.Origin = "upload_search", "uploads"
	search.Description = searchDescription("the staged uploads")
	return []agent.Capability{read, search}
}

const maxSkillContent = 64 << 10

// Skills indexes each staged SKILL.md by the name of its folder and the
// description in its frontmatter. The body is read only when a search loads it.
func (f *Files) Skills() ([]agent.Skill, error) {
	root, err := f.root.Open(".")
	if err != nil {
		return nil, err
	}
	defer root.Close()
	entries, err := root.ReadDir(0)
	if err != nil {
		return nil, err
	}
	var skills []agent.Skill
	for _, entry := range entries {
		if !entry.IsDir() || !agent.ValidSkillName(entry.Name()) {
			continue
		}
		name := entry.Name()
		head, err := f.skillText(name, 8192)
		if err != nil {
			continue
		}
		description, _, ok := splitFrontmatter(head)
		if !ok {
			continue
		}
		skills = append(skills, agent.Skill{Name: name, Description: description, Load: func() (string, error) {
			text, err := f.skillText(name, maxSkillContent+8192)
			if err != nil {
				return "", err
			}
			_, body, ok := splitFrontmatter(text)
			if !ok {
				return "", fmt.Errorf("skill %s has no frontmatter", name)
			}
			if len(body) > maxSkillContent {
				body = runePrefix(body, maxSkillContent/4-64) + "\n[guide truncated; read the rest with file_read at skills/" + name + "/SKILL.md]"
			}
			return body, nil
		}})
	}
	return skills, nil
}

func (f *Files) skillText(name string, limit int64) (string, error) {
	file, err := f.root.Open(name + "/SKILL.md")
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit))
	if err != nil {
		return "", err
	}
	if !utf8.ValidString(string(data)) {
		return "", fmt.Errorf("skill %s is not UTF-8", name)
	}
	return string(data), nil
}

// splitFrontmatter returns the description and the body of a SKILL.md.
func splitFrontmatter(text string) (string, string, bool) {
	if !strings.HasPrefix(text, "---\n") {
		return "", "", false
	}
	end := strings.Index(text[4:], "\n---")
	if end < 0 {
		return "", "", false
	}
	var description string
	for _, line := range strings.Split(text[4:4+end], "\n") {
		if strings.HasPrefix(line, "description:") {
			description = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "description:")), `"'`)
			break
		}
	}
	return runePrefix(description, 1024), strings.TrimLeft(text[4+end+4:], "\n"), true
}

func runePrefix(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit])
}

func validPath(path string) bool {
	return len(path) <= 1024 && filepath.IsLocal(path) && filepath.Clean(path) == path && path != "."
}

func (f *Files) read(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var input struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &input); err != nil || !validPath(input.Path) || input.Offset < 0 || input.Limit < 0 || input.Limit > maxReadLines {
		return nil, fmt.Errorf("invalid workspace path")
	}
	f.gate.RLock()
	defer f.gate.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, _, err := f.load(input.Path)
	if err != nil {
		return nil, err
	}
	version := digest(data)
	f.mu.Lock()
	f.reads[input.Path] = version
	f.mu.Unlock()
	lines := strings.SplitAfter(string(data), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	start := max(input.Offset, 1)
	limit := input.Limit
	if limit == 0 {
		limit = maxReadLines
	}
	var content strings.Builder
	chars, end, truncated := 0, start-1, false
	for i := start - 1; i < len(lines); i++ {
		if i-(start-1) >= limit {
			truncated = true
			break
		}
		line := lines[i]
		if utf8.RuneCountInString(line) > maxLineChars {
			line = runePrefix(line, maxLineChars) + " [line truncated]\n"
		}
		count := utf8.RuneCountInString(line)
		if chars+count > maxReadChars {
			truncated = true
			break
		}
		content.WriteString(line)
		chars += count
		end = i + 1
	}
	return json.Marshal(struct {
		Path       string `json:"path"`
		Content    string `json:"content"`
		Version    string `json:"version"`
		StartLine  int    `json:"start_line"`
		EndLine    int    `json:"end_line"`
		TotalLines int    `json:"total_lines"`
		Truncated  bool   `json:"truncated"`
	}{input.Path, content.String(), version, start, end, len(lines), truncated})
}

func (f *Files) edit(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var input struct {
		Path       string `json:"path"`
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := json.Unmarshal(raw, &input); err != nil || !validPath(input.Path) || input.OldString == "" || input.OldString == input.NewString ||
		len(input.OldString) > maxWrite || len(input.NewString) > maxWrite {
		return nil, fmt.Errorf("invalid file edit")
	}
	f.gate.Lock()
	defer f.gate.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	readVersion := f.reads[input.Path]
	f.mu.Unlock()
	if readVersion == "" {
		return nil, fmt.Errorf("read the file before editing it")
	}
	old, mode, err := f.load(input.Path)
	if err != nil {
		return nil, err
	}
	if digest(old) != readVersion {
		return nil, fmt.Errorf("file changed since the last read; read it again")
	}
	count := strings.Count(string(old), input.OldString)
	if count == 0 {
		return nil, fmt.Errorf("old_string not found")
	}
	if count > 1 && !input.ReplaceAll {
		return nil, fmt.Errorf("old_string matches %d times; add context or set replace_all", count)
	}
	content := strings.Replace(string(old), input.OldString, input.NewString, -1)
	if len(content) > maxFile {
		return nil, fmt.Errorf("edited file exceeds %d bytes", maxFile)
	}
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	tmp := filepath.Join(filepath.Dir(input.Path), ".harness-edit-"+hex.EncodeToString(nonce[:]))
	file, err := f.root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	defer f.root.Remove(tmp)
	if _, err = io.WriteString(file, content); err == nil {
		err = file.Chmod(mode.Perm())
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	// Catch external edits during the write. Harness-owned edits are serialized by gate.
	current, _, err := f.load(input.Path)
	if err != nil || digest(current) != readVersion {
		return nil, fmt.Errorf("file changed during edit")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := f.root.Rename(tmp, input.Path); err != nil {
		return nil, err
	}
	dir, err := f.root.Open(filepath.Dir(input.Path))
	if err == nil {
		err = dir.Sync()
		_ = dir.Close()
	}
	if err != nil {
		return nil, err
	}
	version := digest([]byte(content))
	f.mu.Lock()
	f.reads[input.Path] = version
	f.mu.Unlock()
	return json.Marshal(struct {
		Path         string `json:"path"`
		Version      string `json:"version"`
		Replacements int    `json:"replacements"`
	}{input.Path, version, count})
}

func (f *Files) write(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var input struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &input); err != nil || !validPath(input.Path) || len(input.Content) > maxWrite {
		return nil, fmt.Errorf("invalid file write")
	}
	f.gate.Lock()
	defer f.gate.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	tmp := filepath.Join(filepath.Dir(input.Path), ".harness-write-"+hex.EncodeToString(nonce[:]))
	file, err := f.root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	defer f.root.Remove(tmp)
	if _, err = io.WriteString(file, input.Content); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Linking is atomic and fails if the destination already exists. Rename
	// could silently replace another file created after admission.
	if err := f.root.Link(tmp, input.Path); err != nil {
		return nil, fmt.Errorf("file already exists or path unavailable: %w", err)
	}
	dir, err := f.root.Open(filepath.Dir(input.Path))
	if err == nil {
		err = dir.Sync()
		_ = dir.Close()
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Path    string `json:"path"`
		Version string `json:"version"`
	}{input.Path, digest([]byte(input.Content))})
}

// Hermes search_files returns 50 results by default; a caller may ask for
// up to 200.
const (
	maxSearchVisits  = 10_000
	maxSearchResults = 200
	maxMatchChars    = 500
)

var searchSchema = json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","minLength":1,"maxLength":1024},"glob":{"type":"string","minLength":1,"maxLength":256},"ignore_case":{"type":"boolean"},"limit":{"type":"integer","minimum":1,"maximum":200}},"additionalProperties":false}`)

func searchDescription(where string) string {
	return "Search " + where + ". pattern is an RE2 regex matched per line and returns {path,line,text} matches. glob filters paths: *.csv matches by file name, reports/**/*.md by path. With glob alone, returns matching paths. Default limit 50."
}

func (f *Files) search(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var input struct {
		Pattern    string `json:"pattern"`
		Glob       string `json:"glob"`
		IgnoreCase bool   `json:"ignore_case"`
		Limit      int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &input); err != nil || input.Pattern == "" && input.Glob == "" || len(input.Pattern) > 1024 || len(input.Glob) > 256 ||
		input.Limit < 0 || input.Limit > maxSearchResults {
		return nil, fmt.Errorf("invalid file search")
	}
	limit := input.Limit
	if limit == 0 {
		limit = 50
	}
	var pattern, glob *regexp.Regexp
	var err error
	if input.Pattern != "" {
		expr := input.Pattern
		if input.IgnoreCase {
			expr = "(?i)" + expr
		}
		if pattern, err = regexp.Compile(expr); err != nil {
			return nil, fmt.Errorf("invalid pattern: %w", err)
		}
	}
	byName := !strings.Contains(input.Glob, "/")
	if input.Glob != "" {
		if glob, err = globRegexp(input.Glob); err != nil {
			return nil, err
		}
	}
	type match struct {
		Path string `json:"path"`
		Line int    `json:"line"`
		Text string `json:"text"`
	}
	paths, matches := make([]string, 0), make([]match, 0)
	visited, truncated := 0, false
	stop := fmt.Errorf("file search limit reached")
	f.gate.RLock()
	defer f.gate.RUnlock()
	err = fs.WalkDir(f.root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if visited++; visited > maxSearchVisits {
			truncated = true
			return stop
		}
		if entry.IsDir() && entry.Name() == ".git" {
			return fs.SkipDir
		}
		if !entry.Type().IsRegular() {
			return nil // WalkDir does not follow directory symlinks.
		}
		if glob != nil {
			subject := path
			if byName {
				subject = entry.Name()
			}
			if !glob.MatchString(subject) {
				return nil
			}
		}
		if pattern == nil {
			paths = append(paths, path)
			if len(paths) == limit {
				truncated = true
				return stop
			}
			return nil
		}
		data, _, err := f.load(path)
		if err != nil {
			return nil // A changed, non-text or oversized file is not a match.
		}
		for i, line := range strings.Split(string(data), "\n") {
			if !pattern.MatchString(line) {
				continue
			}
			matches = append(matches, match{path, i + 1, runePrefix(strings.TrimRight(line, "\r"), maxMatchChars)})
			if len(matches) == limit {
				truncated = true
				return stop
			}
		}
		return nil
	})
	if err != nil && err != stop {
		return nil, err
	}
	if pattern == nil {
		return json.Marshal(struct {
			Paths     []string `json:"paths"`
			Truncated bool     `json:"truncated"`
		}{paths, truncated})
	}
	return json.Marshal(struct {
		Matches   []match `json:"matches"`
		Truncated bool    `json:"truncated"`
	}{matches, truncated})
}

// globRegexp turns a glob into an anchored regex: ** spans directories, * and
// ? stay inside one path segment.
func globRegexp(glob string) (*regexp.Regexp, error) {
	var expr strings.Builder
	expr.WriteString("^")
	for i := 0; i < len(glob); i++ {
		switch c := glob[i]; {
		case strings.HasPrefix(glob[i:], "**/"):
			expr.WriteString("(?:.*/)?")
			i += 2
		case strings.HasPrefix(glob[i:], "**"):
			expr.WriteString(".*")
			i++
		case c == '*':
			expr.WriteString("[^/]*")
		case c == '?':
			expr.WriteString("[^/]")
		default:
			expr.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	expr.WriteString("$")
	re, err := regexp.Compile(expr.String())
	if err != nil {
		return nil, fmt.Errorf("invalid glob")
	}
	return re, nil
}

func (f *Files) load(path string) ([]byte, os.FileMode, error) {
	info, err := f.root.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxFile {
		return nil, 0, fmt.Errorf("workspace file unavailable or too large")
	}
	file, err := f.root.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, 0, fmt.Errorf("workspace file changed during open")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxFile+1))
	if err != nil || len(data) > maxFile || !utf8.Valid(data) {
		return nil, 0, fmt.Errorf("workspace file unreadable or too large")
	}
	return data, opened.Mode(), nil
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
