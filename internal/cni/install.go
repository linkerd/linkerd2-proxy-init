package cni

import (
	"fmt"
	"io"
	"os"
	"path"
)

// install all sources (src) to dst. The destination (dst) must exist as a
// directory. Permissions on sources are preserved.
//
// A slice of destination paths is returned or nil and an error.
func (i *installer) install(dst string, src ...string) ([]string, error) {
	info, err := os.Stat(dst)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("dst=%s is not a directory", dst)
	}
	var dstFiles []string
	for _, srcP := range src {
		dstP, err := copyFile(dst, srcP)
		if err != nil {
			return nil, err
		}
		i.appendEntry(&installedFile{dstP})
		dstFiles = append(dstFiles, dstP)
	}
	return dstFiles, nil
}

// installRegularFiles copies all regular files found in src to dst.
func (i *installer) installRegularFiles(dst, src string) ([]string, error) {
	entries, err := os.ReadDir(src)
	if err != nil {
		return nil, err
	}
	srcFiles := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !entry.Type().IsRegular() {
			continue
		}
		srcFiles = append(srcFiles, path.Join(src, entry.Name()))
	}
	dstFiles, err := i.install(dst, srcFiles...)
	if err != nil {
		return dstFiles, err
	}
	return dstFiles, nil
}

// copyFile copies src (file) to dst (dir) and return the copied file and nil or
// an empty string and an error.
func copyFile(dst, src string) (string, error) {
	info, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	dstTmpFile := path.Join(dst, fmt.Sprintf("%s.install", path.Base(src)))
	dstW, err := os.OpenFile(path.Clean(dstTmpFile),
		os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
	if err != nil {
		return "", err
	}
	srcR, err := os.Open(path.Clean(src))
	if err != nil {
		_ = dstW.Close()
		return "", err
	}
	_, err = io.Copy(dstW, srcR)
	if err != nil {
		_ = dstW.Close()
		_ = srcR.Close()
		return "", err
	}
	if err = dstW.Sync(); err != nil {
		_ = dstW.Close()
		return "", err
	}
	if err = dstW.Close(); err != nil {
		return "", err
	}
	dstFile := path.Join(dst, path.Base(src))
	return dstFile, os.Rename(dstTmpFile, dstFile)
}
