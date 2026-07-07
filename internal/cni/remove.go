package cni

import (
	"encoding/json"
	"errors"
	"os"
)

// entry describes an entry in the log that can be undone.
type entry interface {
	filename() string
	revert() error
}

// Remove implements Installer. It walks through the installer's log of entries
// and reverts them.  It collects errors and attempts to complete the entire
// revert process before returning.
func (i *installer) Remove() error {
	var errs []error
	for _, event := range i.log {
		err := event.revert()
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

type cniFile struct {
	name string
}

func (f cniFile) filename() string {
	return f.name
}

// revert changes to a cni file.  The linkerd plugin is gracefully removed from
// the set (e.g. if it does not exist -> revert is a noop).
func (f cniFile) revert() error {
	data, err := os.ReadFile(f.name)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var val map[string]any
	err = json.Unmarshal(data, &val)
	if err != nil {
		return err
	}
	plugins, ok := val[cniKeyPlugins].([]any)
	if !ok {
		return errNoCNIPlugins
	}
	linkerdAt := -1
	for i := 0; i < len(plugins); i++ {
		plugin, ok := plugins[i].(map[string]any)
		if !ok {
			return errInvalidCNIPlugin
		}
		if pluginType, ok := plugin[cniKeyType]; ok && pluginType == cniValTypeLinkerd {
			linkerdAt = i
			break
		}
	}
	if linkerdAt >= 0 {
		plugins = append(plugins[:linkerdAt], plugins[linkerdAt+1:]...)
		val[cniKeyPlugins] = plugins
		data, err = json.MarshalIndent(val, "", "  ")
		if err != nil {
			return err
		}
		stat, err := os.Stat(f.name)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		return os.WriteFile(f.name, data, stat.Mode())
	}
	return nil
}

type installedFile struct {
	name string
}

func (f installedFile) filename() string {
	return f.name
}

func (f installedFile) revert() error {
	if _, err := os.Stat(f.name); err == nil {
		return os.Remove(f.name)
	}
	return nil
}

type k8sFile struct {
	name string
}

func (f k8sFile) filename() string {
	return f.name
}

func (f k8sFile) revert() error {
	if _, err := os.Stat(f.name); err == nil {
		return os.Remove(f.name)
	}
	return nil
}
