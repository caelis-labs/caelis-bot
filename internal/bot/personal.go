package bot

import (
	"errors"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

func (r *Runtime) ConfigurePersonal(store api.PersonalTools) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped || r.cancel != nil && r.personal != nil {
		return errors.New("个人空间须在连接前绑定")
	}
	if store == nil {
		return errors.New("个人空间不可为空")
	}
	// A durable capability transition at connection assembly, not an incidental
	// note edit or context compaction. Unknown future versions fail closed on load.
	if r.state.PersonalVersion == 0 {
		r.state.PersonalVersion = 1
		if e := r.saveLocked(); e != nil {
			r.state.PersonalVersion = 0
			return e
		}
	}
	r.personal = store
	return nil
}
