package applescript

import "github.com/giomhern/psst/internal/domain"

type resultBuffer struct{ data []byte }

func (b *resultBuffer) Write(data []byte) (int, error) {
	if len(data) > 64-len(b.data) {
		return 0, domain.ErrSendUnknown
	}
	b.data = append(b.data, data...)
	return len(data), nil
}
