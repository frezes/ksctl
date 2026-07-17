package securefile

import (
	"errors"
	"os"
	"path/filepath"
)

func Write(path string, data []byte) (retErr error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	temp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() {
		if temp != nil {
			retErr = errors.Join(retErr, temp.Close())
		}
		if retErr != nil {
			_ = os.Remove(tempPath)
		}
	}()

	if err := temp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		temp = nil
		return err
	}
	temp = nil
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	return nil
}
