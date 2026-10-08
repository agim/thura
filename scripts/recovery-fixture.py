#!/usr/bin/env python3
"""Exercise offline recovery from a stopped, synthetic *_test database."""
import argparse
import base64
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
    preview_workspace = str(uuid.uuid4())
    preview_file = str(uuid.uuid4())
    original_key = f"drive/files/{preview_file}/{uuid.uuid4()}"
    preview_key = f"drive/previews/{preview_file}/{uuid.uuid4()}"
    fixture_paths = [args.storage / original_key, args.storage / preview_key]
    seeded = False
    try:
        # Always exercise retained derived metadata, even when browser tests
        # correctly purged their own previews. This source is a stopped *_test
        # database; all fixture rows/objects are removed in finally.
        image = base64.b64decode("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGMQLc3+DwADjQH1XdOL8wAAAABJRU5ErkJggg==")
        for path in fixture_paths:
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(image)
        checksum = recovery.digest(fixture_paths[0])
        recovery.pg(args, ["psql", "-X", "-v", "ON_ERROR_STOP=1", "-c", f"""
            BEGIN;
            INSERT INTO workspace(id,name) VALUES('{preview_workspace}','Offline preview recovery fixture');
            INSERT INTO drive_file(id,workspace_id,name,content_type,size)
              VALUES('{preview_file}','{preview_workspace}','recovery.png','image/png',{len(image)});
            INSERT INTO file_version(file_id,number,object_key,checksum,size,created_by)
              VALUES('{preview_file}',1,'{original_key}','{checksum}',{len(image)},'offline-recovery-fixture');
            INSERT INTO drive_preview(file_id,version,status,object_key,checksum,content_type,width,height)
              VALUES('{preview_file}',1,'ready','{preview_key}','{checksum}','image/png',1,1);
            COMMIT;
        """])
        seeded = True
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
        previews = recovery.pg(args, ["psql", "-X", "-Atc",
            "SELECT COALESCE(json_agg(json_build_object('key',object_key,'hash',checksum)),'[]') FROM drive_preview WHERE status='ready'"])
        preview_rows = json.loads(previews)
        if not preview_rows:
            raise RuntimeError("derived-preview recovery fixture missing")
        for preview in preview_rows:
            path = (restored / preview["key"]).resolve()
            if restored.resolve() not in path.parents or recovery.digest(path) != preview["hash"]:
                raise RuntimeError("preview bytes do not match restored metadata")
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
              "version/preview metadata, populated-target refusal and corruption refusal")
    finally:
        os.environ["DATABASE_URL"] = source_url
        if created:
            recovery.pg(args, ["dropdb", fixture_db])
        if seeded:
            recovery.pg(args, ["psql", "-X", "-v", "ON_ERROR_STOP=1", "-c", f"""
                BEGIN;
                DELETE FROM drive_preview WHERE file_id='{preview_file}';
                DELETE FROM file_version WHERE file_id='{preview_file}';
                DELETE FROM drive_file WHERE id='{preview_file}';
                DELETE FROM workspace WHERE id='{preview_workspace}';
                COMMIT;
            """])
        for path in fixture_paths:
            path.unlink(missing_ok=True)
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (ValueError, RuntimeError, OSError) as error:
        print(f"Recovery fixture failed: {error}", file=sys.stderr)
        sys.exit(1)
