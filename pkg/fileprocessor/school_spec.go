package fileprocessor

import (
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ResolveSchoolSpecPath 解析本校 *.spec.json 绝对路径。
// 优先级：explicitPath > env FORMATTER_SCHOOL_SPEC > schoolID 在常见目录下 Glob。
func ResolveSchoolSpecPath(explicitPath, schoolID string) string {
	if p := strings.TrimSpace(explicitPath); p != "" {
		if fileExists(p) {
			return mustAbs(p)
		}
		log.Printf("[SchoolSpec] explicit path not found: %s", p)
	}
	if e := strings.TrimSpace(os.Getenv("FORMATTER_SCHOOL_SPEC")); e != "" {
		if fileExists(e) {
			return mustAbs(e)
		}
		log.Printf("[SchoolSpec] FORMATTER_SCHOOL_SPEC not found: %s", e)
	}
	sid := strings.TrimSpace(schoolID)
	if sid == "" {
		sid = strings.TrimSpace(os.Getenv("SCHOOL_ID"))
	}
	if sid == "" {
		return ""
	}
	if p := findSpecFileForSchoolID(sid); p != "" {
		log.Printf("[SchoolSpec] resolved school_id=%s -> %s", sid, p)
		return p
	}
	return ""
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func mustAbs(p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return a
}

// findSpecFileForSchoolID 在若干根目录下查找 config/schools/{school_id}*.spec.json
func findSpecFileForSchoolID(schoolID string) string {
	roots := schoolSpecSearchRoots()
	pat := schoolID + "*.spec.json"
	var all []string
	for _, root := range roots {
		matches, _ := filepath.Glob(filepath.Join(root, pat))
		all = append(all, matches...)
	}
	if len(all) == 0 {
		return ""
	}
	sort.Strings(all)
	return mustAbs(all[len(all)-1])
}

func schoolSpecSearchRoots() []string {
	var roots []string
	add := func(p string) {
		if p == "" {
			return
		}
		if fileExists(p) {
			roots = append(roots, p)
		}
	}
	add(filepath.Join("backend", "config", "schools"))
	add(filepath.Join("config", "schools"))
	add(filepath.Join(executableDir(), "..", "config", "schools"))
	add(filepath.Join(executableDir(), "config", "schools"))
	add("/opt/paper/config/schools")
	if wd, err := os.Getwd(); err == nil {
		add(filepath.Join(wd, "backend", "config", "schools"))
		add(filepath.Join(wd, "config", "schools"))
	}
	return uniqueStrings(roots)
}

func uniqueStrings(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range in {
		a := mustAbs(s)
		if _, ok := seen[a]; ok {
			continue
		}
		seen[a] = struct{}{}
		out = append(out, a)
	}
	return out
}

// SchoolIDFromUniversityName 将数据库中的高校映射到 spec 的 school_id（可随学校扩展）。
func SchoolIDFromUniversityName(name, abbr string) string {
	n := strings.TrimSpace(name)
	a := strings.ToLower(strings.TrimSpace(abbr))
	if strings.Contains(n, "重庆人文科技学院") {
		return "cq-hr-university"
	}
	if a == "cqrwst" || a == "cq-hr" || a == "cqhr" {
		return "cq-hr-university"
	}
	return ""
}
