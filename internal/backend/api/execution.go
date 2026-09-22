package api

import "errors"

func ValidateExecutionSettings(v ExecutionSettings) error {
	switch v.ApprovalMode {
	case "", "auto", "ask", "read-only", "full-access":
	default:
		return errors.New("无法识别审批方式")
	}
	if len(v.Model) > 256 || len(v.Effort) > 64 || len(v.ServiceTier) > 64 {
		return errors.New("模型设置过长")
	}
	if v.Model == "" && (v.Effort != "" || v.ServiceTier != "") {
		return errors.New("请先选择模型")
	}
	return nil
}
