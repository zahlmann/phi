package skills

import (
	"os"
	"path/filepath"
	"strings"
)

type Skill struct {
	Name                   string `json:"name"`
	Description            string `json:"description"`
	FilePath               string `json:"filePath"`
	BaseDir                string `json:"baseDir"`
	Source                 string `json:"source"`
	DisableModelInvocation bool   `json:"disableModelInvocation"`
}

type Diagnostic struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}

type LoadResult struct {
	Skills      []Skill      `json:"skills"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

func LoadFromDir(dir string) LoadResult {
	result := LoadResult{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{
				Type:    "error",
				Message: walkErr.Error(),
				Path:    path,
			})
			return nil
		}
		if d.IsDir() || strings.ToUpper(d.Name()) != "SKILL.MD" {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{
				Type:    "error",
				Message: err.Error(),
				Path:    path,
			})
			return nil
		}
		skill := parseSkill(path, string(content))
		if skill.Name == "" {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{
				Type:    "warning",
				Message: "missing skill name, using directory name",
				Path:    path,
			})
			skill.Name = filepath.Base(filepath.Dir(path))
		}
		result.Skills = append(result.Skills, skill)
		return nil
	})
	if err != nil {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{
			Type:    "error",
			Message: err.Error(),
			Path:    dir,
		})
	}
	return result
}

func parseSkill(path, content string) Skill {
	skill := Skill{
		FilePath: path,
		BaseDir:  filepath.Dir(path),
		Source:   content,
	}
	if !strings.HasPrefix(content, "---\n") {
		return skill
	}
	parts := strings.SplitN(content, "\n---\n", 2)
	if len(parts) != 2 {
		return skill
	}
	for _, line := range strings.Split(parts[0], "\n") {
		if line == "---" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k := strings.TrimSpace(strings.ToLower(key))
		v := strings.TrimSpace(value)
		v = strings.Trim(v, "\"")
		switch k {
		case "name":
			skill.Name = v
		case "description":
			skill.Description = v
		case "disablemodelinvocation":
			skill.DisableModelInvocation = strings.EqualFold(v, "true")
		}
	}
	return skill
}
