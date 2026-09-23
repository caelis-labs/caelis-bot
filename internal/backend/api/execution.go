package api

import "errors"

func ValidateExecutionSettings(v ExecutionSettings) error {
	// Names and allowed combinations are provider-owned. Shared validation only
	// bounds the product payload, including values loaded before connection.
	if len(v.Model) > 256 || len(v.Effort) > 64 || len(v.ServiceTier) > 64 || len(v.ApprovalMode) > 64 {
		return errors.New("模型设置过长")
	}
	if v.Model == "" && (v.Effort != "" || v.ServiceTier != "") {
		return errors.New("请先选择模型")
	}
	return nil
}
