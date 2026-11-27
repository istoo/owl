package plugin

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// A dist tarball's add is judged by the copy Bun leaves in node_modules,
// not by what Bun says. Bun 1.4.4 installs a local tarball from its
// install cache (<cache>/@T@sha512-<32 hex>@@@1), trusting that entry
// whole once it is there: an entry missing a file (cut short, or a file
// taken out of it since) gives a copy missing that file, Bun saying "1
// package installed" and exiting 0. And a copy it can't make (a file of
// the old one held open on Windows: "EBUSY: failed copying files from
// cache to destination") still has it print "installed …", write the
// dependency and exit 1 with "Failed to install 1 package"
// (CHENGJIAHEGOXIS on X: "Unexpected: failed copying files …" adding
// GoAnyAPI on v0.1.4229). The tarball owl checked against the index's
// sha512 is in hand, so the copy is checked against it, file by file.

// errDistPartial is a dist add whose copy in node_modules isn't the
// tarball's, after the add was tried again: the plugin isn't installed,
// whatever Bun said.
var errDistPartial = errors.New("the plugin's files didn't all reach plugins/node_modules")

// distAgainAfter is the pause before a dist add is tried again over a
// copy that wasn't whole: what held a file of it (an antivirus' scan, a
// host still letting go) has a moment to finish. Moved by tests.
var distAgainAfter = 2 * time.Second

// addDist adds the dist tarball file (plugins/dist/<file>, its bytes tgz)
// as name's, and checks the copy Bun made. A copy that isn't the
// tarball's is taken out with Bun's cached copy of the tarball, and the
// add tried once more; one still not whole is made by owl from tgz
// (unpackDist) and Bun run over it; one not whole even then is
// errDistPartial, with what Bun said. A whole copy is name installed:
// an error then is Bun's over another package, as before.
func addDist(ctx context.Context, bun, name, file string, tgz []byte) error {
	err := bunAdd(ctx, bun, "./dist/"+file, name)
	bad := distCopyDiffers(tgz, name)
	if bad == "" {
		return err
	}
	dropDistCopy(name, tgz)
	select {
	case <-ctx.Done():
		return fmt.Errorf("%w (%s): %w", errDistPartial, bad, ctx.Err())
	case <-time.After(distAgainAfter):
	}
	err = bunAdd(ctx, bun, "./dist/"+file, name)
	bad2 := distCopyDiffers(tgz, name)
	if bad2 == "" {
		return err
	}
	// Bun can't make the copy at all: owl makes it from the tarball
	// it verified, and Bun takes it as installed from then on
	if uerr := distUnpack(name, tgz); uerr != nil {
		log.Printf("plugins: %s: Bun's copy %s, and owl's own can't be made: %v", name, bad2, uerr)
	} else {
		// Bun again, over owl's copy: it writes the dependency and
		// bun.lock, leaves the copy be, and says what else failed
		err = bunAdd(ctx, bun, "./dist/"+file, name)
		if bad2 = distCopyDiffers(tgz, name); bad2 == "" {
			log.Printf("plugins: %s: Bun couldn't copy it twice; installed from the verified tarball", name)
			return err
		}
	}
	if err == nil {
		return fmt.Errorf("%w (%s)", errDistPartial, bad2)
	}
	return fmt.Errorf("%w (%s): %w", errDistPartial, bad2, err)
}

// distCopyDiffers says how node_modules/<name> isn't the package in tgz,
// or "" when every file of tgz is there with its bytes. Files the copy has
// besides (Bun's .bun-tag) don't count.
func distCopyDiffers(tgz []byte, name string) string {
	at := filepath.Join(Dir(), "node_modules", filepath.FromSlash(name))
	gz, err := gzip.NewReader(bytes.NewReader(tgz))
	if err != nil {
		return "the tarball can't be read: " + err.Error()
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	files := 0
	var total int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "the tarball can't be read: " + err.Error()
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
			continue
		}
		// npm pack's files are under package/; the first part is the
		// package's folder whatever it is named, as Bun takes it
		clean := path.Clean(strings.TrimPrefix(h.Name, "./"))
		_, rel, ok := strings.Cut(clean, "/")
		if !ok || rel == "" {
			continue
		}
		// a name leaving the package's folder is refused here too: on
		// Windows a backslash in it is a separator, and filepath.Join
		// would read the file outside node_modules
		if !filepath.IsLocal(filepath.FromSlash(rel)) {
			continue
		}
		if err := checkTarSize(h.Size, &total); err != nil {
			return "the tarball can't be read: " + err.Error()
		}
		want, err := io.ReadAll(io.LimitReader(tr, maxTarFile+1))
		if err != nil {
			return "the tarball can't be read: " + err.Error()
		}
		if int64(len(want)) > maxTarFile {
			return "the tarball can't be read: a file is larger than " + sizeText(maxTarFile)
		}
		files++
		got, err := os.ReadFile(filepath.Join(at, filepath.FromSlash(rel)))
		if errors.Is(err, fs.ErrNotExist) {
			return rel + " is missing"
		}
		if err != nil {
			return rel + " can't be read: " + err.Error()
		}
		if !bytes.Equal(got, want) {
			return rel + " isn't the tarball's"
		}
	}
	if files == 0 {
		return "the tarball has no files"
	}
	return ""
}

// dropDistCopy takes out node_modules/<name> and Bun's cached copy of
// tgz: the next add extracts the tarball afresh. Best-effort: a file
// still held stays, and the next add says so.
func dropDistCopy(name string, tgz []byte) {
	removeTree(filepath.Join(Dir(), "node_modules", filepath.FromSlash(name)))
	sum := sha512.Sum512(tgz)
	entry := "@T@sha512-" + hex.EncodeToString(sum[:])[:32] + "@@@1"
	root := installCacheDir()
	removeTree(filepath.Join(root, entry))
	os.Remove(filepath.Join(root, filepath.FromSlash(name), entry))
}

