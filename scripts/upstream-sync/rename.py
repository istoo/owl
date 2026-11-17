#!/usr/bin/env python3
"""Render an upstream yetone/magpie tree in owl's naming.

Usage:
    rename.py SRC DST

SRC is an unpacked upstream tree (for example `git archive upstream/main`),
DST is where the transformed tree is written. The transform is deterministic:
the same input always gives the same output, so a later sync can calibrate it
against a known tree (see scripts/upstream-sync/README.md).

What it changes
---------------
Paths: every path component is run through the text transform, so
`internal/gateway/remote_magpie.go` becomes `internal/gateway/remote_owl.go`.

Text (all files that look like text): the upstream product name becomes owl,
in the three spellings upstream uses:

    github.com/yetone/magpie  -> github.com/istoo/owl   (and any yetone/magpie*)
    MAGPIE_...                -> OWL_...
    Magpie / Magpie.app      -> Owl / Owl.app
    magpie                    -> owl

What it leaves alone
--------------------
The compatibility boundary with the plugins community and with an upstream
install, which is not owl's name to change:

    @magpie-community/...      npm scope of the community plugins
    magpie-community/...       the community plugins repository
    X-Magpie-*, x-magpie-*     gateway request headers
    json:"magpie"              a plugin package.json's manifest key
    "magpie":                  the same key inside JSON text
    magpie_label, magpie_by, magpie_price, magpie_via,
    magpie_cache_unknown, magpie_deferred
                              old JSON fields the catalog and gateway read
    usemagpie                  the upstream site; owl removes those calls
                              itself, and leaving them visible is the point

Upstream's own files that are history (LICENSE, THIRD_PARTY_NOTICES) are
outside the transform only in the sense that owl keeps its own copies; the
transform still runs on them, and the sync resolves them to owl's versions.
"""

import os
import re
import shutil
import subprocess
import sys

# Patterns that keep upstream's spelling. Each match is replaced by a
# sentinel before the generic replacements and put back afterwards.
#
# The plugin manifest key is protected only where it is a JSON key (the
# value follows the colon), because the same spelling appears in prose that
# names a mode, and owl renamed those.
PROTECTED = [
    re.compile(r"@magpie-community"),
    re.compile(r"magpie-community"),
    re.compile(r"X-Magpie-"),
    re.compile(r"x-magpie-"),
    re.compile(r"usemagpie"),
    re.compile(r'json:"magpie"'),
    re.compile(r'"magpie":(?=\s*(?:true|false))'),
    re.compile(r'"magpie":"routing-group"'),
    re.compile(r'"magpie":\s*\{\s*"(?:agent|hooks|middleware|gateway|page|icon|'
               r'summary|replaces|permissions|service|tools|skills)"'),
    re.compile(r"\bmagpie_(?:label|by|price|via|cache_unknown|deferred)\b"),
    re.compile(r"\?\.magpie\?"),
]

# In the plugin subsystem the same spelling is the package.json manifest key,
# not a provider id: `"magpie": {...}`, `pkg["magpie"]` and `package.json`
# fixtures there keep upstream's spelling. Outside it, a provider id named
# magpie is owl's and is renamed.
PLUGIN_PATH = re.compile(
    r"(^|/)(internal/(plugin|middleware|library/agent_plugin|"
    r"cli/plugin_|gui/plugin|gui/testdata)|package\.json)"
)
PLUGIN_ONLY = [
    re.compile(r'"magpie":'),
    re.compile(r'\["magpie"\]'),
    re.compile(r"\['magpie'\]"),
]

# Ordered replacements. The module path goes first so that
# `yetone/magpie-releases` becomes `istoo/owl-releases`.
REPLACEMENTS = [
    ("github.com/yetone/magpie", "github.com/istoo/owl"),
    ("yetone/magpie", "istoo/owl"),
    ("MAGPIE", "OWL"),
    ("Magpie", "Owl"),
    ("magpie", "owl"),
]

BINARY_SNIFF = 8192


def transform(text, path=""):
    """Apply the protected patterns and the name replacements to one string."""
    kept = []

    def protect(m):
        kept.append(m.group(0))
        return "\x01%d\x02" % (len(kept) - 1)

    pats = list(PROTECTED)
    if PLUGIN_PATH.search(path):
        pats += PLUGIN_ONLY
    for pat in pats:
        text = pat.sub(protect, text)
    for old, new in REPLACEMENTS:
        text = text.replace(old, new)
    return re.sub(r"\x01([0-9]+)\x02", lambda m: kept[int(m.group(1))], text)


def transform_path(rel):
    parts = []
    for part in rel.split(os.sep):
        parts.append(transform(part))
    return os.path.join(*parts) if parts else rel


def is_binary(data):
    return b"\x00" in data[:BINARY_SNIFF]


def copy_tree(src, dst):
    written = {}
    skipped = 0
    for root, dirs, files in os.walk(src):
        dirs.sort()
        rel_root = os.path.relpath(root, src)
        if rel_root == ".":
            rel_root = ""
        for name in sorted(files):
            spath = os.path.join(root, name)
            if os.path.islink(spath):
                rel = os.path.join(rel_root, name) if rel_root else name
                drel = transform_path(rel)
                dpath = os.path.join(dst, drel)
                if drel in written and written[drel] != rel:
                    raise SystemExit("collision: %s and %s both -> %s" % (written[drel], rel, drel))
                written[drel] = rel
                os.makedirs(os.path.dirname(dpath), exist_ok=True)
                if os.path.lexists(dpath):
                    os.remove(dpath)
                os.symlink(os.readlink(spath), dpath)
                continue
            rel = os.path.join(rel_root, name) if rel_root else name
            drel = transform_path(rel)
            dpath = os.path.join(dst, drel)
            if drel in written and written[drel] != rel:
                raise SystemExit("collision: %s and %s both -> %s" % (written[drel], rel, drel))
            written[drel] = rel
            os.makedirs(os.path.dirname(dpath), exist_ok=True)
            with open(spath, "rb") as f:
                data = f.read()
            if not data:
                out = data
            elif is_binary(data):
                out = data
            else:
                try:
                    text = data.decode("utf-8")
                except UnicodeDecodeError:
                    out = data
                else:
                    out = transform(text, rel).encode("utf-8")
            with open(dpath, "wb") as f:
                f.write(out)
            shutil.copymode(spath, dpath)
    return len(written), skipped


def gofmt(dst):
    """Re-run gofmt: a shorter struct field realigns every field after it."""
    files = []
    for root, _, names in os.walk(dst):
        for name in names:
            if name.endswith(".go"):
                files.append(os.path.join(root, name))
    for i in range(0, len(files), 400):
        subprocess.run(["gofmt", "-w"] + files[i:i + 400], check=True)


def main(argv):
    if len(argv) != 3:
        raise SystemExit("usage: rename.py SRC DST")
    src, dst = argv[1], argv[2]
    if not os.path.isdir(src):
        raise SystemExit("not a directory: %s" % src)
    if os.path.exists(dst):
        raise SystemExit("dst exists: %s" % dst)
    n, _ = copy_tree(src, dst)
    gofmt(dst)
    print("%d files written to %s" % (n, dst))


if __name__ == "__main__":
    main(sys.argv)