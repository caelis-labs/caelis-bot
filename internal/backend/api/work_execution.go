package api

import (
	"errors"
	"slices"
)

func (v WorkExecutionSettings) Execution() ExecutionSettings {
	return ExecutionSettings{Model: v.Model, Effort: v.Effort, ServiceTier: v.ServiceTier}
}

func WorkModel(v ExecutionSettings) WorkExecutionSettings {
	return WorkExecutionSettings{Model: v.Model, Effort: v.Effort, ServiceTier: v.ServiceTier}
}

func ValidateWorkExecution(v WorkExecutionSettings, catalog []ModelOption) error {
	if err := ValidateExecutionSettings(v.Execution()); err != nil {
		return err
	}
	if err := ValidateNewExecutionMode(v.ApprovalMode); err != nil {
		return err
	}
	if v.Model == "" {
		return nil
	}
	i := slices.IndexFunc(catalog, func(m ModelOption) bool { return m.Model == v.Model })
	if i < 0 {
		return errors.New("该工作模型已不可用，请刷新后重新选择")
	}
	m := catalog[i]
	if !slices.Contains(m.Efforts, v.Effort) && !(v.Effort == "" && len(m.Efforts) == 0) {
		return errors.New("工作模型不支持所选推理强度")
	}
	if v.ServiceTier != "" && !slices.ContainsFunc(m.ServiceTiers, func(t ServiceTier) bool { return t.ID == v.ServiceTier }) {
		return errors.New("工作模型不支持所选响应速度")
	}
	return nil
}

// NormalizeNewExecutionMode keeps historical preferences on the safe default.
func NormalizeNewExecutionMode(mode string) string {
	if mode == "" || mode == "workspace-write" {
		return "auto"
	}
	return mode
}

func ValidateNewExecutionMode(mode string) error {
	switch mode {
	case "", "auto", "full-access":
		return nil
	default:
		return errors.New("执行模式仅支持自动审查或完全访问")
	}
}
