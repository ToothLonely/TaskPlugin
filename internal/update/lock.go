package update

import (
	"os"

	"git-task/internal/diagnostic"
)

func acquireLock(path string) (func() error, error) {
	lock := path + ".update-lock"
	if err := os.Mkdir(lock, 0700); err != nil {
		return nil, diagnostic.Errorf("не удалось получить блокировку %s; проверьте другое обновление или прерванную операцию: %w", lock, err)
	}
	return func() error { return os.Remove(lock) }, nil
}
