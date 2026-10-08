package plugins

import (
	"errors"
	"fmt"
	"strings"
)

// SkillDetail reads the reviewed source only on explicit user inspection.
// The model's normal skill loading still uses the immutable package directory.
func (m *Manager) SkillDetail(id, skill string) (SkillDetail, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entry(id)
	if !ok {
		return SkillDetail{}, errors.New("plugin is not reviewed")
	}
	name := "skills/" + skill + "/SKILL.md"
	if !serviceName.MatchString(skill) {
		return SkillDetail{}, errors.New("invalid skill")
	}
	if m.sources[e.ID] == nil {
		return SkillDetail{}, errors.New("reviewed package source unavailable")
	}
	body, err := reviewedFile(m.sources[e.ID], e, name)
	if err != nil {
		return SkillDetail{}, err
	}
	if nameValue, _ := skillDisplay(body); nameValue == "" {
		return SkillDetail{}, errors.New("skill metadata unavailable")
	}
	_, instructions, ok := strings.Cut(string(body[4:]), "\n---\n")
	if !ok {
		return SkillDetail{}, errors.New("skill instructions unavailable")
	}
	var lines []string
	length := 0
	for _, line := range strings.Split(instructions, "\n") {
		length += len(line) + 1
		if length > 8192 {
			return SkillDetail{}, fmt.Errorf("skill preview exceeds display limit")
		}
		if line == "" {
			lines = append(lines, "")
			continue
		}
		if displayTextBound(line, 8192) == "" {
			continue // Do not send local paths, environment values or secrets to UI.
		}
		lines = append(lines, line)
	}
	return SkillDetail{Body: strings.TrimSpace(strings.Join(lines, "\n"))}, nil
}
