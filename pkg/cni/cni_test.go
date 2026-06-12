package cni

import (
	"os"
	"path"
	"reflect"
	"testing"
)

// assertDeepEqual uses reflect to compare expected and actual values. It fails
// the test case if they are not equal.
func assertDeepEqual(t *testing.T, expVal, actVal any) {
	t.Helper()
	if !reflect.DeepEqual(expVal, actVal) {
		t.Fatalf("expected value does not equal actual '%v'<>'%v'",
			expVal, actVal)
	}
}

// assertErr checks either expErr and actErr's message are identical or both
// are unset (empty and nil).
//
// Returns true if the actual error is non-nil (i.e. the caller should skip
// any remaining assertions).
func assertErr(t *testing.T, expErr string, actErr error) bool {
	t.Helper()
	if actErr != nil && expErr != "" && actErr.Error() == expErr {
		// the expected error was set and it matches the actual error message
		return true
	}
	if actErr != nil {
		if expErr == "" {
			// an error was returned that was not expected
			t.Fatalf("unexpected error for test=%s err=%v", t.Name(), actErr)
		} else {
			t.Fatalf("expected error from config does not match '%s'<>'%v'", expErr, actErr)
		}
		return true
	}
	return false
}

// mustCopyFiles recursively copies files from one directory (src) to another (dst).
//
// If an error occurs the test is failed.
func mustCopyFiles(t *testing.T, dst, src string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("cannot read dir=%s err=%v", src, err)
	}
	for _, entry := range entries {
		dstPath := path.Join(dst, entry.Name())
		srcPath := path.Join(src, entry.Name())
		if entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				t.Fatalf("cannot stat dir=%s err=%v", srcPath, err)
			}
			if err := os.MkdirAll(dstPath, info.Mode()); err != nil {
				t.Fatalf("cannot create dir=%s err=%v", dstPath, err)
			}
			mustCopyFiles(t, dstPath, srcPath)
			continue
		}
		mustCopyFile(t, dst, srcPath)
	}
}

// mustCopyFile copies src (file) to dst (dir) and returns the copied file. If
// an error occurs the test is failed.
func mustCopyFile(t *testing.T, dst, src string) string {
	t.Helper()
	dstFile, err := copyFile(dst, src)
	if err != nil {
		t.Fatalf("cannot copy file=%s to dir=%s err=%v", src, dst, err)
	}
	return dstFile
}

// mustReadFile reads the file and fails the test if an error occurs. It returns
// the entire file.
func mustReadFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(path.Clean(name))
	if err != nil {
		t.Fatalf("cannot read file name=%s err=%v", name, err)
	}
	return data
}

func mustWriteFile(t *testing.T, name string, data []byte, perm os.FileMode) {
	t.Helper()
	err := os.WriteFile(name, data, perm)
	if err != nil {
		t.Fatalf("cannot write file name=%s err=%v", name, err)
	}
}

// mustReadUnmarshal reads the file (name) and parses it using the unmarshal
// function into a value, and returns the value.  It fails the test if an
// error occurs.
func mustReadUnmarshal(t *testing.T, name string,
	unmarshalFn func([]byte, any) error) map[string]any {
	t.Helper()
	data := mustReadFile(t, name)
	var val map[string]any
	err := unmarshalFn(data, &val)
	if err != nil {
		t.Fatalf("cannot unmarshal json from file name=%s err=%v", name, err)
	}
	return val
}

// newTestInstaller creates an installer for testing.
func newTestInstaller(t *testing.T) *installer {
	t.Helper()
	return &installer{
		fileHashSet: map[string]string{},
		logIdx:      map[string]struct{}{},
	}
}
