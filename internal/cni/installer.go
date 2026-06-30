package cni

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/fsnotify/fsnotify"
)

const (
	// serviceAccountDir is the directory at which the service account token
	// resides.
	serviceAccountDir           = "/var/run/secrets/kubernetes.io/serviceaccount"
	serviceAccountTokenFilename = serviceAccountDir + "/token"
)

var (
	// base64C is the codec used to encode values into the config.
	base64C = base64.StdEncoding
)

// An Installer that performs a single static installation of all binaries from
// the directory container's install directory to directory representing the
// underlying host's mount point for plugin binaries.
//
// It re-writes the kubeconfig file inserting the service account token.
//
// It re-writes cni configuration injecting linkerd.
//
// It then use inotify (via fsnotify) to register watches against the service
// account token file, as well as the cni configuration root. If events for
// either watch fire the corresponding configuration is rewritten.
//
// If an error occurs it is returned.
type Installer interface {
	// Remove the cni install.
	Remove() error
	// Run the cni installer.
	Run(context.Context) error
}

// NewInstaller returns an instance of the cni plugin's installer.
func NewInstaller() Installer {
	return &installer{
		fileHashSet:                 map[string]string{},
		log:                         []entry{},
		logIdx:                      map[string]struct{}{},
		serviceAccountTokenFilename: serviceAccountTokenFilename,
		sources: []source{
			&environmentSource{
				key: "CNI_NETWORK_CONFIG",
			},
			&fileSource{
				filename: cniNetworkConfigFile.get(),
			},
		},
	}
}

type installer struct {
	// fileHashSet tracks the hex encoded hash of a file. Indexed by filename.
	fileHashSet map[string]string
	// log of entries performed by the installer in order for remove to revert
	log []entry
	// track log entries
	logIdx map[string]struct{}
	// serviceAccountTokenFilename is the filename in which the kubernetes
	// service account token is set.
	serviceAccountTokenFilename string
	// sources used to configure the plugin.
	sources []source
	// watcherErrors is created by watchFS; it is a copy of the error channel
	// created by fsnotify.
	watcherErrors chan error
	// watcherEvents is created by watchFS; it is a copy of the event channel
	// created by fsnotify.
	watcherEvents chan fsnotify.Event
}

// appendEntry to the log.  If the entry filename is already indexed by the log
// appendEntry is a noop.
func (i *installer) appendEntry(e entry) {
	if _, ok := i.logIdx[e.filename()]; ok {
		return
	}
	i.log = append(i.log, e)
	i.logIdx[e.filename()] = struct{}{}
}

// hashEncode uses sha256 to create a checksum of a file and returns the hex
// encoding.
func hashEncode(data []byte) string {
	// use a sha256 hash to verify the files that were installed
	hash := sha256.New()
	_, err := hash.Write(data)
	if err != nil {
		// unreachable code writing data to the hash does not return an error
		panic(fmt.Sprintf("cannot hash data=%v err=%v", data, err))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// fileSource pulls configuration from a local file.
type fileSource struct {
	filename string
}

// name implements source.
func (fs *fileSource) name() string {
	return fmt.Sprintf("file:%s", fs.filename)
}

// read implements source.
func (fs *fileSource) read() ([]byte, error) {
	return os.ReadFile(fs.filename)
}

// environmentSource pulls configuration from an environment variable.
type environmentSource struct {
	// key used to grab configuration from the environment.
	key string
}

// read implements source.
func (es *environmentSource) read() ([]byte, error) {
	val := os.Getenv(es.key)
	return []byte(val), nil
}

// name implements source.
func (es *environmentSource) name() string {
	return fmt.Sprintf("env:%s", es.key)
}
