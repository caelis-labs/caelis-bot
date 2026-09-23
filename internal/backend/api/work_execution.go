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
