package desktop

// UI-thread owned. Closing one retained window invalidates only that window's
// outstanding paint acknowledgement, never its sibling's presentation or work.
type surfacePresentations struct {
	serial uint64
	tokens map[string]uint64
}

func (p *surfacePresentations) prepare(surface string) uint64 {
	p.serial++
	if p.tokens == nil {
		p.tokens = make(map[string]uint64)
	}
	p.tokens[surface] = p.serial
	return p.serial
}
func (p *surfacePresentations) cancel(surface string) { delete(p.tokens, surface) }
func (p *surfacePresentations) accept(surface string, token uint64) bool {
	if token == 0 || p.tokens[surface] != token {
		return false
	}
	delete(p.tokens, surface)
	return true
}
