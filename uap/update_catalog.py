#!/usr/bin/env python3
"""Download Google's supported-device catalog and generate the embedded index."""

import argparse
import csv
import datetime
import hashlib
import io
import json
from pathlib import Path
import urllib.request


SOURCE = "https://storage.googleapis.com/play_public/supported_devices.csv"
ROOT = Path(__file__).resolve().parent


def atomic_write(path: Path, payload: bytes) -> None:
    temporary = path.with_name(path.name + ".tmp")
    temporary.write_bytes(payload)
    temporary.replace(path)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--date",
        default=datetime.datetime.now(datetime.timezone.utc).date().isoformat(),
        help="catalog snapshot date (defaults to today's UTC date)",
    )
    args = parser.parse_args()
    snapshot_date = datetime.date.fromisoformat(args.date).isoformat()

    request = urllib.request.Request(SOURCE, headers={"User-Agent": "tapmize-deviceua-updater"})
    with urllib.request.urlopen(request, timeout=60) as response:
        raw = response.read()

    text = raw.decode("utf-16" if raw.startswith((b"\xff\xfe", b"\xfe\xff")) else "utf-8-sig")
    reader = csv.DictReader(io.StringIO(text))
    expected = ["Retail Branding", "Marketing Name", "Device", "Model"]
    if reader.fieldnames != expected:
        raise SystemExit(f"Unexpected Google catalog columns: {reader.fieldnames!r}")

    entries = set()
    source_rows = 0
    for row in reader:
        source_rows += 1
        model, brand = row["Model"].strip(), row["Retail Branding"].strip()
        if model and brand:
            entries.add((model, brand))
    if not entries:
        raise SystemExit("Google catalog contains no usable model/brand pairs")

    output = io.StringIO(newline="")
    writer = csv.writer(output, lineterminator="\n")
    writer.writerow(["model", "brand"])
    writer.writerows(sorted(entries, key=lambda item: (item[0].casefold(), item[1].casefold(), item)))
    catalog = output.getvalue().encode("utf-8")
    metadata = {
        "source": SOURCE,
        "source_file": "downloaded by update_catalog.py; not stored separately",
        "listing": "https://storage.googleapis.com/play_public/supported_devices.html",
        "version": snapshot_date,
        "source_sha256": hashlib.sha256(raw).hexdigest(),
        "catalog_sha256": hashlib.sha256(catalog).hexdigest(),
        "source_rows": source_rows,
        "model_brand_pairs": len(entries),
        "generation": "Distinct non-empty Model/Retail Branding pairs; preserve cross-brand collisions; no customer data.",
    }

    atomic_write(ROOT / "catalog.csv", catalog)
    atomic_write(ROOT / "catalog.meta.json", (json.dumps(metadata, indent=2) + "\n").encode())
    print(f"{source_rows:,} Google rows -> {len(entries):,} model/brand pairs; {len(catalog):,} bytes")


if __name__ == "__main__":
    main()
