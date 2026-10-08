#!/usr/bin/env python3
"""Offline Postgres + local-storage recovery. DATABASE_URL stays off argv."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
from urllib.parse import parse_qs, unquote, urlparse


def digest(path):
    h = hashlib.sha256()
    with path.open("rb") as source:
        for block in iter(lambda: source.read(1024 * 1024), b""):
            h.update(block)
    return h.hexdigest()


def inventory(root):
    result = {}
    for path in sorted(root.rglob("*")):
        if path.is_symlink():
            raise ValueError("symlinks are not supported")
        if path.is_file():
            result[path.relative_to(root).as_posix()] = {
                "size": path.stat().st_size, "sha256": digest(path)}
        elif not path.is_dir():
            raise ValueError("special files are not supported")
    return result


def pg_environment():
    url = urlparse(os.environ.get("DATABASE_URL", ""))
    if url.scheme not in ("postgres", "postgresql") or not url.path.strip("/"):
        raise ValueError("export DATABASE_URL privately before running")
    result = os.environ.copy()
    # Explicit connection values prevent accidental fallback to another database.
    for key, value in {"PGHOST": url.hostname or "localhost", "PGPORT": str(url.port or 5432),
                       "PGUSER": unquote(url.username or "postgres"),
                       "PGPASSWORD": unquote(url.password or ""),
                       "PGDATABASE": unquote(url.path[1:])}.items():
        result[key] = value
    options = parse_qs(url.query)
    for name in ("sslmode", "sslrootcert", "sslcert", "sslkey"):
        if name in options:
            result["PG" + name.upper()] = options[name][0]
    return result


def pg(args, command, *, stdin=None, stdout=subprocess.PIPE):
    environment = pg_environment()
    prefix = []
    if args.pg_container:
        prefix = ["docker", "exec", "-i"]
        for key in ("PGHOST", "PGPORT", "PGUSER", "PGPASSWORD", "PGDATABASE",
                    "PGSSLMODE", "PGSSLROOTCERT", "PGSSLCERT", "PGSSLKEY"):
            if key in environment:
                prefix += ["--env", key]
        prefix += [args.pg_container]
    result = subprocess.run(prefix + command, env=environment, stdin=stdin,
                            stdout=stdout, stderr=subprocess.PIPE, check=False)
    if result.returncode:
        # Provider diagnostics can contain connection details. Do not echo them.
        raise RuntimeError("Postgres command failed; inspect connection and permissions privately")
    return result.stdout


def backup(args):
    source = args.storage.resolve()
    target = args.bundle.resolve()
    if not source.is_dir() or args.storage.is_symlink():
        raise ValueError("storage must be an existing directory without symlinks")
    if target == source or source in target.parents or target in source.parents:
        raise ValueError("backup and storage directories must be separate")
    inventory(source)  # Refuse symlinks before copying.
    target.mkdir(mode=0o700, parents=False, exist_ok=False)
    with (target / "database.dump").open("xb") as dump:
        pg(args, ["pg_dump", "--format=custom", "--no-owner", "--no-acl"], stdout=dump)
    shutil.copytree(source, target / "objects")
    commit = subprocess.run(["git", "rev-parse", "HEAD"], capture_output=True, text=True)
    manifest = {"version": 1, "commit": commit.stdout.strip() if commit.returncode == 0 else None,
                "files": inventory(target)}
    (target / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    print(f"Backup complete: {len(manifest['files'])} checksummed files")


def restore(args):
    source = args.bundle.resolve()
    target = args.storage.resolve()
    if args.bundle.is_symlink() or args.storage.is_symlink():
        raise ValueError("symlinks are not supported")
    manifest = json.loads((source / "manifest.json").read_text())
    actual = inventory(source)
    actual.pop("manifest.json", None)
    if manifest.get("version") != 1 or actual != manifest.get("files"):
        raise ValueError("backup checksum verification failed")
    if not (source / "database.dump").is_file() or not (source / "objects").is_dir():
        raise ValueError("incomplete backup")
    if target == source or source in target.parents or target in source.parents:
        raise ValueError("backup and storage directories must be separate")
    if target.exists() and (not target.is_dir() or any(target.iterdir())):
        raise ValueError("restore requires empty storage")
    # Include every non-system schema, including extension-managed tables.
    tables = pg(args, ["psql", "-X", "-v", "ON_ERROR_STOP=1", "-Atc",
                      "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace "
                      "WHERE c.relkind IN ('r','p','v','m','S','f') "
                      "AND n.nspname NOT LIKE 'pg_%' AND n.nspname<>'information_schema'"])
    if int(tables.strip()) != 0:
        raise ValueError("restore refuses a populated database; create a clean database first")
    with (source / "database.dump").open("rb") as dump:
        pg(args, ["pg_restore", "--exit-on-error", "--single-transaction", "--no-owner",
                  "--no-acl", "--dbname", pg_environment()["PGDATABASE"]], stdin=dump)
    target.mkdir(mode=0o700, parents=True, exist_ok=True)
    shutil.copytree(source / "objects", target, dirs_exist_ok=True)
    if inventory(target) != inventory(source / "objects"):
        raise RuntimeError("restored object checksum verification failed; keep application stopped")
    print("Restore complete: database restored and every object checksum verified")


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("backup", "restore"))
    parser.add_argument("--bundle", type=Path, required=True)
    parser.add_argument("--storage", type=Path, required=True)
    parser.add_argument("--pg-container", help="optional container with Postgres client tools")
    parser.add_argument("--maintenance-ack", action="store_true", required=True,
                        help="operator confirms all app writers/jobs/ingress are stopped")
    args = parser.parse_args()
    try:
        (backup if args.action == "backup" else restore)(args)
    except (OSError, ValueError, RuntimeError, KeyError) as error:
        print(f"Recovery failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
