#!/usr/bin/env python3
"""Refresh pricing metadata in the shared model catalog.

This script updates the pricing fields in lib/sop/model_catalog.json from a
curated snapshot of provider-maintained values. It is designed to be run in CI
on a schedule and can also be run locally to produce a deterministic refresh.
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parent.parent
CATALOG_PATH = ROOT / "lib" / "sop" / "model_catalog.json"

PRICING_SNAPSHOT: dict[str, dict[str, dict[str, Any]]] = {
    "google": {
        "gemini-2.5-pro": {"input": 1.25, "output": 10.0, "status": "official", "source": "google-gemini-pricing"},
        "gemini-2.5-flash": {"input": 0.30, "output": 2.50, "status": "official", "source": "google-gemini-pricing"},
        "gemini-2.5-flash-lite": {"input": 0.10, "output": 0.40, "status": "official", "source": "google-gemini-pricing"},
        "gemini-2.0-flash": {"input": 0.10, "output": 0.40, "status": "official", "source": "google-gemini-pricing"},
        "gemini-2.0-flash-lite": {"input": 0.075, "output": 0.30, "status": "official", "source": "google-gemini-pricing"},
    },
    "anthropic": {
        "claude-opus-4-8": {"input": 5.0, "output": 25.0, "status": "official", "source": "anthropic-pricing"},
        "claude-opus-4-6": {"input": 5.0, "output": 25.0, "status": "official", "source": "anthropic-pricing"},
        "claude-sonnet-4-6": {"input": 3.0, "output": 15.0, "status": "official", "source": "anthropic-pricing"},
        "claude-sonnet-4-5": {"input": 3.0, "output": 15.0, "status": "official", "source": "anthropic-pricing"},
        "claude-3-7-sonnet": {"input": 3.0, "output": 15.0, "status": "official", "source": "anthropic-pricing"},
        "claude-3-5-sonnet": {"input": 3.0, "output": 15.0, "status": "official", "source": "anthropic-pricing"},
        "claude-3-5-haiku": {"input": 0.8, "output": 4.0, "status": "official", "source": "anthropic-pricing"},
        "claude-3-haiku": {"input": 0.25, "output": 1.25, "status": "last_known", "source": "anthropic-pricing"},
    },
    "openai": {
        "gpt-5.5": {"input": 5.0, "output": 30.0, "status": "official", "source": "openai-pricing"},
        "gpt-5.4": {"input": 2.5, "output": 15.0, "status": "official", "source": "openai-pricing"},
        "gpt-5.4-mini": {"input": 0.75, "output": 4.50, "status": "official", "source": "openai-pricing"},
        "gpt-4.1": {"input": 2.0, "output": 8.0, "status": "official", "source": "openai-pricing"},
        "gpt-4.1-mini": {"input": 0.4, "output": 1.6, "status": "official", "source": "openai-pricing"},
        "gpt-4.1-nano": {"input": 0.10, "output": 0.40, "status": "official", "source": "openai-pricing"},
        "gpt-4o": {"input": 2.5, "output": 10.0, "status": "official", "source": "openai-pricing"},
        "gpt-4o-mini": {"input": 0.15, "output": 0.60, "status": "official", "source": "openai-pricing"},
        "gpt-4o-audio-preview": {"input": 2.5, "output": 10.0, "status": "last_known", "source": "openai-pricing"},
        "gpt-5.4-nano": {"input": 0.20, "output": 1.25, "status": "official", "source": "openai-pricing"},
    },
}


def _provider_from_value(value: str) -> str | None:
    if not value:
        return None
    prefix, _, rest = value.partition(":")
    if not rest:
        return None
    lowered = prefix.lower()
    if lowered == "gemini":
        return "google"
    if lowered == "anthropic":
        return "anthropic"
    if lowered == "openai":
        return "openai"
    return None


def _model_from_value(value: str) -> str | None:
    if not value:
        return None
    _, _, rest = value.partition(":")
    if not rest:
        return None
    return rest.strip()


def refresh_catalog_pricing(catalog: dict[str, Any]) -> tuple[int, int]:
    updated_entries = 0
    scanned_entries = 0
    for group in catalog.get("llm", []):
        for option in group.get("options", []):
            scanned_entries += 1
            provider = _provider_from_value(option.get("value", ""))
            model = _model_from_value(option.get("value", ""))
            if provider is None or model is None:
                continue
            pricing = PRICING_SNAPSHOT.get(provider, {}).get(model)
            if pricing is None:
                continue
            changed = False
            if option.get("input_cost_per_1m_tokens") != pricing["input"]:
                option["input_cost_per_1m_tokens"] = pricing["input"]
                changed = True
            if option.get("output_cost_per_1m_tokens") != pricing["output"]:
                option["output_cost_per_1m_tokens"] = pricing["output"]
                changed = True
            if option.get("pricing_status") != pricing.get("status", "official"):
                option["pricing_status"] = pricing.get("status", "official")
                changed = True
            if option.get("pricing_source") != pricing.get("source", "provider-pricing"):
                option["pricing_source"] = pricing.get("source", "provider-pricing")
                changed = True
            if option.get("last_known_input_cost_usd") != pricing["input"]:
                option["last_known_input_cost_usd"] = pricing["input"]
                changed = True
            if option.get("last_known_output_cost_usd") != pricing["output"]:
                option["last_known_output_cost_usd"] = pricing["output"]
                changed = True
            if changed:
                updated_entries += 1
    return scanned_entries, updated_entries


def write_catalog(catalog_path: Path, catalog: dict[str, Any]) -> None:
    catalog_path.write_text(json.dumps(catalog, indent=4) + "\n", encoding="utf-8")


def main() -> int:
    parser = argparse.ArgumentParser(description="Refresh pricing metadata in the shared model catalog")
    parser.add_argument("--catalog", type=Path, default=CATALOG_PATH, help="Path to the model_catalog.json file")
    parser.add_argument("--write", action="store_true", help="Write the refreshed catalog back to disk")
    args = parser.parse_args()

    catalog_path = args.catalog.resolve()
    with catalog_path.open("r", encoding="utf-8") as handle:
        catalog = json.load(handle)

    scanned_entries, updated_entries = refresh_catalog_pricing(catalog)
    if args.write:
        write_catalog(catalog_path, catalog)

    print(f"scanned_entries={scanned_entries}")
    print(f"updated_entries={updated_entries}")
    if not args.write:
        print("dry_run=1")
    return 0


if __name__ == "__main__":
    sys.exit(main())
