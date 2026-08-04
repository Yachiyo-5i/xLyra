package usage

import (
	"xlyra/server/internal/config"
)

func usageUTCService() *Service {
	return NewService(nil, config.LoadTimeZone("UTC"))
}
