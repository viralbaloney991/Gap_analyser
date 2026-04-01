import requests

STIX_URL = "https://raw.githubusercontent.com/mitre/cti/master/enterprise-attack/enterprise-attack.json"


def fetch_stix(url: str = STIX_URL) -> dict:
    resp = requests.get(url, timeout=30)
    resp.raise_for_status()
    return resp.json()


def parse_techniques(stix: dict) -> list[dict]:
    """Parse MITRE ATT&CK techniques from STIX bundle. Excludes deprecated entries."""
    techniques = []
    for obj in stix.get("objects", []):
        if obj.get("type") != "attack-pattern":
            continue
        if obj.get("x_mitre_deprecated", False):
            continue

        technique_id = None
        for ref in obj.get("external_references", []):
            if ref.get("source_name") == "mitre-attack":
                technique_id = ref.get("external_id")
                break
        if not technique_id:
            continue

        techniques.append({
            "id": technique_id,
            "name": obj.get("name", ""),
            "description": obj.get("description", ""),
            "detection": obj.get("x_mitre_detection", ""),
        })

    return techniques
