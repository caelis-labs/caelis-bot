//go:build !darwin && !linux

package managedupdate

import "os"

func privateFile(string) error             { return problem("platform_unsupported") }
func privateDirectory(string) error        { return problem("platform_unsupported") }
func lockDirectory(string) (func(), error) { return nil, problem("platform_unsupported") }
func diskBudget(string, int64) error       { return problem("platform_unsupported") }

func acquireLease(string) (*os.File, error) { return nil, problem("platform_unsupported") }
func RetainChildLease(string) error         { return problem("platform_unsupported") }
