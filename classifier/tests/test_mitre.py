import json
import pytest
from unittest.mock import patch, MagicMock
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


def test_fetch_stix_returns_dict():
    mock_resp = MagicMock()
    mock_resp.json.return_value = {"objects": []}
    mock_resp.raise_for_status.return_value = None
    with patch("requests.get", return_value=mock_resp) as mock_get:
        result = fetch_stix("http://example.com/stix.json")
        mock_get.assert_called_once_with("http://example.com/stix.json", timeout=30)
        assert result == {"objects": []}


def test_fetch_stix_raises_on_network_error():
    import requests as req
    with patch("requests.get", side_effect=req.exceptions.ConnectionError("timeout")):
        with pytest.raises(RuntimeError, match="Failed to fetch MITRE STIX"):
            fetch_stix("http://example.com/stix.json")
