package pimodel

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// AddProvider runs only after an explicit next-model switch has reaped the old
// CLI. Existing provider entries (and refreshed OAuth tokens) remain snapshots.
func (c *Connections) AddProvider(source, target, provider string) error {
	if err := c.Prepare(source, target, provider); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(c.root, ".pi-provider-")
	if err != nil {
		return errors.New("无法准备新模型凭据")
	}
	defer os.RemoveAll(stage)
	if err = c.Prepare(source, stage, provider); err != nil {
		return err
	}
	for _, name := range []string{"auth.json", "models.json"} {
		path := filepath.Join(target, name)
		old, err := readConfigObject(path)
		if err != nil {
			return err
		}
		fresh, err := readConfigObject(filepath.Join(stage, name))
		if err != nil {
			return err
		}
		dest, src := old, fresh
		if name == "models.json" {
			dest = map[string]json.RawMessage{}
			src = map[string]json.RawMessage{}
			if json.Unmarshal(old["providers"], &dest) != nil || json.Unmarshal(fresh["providers"], &src) != nil {
				return errors.New("会话模型配置无效")
			}
		}
		if _, ok := dest[provider]; ok {
			continue
		}
		value, ok := src[provider]
		if !ok {
			continue
		}
		dest[provider] = value
		if name == "models.json" {
			old["providers"], _ = json.Marshal(dest)
		}
		b, _ := json.Marshal(old)
		f, err := os.CreateTemp(target, ".pi-provider-")
		if err != nil {
			return errors.New("无法扩充会话模型")
		}
		tmp := f.Name()
		err = f.Chmod(0600)
		if err == nil {
			_, err = f.Write(b)
		}
		if err == nil {
			err = f.Sync()
		}
		ce := f.Close()
		if err == nil {
			err = ce
		}
		if err == nil {
			err = os.Rename(tmp, path)
		}
		_ = os.Remove(tmp)
		if err != nil {
			return errors.New("无法扩充会话模型")
		}
	}
	return nil
}
func readConfigObject(path string) (map[string]json.RawMessage, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4<<20 {
		return nil, errors.New("会话配置归属或大小异常")
	}
	b, err := os.ReadFile(path)
	data := map[string]json.RawMessage{}
	if err != nil || json.Unmarshal(b, &data) != nil || data == nil {
		return nil, errors.New("会话配置无效")
	}
	return data, nil
}
