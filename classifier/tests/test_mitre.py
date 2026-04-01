import json
import pytest
from unittest.mock import patch
from classifier.mitre import parse_techniques, fetch_stix


SAMPLE_STIX = {
    "objects": [
        {
            "type": "attack-pattern",
            "id": "attack-pattern--0a3ead4e-6d47-4ccb-854c-a6a4f9d96b22",
            "name": "OS Credential Dumping",
            "description": "Adversaries may attempt to dump credentials to obtain account login information.",
            "x_mitre_deprecated": False,
            "x_mitre_detection": "Monitor for unexpected processes interacting with lsass.exe",
            "external_references": [
                {"source_name": "mitre-attack", "external_id": "T1003"}
            ]
        },
        {
            "type": "attack-pattern",
            "id": "attack-pattern--deprecated",
            "name": "Old Technique",
            "description": "Deprecated.",
            "x_mitre_deprecated": True,
            "external_references": [
                {"source_name": "mitre-attack", "external_id": "T9999"}
            ]
        },
        {
            "type": "course-of-action",
            "name": "Not a technique",
        }
    ]
}


def test_parse_techniques_filters_deprecated():
    techniques = parse_techniques(SAMPLE_STIX)
    ids = [t["id"] for t in techniques]
    assert "T1003" in ids
    assert "T9999" not in ids


def test_parse_techniques_excludes_non_attack_pattern():
    techniques = parse_techniques(SAMPLE_STIX)
    assert len(techniques) == 1


def test_parse_techniques_fields():
    techniques = parse_techniques(SAMPLE_STIX)
    t = techniques[0]
    assert t["id"] == "T1003"
    assert t["name"] == "OS Credential Dumping"
    assert "dump credentials" in t["description"]
    assert "lsass" in t["detection"]
