// Package catalog resolves modules from local catalog directories and computes
// their content digests.
package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Dhananjay-JSR/Plinth/internal/module"
)

// Catalog is a named local directory; modules live at
// <Dir>/modules/<kind>/<flavor>/<version>/.
type Catalog struct {
	Name string
	Dir  string
}

type Resolved struct {
	Module  *module.Module
	Catalog string
	Digest  string // "sha256:<hex>" over the module directory's files
}

// Resolve finds exactly one module with this identity across catalogs.
func Resolve(catalogs []Catalog, kind, flavor, version string) (*Resolved, error) {
	id := kind + "/" + flavor + "/" + version
	// These become path components: reject anything that could escape the catalog.
	if !module.KindPattern.MatchString(kind) || !module.IdentPattern.MatchString(flavor) || !module.VersionPattern.MatchString(version) {
		return nil, fmt.Errorf("invalid module identity %q", id)
	}
	var found []*Resolved
	var searched []string
	for _, c := range catalogs {
		searched = append(searched, c.Name)
		dir := filepath.Join(c.Dir, "modules", kind, flavor, version)
		if _, err := os.Stat(filepath.Join(dir, module.ContractFile)); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		m, err := module.Load(dir)
		if err != nil {
			return nil, err
		}
		if m.ID() != id {
			return nil, fmt.Errorf("%s: contract declares %s but lives at %s", dir, m.ID(), id)
		}
		digest, err := Digest(dir)
		if err != nil {
			return nil, err
		}
		found = append(found, &Resolved{Module: m, Catalog: c.Name, Digest: digest})
	}
	switch len(found) {
	case 0:
		return nil, fmt.Errorf("module %s not found in catalogs [%s]", id, strings.Join(searched, ", "))
	case 1:
		return found[0], nil
	default:
		return nil, fmt.Errorf("module %s found in more than one catalog (%s, %s)", id, found[0].Catalog, found[1].Catalog)
	}
}

// Digest hashes every regular file under dir (relative path and contents, in
// lexical order). Symlinks and other special files are rejected so a module can
// never pull in content from outside its directory.
func Digest(dir string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s: only regular files are allowed in a module", path)
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", filepath.ToSlash(rel), len(data))
		h.Write(data)
		return nil
	})
	if err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