// removeTree takes out dir, making its folders writable where that is
// what stops it (a read-only folder's files can't be unlinked on unix).
func removeTree(dir string) {
	if os.RemoveAll(dir) == nil {
		return
	}
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			os.Chmod(p, 0o755)
		}
		return nil
	})
	os.RemoveAll(dir)
}

// distUnpack is unpackDist; tests make it fail.
var distUnpack = unpackDist

// unpackDist puts the package in tgz, a dist tarball checked against the
// index's sha512, at node_modules/<name> itself, when Bun can't copy it
// there from its cache: CHENGJIAHEGOXIS on X (v0.1.4241) got "Unexpected:
// failed copying files from cache to destination" adding GoAnyAPI at
// both tries, which on Windows is CopyFileW failing with a Win32 code
// Bun has no errno for (Bun 1.4.4's hardlink backend falls back to it,
// and never to anything else on Windows). The copy's .bun-tag is the
// tarball's integrity, as Bun writes it: Bun checks an installed
// tarball by that alone and leaves the copy as it is at every later
// command (Bun 1.4.4, with bun.lock and without). Only a package with no
// dependencies of its own: Bun would have to install those.
func unpackDist(name string, tgz []byte) error {
	files, err := tarballFiles(tgz)
	if err != nil {
		return err
	}
	var pj struct {
		Name                 string
		Dependencies         map[string]any
		OptionalDependencies map[string]any
		PeerDependencies     map[string]any
		BundleDependencies   any
		BundledDependencies  any
	}
	b, ok := files["package.json"]
	if !ok {
		return errors.New("the tarball has no package.json")
	}
	if err := json.Unmarshal(b.body, &pj); err != nil {
		return fmt.Errorf("the tarball's package.json: %w", err)
	}
	if pj.Name != name {
		return fmt.Errorf("the tarball is %q's, not %s's", pj.Name, name)
	}
	if len(pj.Dependencies)+len(pj.OptionalDependencies)+len(pj.PeerDependencies) > 0 || pj.BundleDependencies != nil || pj.BundledDependencies != nil {
		return errors.New("the package has dependencies of its own, which only Bun installs")
	}
	at := filepath.Join(Dir(), "node_modules", filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(at), "."+path.Base(name)+"-")
	if err != nil {
		return err
	}
	defer removeTree(tmp)
	for rel, f := range files {
		to := filepath.Join(tmp, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(to, f.body, f.mode); err != nil {
			return err
		}
	}
	sum := sha512.Sum512(tgz)
	if err := os.WriteFile(filepath.Join(tmp, ".bun-tag"), []byte("sha512-"+base64.StdEncoding.EncodeToString(sum[:])), 0o644); err != nil {
		return err
	}
	removeTree(at)
	return os.Rename(tmp, at)
}

// tarFile is a regular file of a tarball: its bytes and the mode it is
// written with.
type tarFile struct {
	body []byte
	mode fs.FileMode
}

// maxTarFile is the most one file of a tarball may hold, unpacked, and the
// most every file of it together: a gzip bomb is a few KB that expands to
// gigabytes, and reading one into memory ends the host.
const maxTarFile = 64 << 20

// checkTarSize says h fits the single-file and total limits, adding it to
// total.
func checkTarSize(h int64, total *int64) error {
	if h < 0 || h > maxTarFile {
		return fmt.Errorf("a file of %s is past the %s limit", sizeText(h), sizeText(maxTarFile))
	}
	if *total += h; *total > maxTarFile {
		return fmt.Errorf("the tarball's files are past %s unpacked", sizeText(maxTarFile))
	}
	return nil
}

// sizeText is n bytes as a reader says it: 64 MiB.
func sizeText(n int64) string {
	if n >= 1<<20 {
		return strconv.FormatInt(n>>20, 10) + " MiB"
	}
	return strconv.FormatInt(n, 10) + " bytes"
}

// tarballFiles is every regular file of an npm pack tarball, by its path
// under the package's folder (the first part of each name, whatever it
// is called, as Bun takes it). A name leaving that folder is refused.
func tarballFiles(tgz []byte) (map[string]tarFile, error) {
	gz, err := gzip.NewReader(bytes.NewReader(tgz))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	files := map[string]tarFile{}
	var total int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
			continue
		}
		clean := path.Clean(strings.TrimPrefix(h.Name, "./"))
		if !filepath.IsLocal(filepath.FromSlash(clean)) {
			return nil, fmt.Errorf("the tarball names %q, outside its package", h.Name)
		}
		_, rel, ok := strings.Cut(clean, "/")
		if !ok || rel == "" {
			continue
		}
		// a gzip bomb names a small file and expands to a huge one: the
		// declared and the read bytes are both bounded
		if err := checkTarSize(h.Size, &total); err != nil {
			return nil, err
		}
		body, err := io.ReadAll(io.LimitReader(tr, maxTarFile+1))
		if err != nil {
			return nil, err
		}
		if int64(len(body)) > maxTarFile {
			return nil, fmt.Errorf("the tarball's %q is larger than %s", rel, sizeText(maxTarFile))
		}
		mode := fs.FileMode(0o644)
		if h.Mode&0o111 != 0 {
			mode = 0o755
		}
		files[rel] = tarFile{body, mode}
	}
	if len(files) == 0 {
		return nil, errors.New("the tarball has no files")
	}
	return files, nil
}
