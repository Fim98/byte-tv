//go:build !darwin && !windows

package main

// powerAssertion Linux 下为空实现（仅用于开发/CI）。
type powerAssertion struct{}

func (p *powerAssertion) acquire(string) {}
func (p *powerAssertion) release()       {}
