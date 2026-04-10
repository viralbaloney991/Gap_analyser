package similarity

import "strings"

// pivotCategoryMap maps lowercase group_by field paths to semantic pivot categories.
// Unknown paths are used as their own category (exact-path matching still works).
var pivotCategoryMap = map[string]string{
	// user identity
	"useridentity.arn":                                   "user",
	"useridentity.principalid":                           "user",
	"useridentity.sessioncontext.sessionissuer.username": "user",
	"actor.email":                                        "user",
	"actor.user.email":                                   "user",
	"cloudflare.actor.email":                             "user",
	"okta.actor.alternateid":                             "user",
	"cx_security.email":                                  "user",
	"cx_security.username":                               "user",
	"user.username":                                      "user",
	"actor_id":                                           "user",
	"actor":                                              "user",
	"user_name":                                          "user",
	"requestparameters.username":                         "user",
	"event.userid":                                       "user",
	"event.parameters.user_email":                        "user",
	"username":                                           "user",
	"arn_extracted":                                      "user",
	// source IP
	"clientip":                 "ip",
	"cx_security.source_ip":    "ip",
	"okta.client.ipaddress":    "ip",
	"remote_ip":                "ip",
	"msg.extension.publicipv4": "ip",
	// hostname / endpoint
	"event.hostname":     "hostname",
	"event.computername": "hostname",
	// cloud / infrastructure resource
	"requestparameters.bucketname":  "resource",
	"requestparameters.instanceid":  "resource",
	"requestparameters.domainname":  "resource",
	"requestparameters.rolearn":     "resource",
	"instance-id":                   "resource",
	// account / tenant
	"useridentity.accountid":             "account",
	"coralogix.metadata.applicationname": "account",
	"awsregion":                          "account",
	// detection / rule identifier
	"event.detectid":    "detection",
	"event.compositeid": "detection",
	"event.aggregateid": "detection",
	"sourcerule.name":   "detection",
	"detail.id":         "detection",
}

// normalizeGroupByKeys maps a slice of group_by field paths to a set of semantic
// pivot categories. Keys are lowercased before lookup. Unknown paths are kept
// verbatim as their own category so exact-path matches still count.
// Returns nil for empty/nil input.
func normalizeGroupByKeys(keys []string) map[string]struct{} {
	if len(keys) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		lower := strings.ToLower(strings.TrimSpace(k))
		if lower == "" {
			continue
		}
		if cat, ok := pivotCategoryMap[lower]; ok {
			out[cat] = struct{}{}
		} else {
			out[lower] = struct{}{}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// jaccardGroupBy computes Jaccard similarity for pivot category sets.
// Unlike jaccard(), two empty sets return 1.0 — both unspecified means compatible.
func jaccardGroupBy(a, b map[string]struct{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	return jaccard(a, b)
}
