package filestore

import (
	"errors"
	"os"
	"path"

	"github.com/adrg/xdg"
	"github.com/konflux-ci/konflux-ci/pkg/konftool/webapps"
	"github.com/mitchellh/mapstructure"
	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/afero"
)

// A store that stores data in a TOML file. The data is cached in memory and
// written with a write-throu strategy
type Filestore struct {
	Filepath string // Optional: a full path to the configuration file, if not
	                // specified, an appropriate system-specific default
					// location would be used
	Fs afero.Fs     // Optional: a filesystem to work with, in not specified, 
	                // afero.OsFs would be used
	cache map[string]interface{}
}

func (s *Filestore) filepath() string {
	if s.Filepath == "" {
		s.Filepath = path.Join(xdg.ConfigHome, "konftool", "konftool.toml")
	}
	return s.Filepath
}

func (s *Filestore) fs() afero.Fs {
	if s.Fs == nil {
		s.Fs = afero.NewOsFs()
	}
	return s.Fs
}

func (s *Filestore) getCache() (map[string]interface{}, error) {
	if s.cache != nil {
		return s.cache, nil
	}
	if f, err := s.fs().Open(s.filepath()); err == nil {
		defer f.Close()
		d := toml.NewDecoder(f)
		err := d.Decode(&s.cache)
		return s.cache, err
	} else if errors.Is(err, os.ErrNotExist) {
		s.cache = make(map[string]interface{})
		return s.cache, nil
	} else {
		return nil, err
	}
}

func (s *Filestore) Get(key string, value interface{}) error {
	cache, err := s.getCache()
	if err != nil {
		return webapps.CantReadStoreErr(err)
	}
	rawValue, ok := cache[key]
	if !ok {
		return webapps.KeyNotFound(key)
	}
	return webapps.CantReadStoreErr(mapstructure.Decode(rawValue, value))
}

func (s *Filestore) Put(key string, value interface{}) error {
	cache, err := s.getCache()
	if err != nil {
		return webapps.CantReadStoreErr(err)
	}
	cache[key] = value
	// We make a somewhat silly assumption here that we can write sructs 
	// directly to TOML but then read them from TOML into map[string]interface{}
	// and from there to strucst via maptructure and hope it somehow works
	// consistently
	return webapps.CantWriteToStoreErr(s.writeCache())
}

func (s *Filestore) writeCache() error {
	fs := s.fs()
	filepath := s.filepath()
	if err := fs.MkdirAll(path.Dir(filepath), os.ModeDir|0700); err != nil {
		return err
	}
	if file, err := fs.OpenFile(filepath, os.O_RDWR|os.O_CREATE, os.ModeExclusive|0600); err != nil {
		return err
	} else {
		defer file.Close()
		encoder := toml.NewEncoder(file)
		return encoder.Encode(s.cache)
	}
}
