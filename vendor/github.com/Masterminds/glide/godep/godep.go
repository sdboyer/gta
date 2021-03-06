// Package godep provides basic importing of Godep dependencies.
//
// This is not a complete implementation of Godep.
package godep

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/Masterminds/glide/cfg"
	"github.com/Masterminds/glide/msg"
	gpath "github.com/Masterminds/glide/path"
	"github.com/Masterminds/glide/util"
)

// This file contains commands for working with Godep.

// The Godeps struct from Godep.
//
// https://raw.githubusercontent.com/tools/godep/master/dep.go
//
// We had to copy this because it's in the package main for Godep.
type Godeps struct {
	ImportPath string
	GoVersion  string
	Packages   []string `json:",omitempty"` // Arguments to save, if any.
	Deps       []Dependency

	outerRoot string
}

// Dependency is a modified version of Godep's Dependency struct.
// It drops all of the unexported fields.
type Dependency struct {
	ImportPath string
	Comment    string `json:",omitempty"` // Description of commit, if present.
	Rev        string // VCS-specific commit ID.
}

// Has is a command to detect if a package contains a Godeps.json file.
func Has(dir string) bool {
	path := filepath.Join(dir, "Godeps/Godeps.json")
	_, err := os.Stat(path)
	return err == nil
}

// Parse parses a Godep's Godeps file.
//
// It returns the contents as a dependency array.
func Parse(dir string) ([]*cfg.Dependency, error) {
	path := filepath.Join(dir, "Godeps/Godeps.json")
	if _, err := os.Stat(path); err != nil {
		return []*cfg.Dependency{}, nil
	}
	msg.Info("Found Godeps.json file in %s", gpath.StripBasepath(dir))
	msg.Info("--> Parsing Godeps metadata...")

	buf := []*cfg.Dependency{}

	godeps := &Godeps{}

	// Get a handle to the file.
	file, err := os.Open(path)
	if err != nil {
		return buf, err
	}
	defer file.Close()

	dec := json.NewDecoder(file)
	if err := dec.Decode(godeps); err != nil {
		return buf, err
	}

	seen := map[string]bool{}
	for _, d := range godeps.Deps {
		pkg, _ := util.NormalizeName(d.ImportPath)
		if !seen[pkg] {
			seen[pkg] = true
			dep := &cfg.Dependency{Name: pkg, Version: d.Rev}
			buf = append(buf, dep)
		}
	}

	return buf, nil
}

func AsMetadataPair(dir string) ([]*cfg.Dependency, *cfg.Lockfile, error) {
	path := filepath.Join(dir, "Godeps/Godeps.json")
	if _, err := os.Stat(path); err != nil {
		return nil, nil, err
	}

	var m []*cfg.Dependency
	l := &cfg.Lockfile{}
	godeps := &Godeps{}

	// Get a handle to the file.
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()

	dec := json.NewDecoder(file)
	if err := dec.Decode(godeps); err != nil {
		return nil, nil, err
	}

	seen := map[string]bool{}
	for _, d := range godeps.Deps {
		pkg, _ := util.NormalizeName(d.ImportPath)
		if _, ok := seen[pkg]; !ok {
			seen[pkg] = true

			// Place no real *actual* constraint on the project; instead, we
			// rely on gps using the 'preferred' version mechanism by
			// working from the lock file. Without this, users would end up with
			// the same mind-numbing diamond dep problems as currently exist.
			// This approach does make for an uncomfortably wide possibility
			// space where deps aren't getting what they expect, but that's
			// better than just having the solver give up completely.
			m = append(m, &cfg.Dependency{Name: pkg})
			l.Imports = append(l.Imports, &cfg.Lock{Name: pkg, Revision: d.Rev})

			// TODO this fails to differentiate between dev and non-dev imports;
			// need static analysis for that
		}
	}

	return m, l, nil
}
