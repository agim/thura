#!/usr/bin/env python3
"""Exercise offline recovery from a stopped, synthetic *_test database."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import sys
from urllib.parse import urlparse, urlunparse
import uuid

spec = importlib.util.spec_from_file_location("recovery", Path(__file__).with_name("backup.py"))
recovery = importlib.util.module_from_spec(spec)
spec.loader.exec_module(recovery)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--storage", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--pg-container")
    parser.add_argument("--maintenance-ack", action="store_true", required=True)
    args = parser.parse_args()
    source_url = os.environ.get("DATABASE_URL", "")
    if not urlparse(source_url).path.endswith("_test"):
        raise ValueError("fixture requires a synthetic database ending in _test")
    args.output.mkdir(mode=0o700, parents=False, exist_ok=False)
    fixture_db = "thura_recovery_" + uuid.uuid4().hex
    bundle = args.output / "snapshot"
    restored = args.output / "restored"
    backup_args = argparse.Namespace(storage=args.storage, bundle=bundle,
                                     pg_container=args.pg_container)
    created = False
    try:
        recovery.backup(backup_args)
        tables = recovery.pg(args, ["psql", "-X", "-Atc",
            "SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename"])
        tables = tables.decode().splitlines()
        def literal(value):
            return "'" + value.replace("'", "''") + "'"
        def identifier(value):
            return '"' + value.replace('"', '""') + '"'
        counts_sql = " UNION ALL ".join(
            "SELECT " + literal(name) + " AS name,count(*) AS n FROM " + identifier(name)
            for name in tables)
        def counts():
            return recovery.pg(args, ["psql", "-X", "-Atc", counts_sql])
        original_counts = counts()
        recovery.pg(args, ["createdb", fixture_db])
        created = True
        parsed = urlparse(source_url)
        os.environ["DATABASE_URL"] = urlunparse(parsed._replace(path="/" + fixture_db))
        recovery.restore(argparse.Namespace(storage=restored, bundle=bundle,
                                            pg_container=args.pg_container))
        if counts() != original_counts:
            raise RuntimeError("restored table counts differ")
        versions = recovery.pg(args, ["psql", "-X", "-Atc",
            "SELECT COALESCE(json_agg(json_build_object('key',object_key,'hash',checksum,'size',size)),'[]') FROM file_version"])
        for version in json.loads(versions):
            path = (restored / version["key"]).resolve()
            if restored.resolve() not in path.parents:
                raise RuntimeError("version object escaped storage root")
            if path.stat().st_size != version["size"] or recovery.digest(path) != version["hash"]:
                raise RuntimeError("version bytes do not match restored metadata")
        # A populated destination must never be overwritten.
        try:
            recovery.restore(argparse.Namespace(storage=args.output / "refused",
                                                bundle=bundle, pg_container=args.pg_container))
        except ValueError as error:
            if "populated database" not in str(error):
                raise
        else:
            raise RuntimeError("restore accepted a populated database")
        dump = bundle / "database.dump"
        with dump.open("ab") as target:
            target.write(b"corruption fixture")
        try:
            recovery.restore(argparse.Namespace(storage=args.output / "corrupt-refused",
                                                bundle=bundle, pg_container=args.pg_container))
        except ValueError as error:
            if "checksum" not in str(error):
                raise
        else:
            raise RuntimeError("restore accepted corrupted backup bytes")
        print(f"Recovery fixture passed: {len(tables)} table counts, all object hashes, "
              "version metadata, populated-target refusal and corruption refusal")
    finally:
        os.environ["DATABASE_URL"] = source_url
        if created:
            recovery.pg(args, ["dropdb", fixture_db])
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (ValueError, RuntimeError, OSError) as error:
        print(f"Recovery fixture failed: {error}", file=sys.stderr)
        sys.exit(1)
