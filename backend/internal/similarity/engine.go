package similarity

import (
	"fmt"
	"math"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"

	"coralogix-alert-analyzer/internal/coralogix"
	"coralogix-alert-analyzer/internal/models"
)

// featureVector holds the tokenized, lowercase feature sets for a single alert.
type featureVector struct {
	alertID           string
	alertName         string
	alertType         string
	dataSources       map[string]struct{}
	entities          map[string]struct{}
	actions           map[string]struct{}
	conditions        map[string]struct{}
	techniques        map[string]struct{}
	groupByCategories map[string]struct{}
	luceneQuery       map[string]struct{} // tokenised Lucene query — actual detection logic
	timeWindow        string              // from AlertFeatures.TimeWindow
	tactics           []string            // from AlertFeatures.Tactics — used by deriveFamilyName
}

// pairScore stores the similarity score between two alerts.
type pairScore struct {
	i, j  int
	score float64
}

// idfTable holds per-dimension inverse-document-frequency weights for the corpus.
// idf(t) = log(1 + N/df(t)) where N = number of alerts and df(t) = number of
// alerts that contain token t in the given dimension.
type idfTable struct {
	dataSources map[string]float64
	entities    map[string]float64
	actions     map[string]float64
	conditions  map[string]float64
	techniques  map[string]float64
	groupBy     map[string]float64
	luceneQuery map[string]float64
}

// buildIDF computes an idfTable from the full set of feature vectors.
// Each dimension is scored independently. Runs in O(n × avg_tokens_per_alert).
func buildIDF(vectors []featureVector) idfTable {
	n := float64(len(vectors))
	if n == 0 {
		return idfTable{}
	}

	tbl := idfTable{
		dataSources: make(map[string]float64),
		entities:    make(map[string]float64),
		actions:     make(map[string]float64),
		conditions:  make(map[string]float64),
		techniques:  make(map[string]float64),
		groupBy:     make(map[string]float64),
		luceneQuery: make(map[string]float64),
	}

	type dimDef struct {
		get func(featureVector) map[string]struct{}
		out map[string]float64
	}
	dims := []dimDef{
		{func(v featureVector) map[string]struct{} { return v.dataSources }, tbl.dataSources},
		{func(v featureVector) map[string]struct{} { return v.entities }, tbl.entities},
		{func(v featureVector) map[string]struct{} { return v.actions }, tbl.actions},
		{func(v featureVector) map[string]struct{} { return v.conditions }, tbl.conditions},
		{func(v featureVector) map[string]struct{} { return v.techniques }, tbl.techniques},
		{func(v featureVector) map[string]struct{} { return v.groupByCategories }, tbl.groupBy},
		{func(v featureVector) map[string]struct{} { return v.luceneQuery }, tbl.luceneQuery},
	}

	for _, dim := range dims {
		df := make(map[string]int)
		for _, v := range vectors {
			for t := range dim.get(v) {
				df[t]++
			}
		}
		for t, count := range df {
			dim.out[t] = math.Log(1 + n/float64(count))
		}
	}

	return tbl
}

// Similarity weights for each feature dimension.
const (
	// Weights sum to exactly 1.00.
	// 9-dimension model: Lucene query and TimeWindow added in 2026-04.
	weightDataSources  = 0.15
	weightEntities     = 0.10
	weightActions      = 0.15
	weightConditions   = 0.10 // reduced: less signal than Lucene query
	weightTechniques   = 0.10
	weightGroupBy      = 0.15 // reduced: was over-dominant
	weightAlertType    = 0.05 // reduced: binary, low signal
	weightLuceneQuery  = 0.15 // new: actual detection logic
	weightTimeWindow   = 0.05 // new: binary equality bonus

	duplicateThreshold = 0.85
	familyThreshold    = 0.60
	mergeAvgThreshold  = 0.70
	uniqueThreshold    = 0.30

	// When the number of alerts exceeds this value, pairwise comparison is
	// parallelised across a worker pool.
	parallelThreshold = 50

	// Minimum tactic coverage % to avoid a thin-coverage insight.
	minTacticCoveragePct = 25.0
)

// Package-level regex variables for tokenizeLucene.
// These are compiled once at startup instead of on every tokenizeLucene call.
var (
	luceneAtomRe  = regexp.MustCompile(`[\w.]+:(?:"[^"]*"|[^\s()\[\]{}+\-!"]+)`)
	luceneNormRe  = regexp.MustCompile(`["\s]+`)
	luceneSplitRe = regexp.MustCompile(`[:()\[\]{}\s+\-!"]+`)
)

// tacticLabels maps MITRE ATT&CK tactic slugs to human-readable names.
var tacticLabels = map[string]string{
	"initial-access":       "Initial Access",
	"execution":            "Execution",
	"persistence":          "Persistence",
	"privilege-escalation": "Privilege Escalation",
	"defense-evasion":      "Defense Evasion",
	"credential-access":    "Credential Access",
	"discovery":            "Discovery",
	"lateral-movement":     "Lateral Movement",
	"collection":           "Collection",
	"exfiltration":         "Exfiltration",
	"command-and-control":  "Command & Control",
	"impact":               "Impact",
	"reconnaissance":       "Reconnaissance",
	"resource-development": "Resource Development",
}

// actionCategories maps action keyword prefixes to security category labels.
// Order matters: first match wins.
var actionCategories = []struct {
	keywords []string
	category string
}{
	{[]string{"remove", "delete", "revoke", "wipe"}, "Tampering"},
	{[]string{"login", "authenticate", "signin", "logon"}, "Authentication"},
	{[]string{"escalat", "grant", "privilege", "sudo"}, "Privilege Escalation"},
	{[]string{"exfiltrat", "download", "upload", "transfer"}, "Exfiltration"},
	{[]string{"scan", "enumerat", "discover", "recon"}, "Discovery"},
	{[]string{"execute", "run", "inject", "spawn"}, "Execution"},
	{[]string{"persist", "install", "schedule", "startup"}, "Persistence"},
	{[]string{"encrypt", "ransom", "destroy"}, "Impact"},
}

// Analyze performs full similarity analysis on a set of alert definitions.
// eventCounts maps alertID → 30-day trigger count; pass nil to skip behavioral noise detection.
// integrationCount is the total number of integrations in the org.
// mitreResult provides MITRE tactic coverage for gap detection; pass nil to skip coverage insights.
func Analyze(
	alerts []*models.AlertDef,
	eventCounts map[string]int,
	integrationCount int,
	mitreResult *models.MITRECoverageResult,
) *models.SimilarityResult {
	if len(alerts) == 0 {
		return &models.SimilarityResult{}
	}

	// Step 1: Build feature vectors.
	vectors := buildFeatureVectors(alerts)

	// Step 1b: Build IDF table for the corpus — used by scorePair to weight
	// rare tokens more heavily than common ones.
	idf := buildIDF(vectors)

	// Step 2: Compute pairwise similarity scores.
	n := len(vectors)
	scores := computePairwiseScores(vectors, n, idf)

	// Build a quick-lookup matrix (symmetric).
	matrix := make([][]float64, n)
	for i := range matrix {
		matrix[i] = make([]float64, n)
	}
	for _, ps := range scores {
		matrix[ps.i][ps.j] = ps.score
		matrix[ps.j][ps.i] = ps.score
	}

	// Step 3: Group into families.
	families := groupFamilies(vectors, matrix, n)

	// Step 4: Identify duplicates.
	duplicates := identifyDuplicates(vectors, matrix, n)

	// Step 5: Merge suggestions.
	mergeSuggestions := buildMergeSuggestions(vectors, matrix, n)

	// Step 6: Coverage insights (MITRE-based; nil = no coverage insights).
	coverageInsights := analyzeCoverage(mitreResult)

	// Step 7: Unique detections.
	uniqueDetections := findUniqueDetections(vectors, matrix, n)

	// Step 8: Noise detection.
	noiseAlerts := findNoiseAlerts(vectors, alerts, eventCounts, integrationCount)

	return &models.SimilarityResult{
		Families:         families,
		Duplicates:       duplicates,
		MergeSuggestions: mergeSuggestions,
		CoverageInsights: coverageInsights,
		UniqueDetections: uniqueDetections,
		NoiseAlerts:      noiseAlerts,
	}
}

// tokenizeLucene splits a Lucene query string into a lowercase token set.
//
// Two-pass approach:
//  1. Extract field:value pairs as atomic tokens (e.g. record_type:"AAAA" → record_type:aaaa).
//     This preserves discriminating field values that would otherwise be split or dropped.
//  2. Tokenise the remainder (query with atoms stripped) on Lucene operators and whitespace.
//     Single-character standalone tokens are dropped as noise.
func tokenizeLucene(q string) map[string]struct{} {
	if q == "" {
		return nil
	}
	lower := strings.ToLower(q)
	s := make(map[string]struct{})

	// Pass 1: extract field:value atoms.
	// Matches word chars (including dots) followed by : and a value.
	// Value can be quoted ("...") or unquoted (non-whitespace, non-operator chars).
	for _, atom := range luceneAtomRe.FindAllString(lower, -1) {
		norm := luceneNormRe.ReplaceAllString(atom, "")
		if len(norm) > 2 { // skip trivially short atoms (≤2 chars total)
			s[norm] = struct{}{}
		}
	}

	// Pass 2: strip atoms from query, tokenise remainder.
	remainder := luceneAtomRe.ReplaceAllString(lower, " ")
	for _, t := range luceneSplitRe.Split(remainder, -1) {
		t = strings.TrimSpace(t)
		if len(t) > 1 {
			s[t] = struct{}{}
		}
	}

	if len(s) == 0 {
		return nil
	}
	return s
}

// ---------------------------------------------------------------------------
// Step 1: Feature Vector Creation
// ---------------------------------------------------------------------------

// buildFeatureVectors converts each alert's Features into a normalised set of
// lowercase tokens suitable for Jaccard comparison.
func buildFeatureVectors(alerts []*models.AlertDef) []featureVector {
	vectors := make([]featureVector, len(alerts))
	for i, a := range alerts {
		vectors[i] = featureVector{
			alertID:           a.ID,
			alertName:         a.Name,
			alertType:         strings.ToLower(a.AlertType),
			dataSources:       toSet(a.Features.DataSources),
			entities:          toSet(a.Features.Entities),
			actions:           toSet(a.Features.Actions),
			conditions:        toSet(a.Features.Conditions),
			techniques:        toSet(a.Features.Techniques),
			groupByCategories: normalizeGroupByKeys(a.GroupByKeys),
			luceneQuery:       tokenizeLucene(coralogix.ExtractLuceneQuery(a.TypeDef)),
			timeWindow:        a.Features.TimeWindow,
			tactics:           a.Features.Tactics,
		}
	}
	return vectors
}

// toSet converts a string slice into a set of lowercase tokens.
func toSet(items []string) map[string]struct{} {
	s := make(map[string]struct{}, len(items))
	for _, item := range items {
		tok := strings.ToLower(strings.TrimSpace(item))
		if tok != "" {
			s[tok] = struct{}{}
		}
	}
	return s
}

// ---------------------------------------------------------------------------
// Step 2: Pairwise Similarity
// ---------------------------------------------------------------------------

// computePairwiseScores calculates the weighted Jaccard similarity for every
// unique pair (i < j). When the alert count exceeds parallelThreshold, the
// work is distributed across a goroutine worker pool.
func computePairwiseScores(vectors []featureVector, n int, idf idfTable) []pairScore {
	totalPairs := n * (n - 1) / 2
	if totalPairs == 0 {
		return nil
	}

	results := make([]pairScore, totalPairs)

	if n <= parallelThreshold {
		// Sequential path for small sets.
		idx := 0
		for i := 0; i < n; i++ {
			for j := i + 1; j < n; j++ {
				results[idx] = pairScore{i: i, j: j, score: scorePair(vectors[i], vectors[j], idf)}
				idx++
			}
		}
		return results
	}

	// Parallel path: worker pool.
	type pairInput struct {
		i, j    int
		destIdx int
	}

	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}

	ch := make(chan pairInput, workers*4)
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range ch {
				results[p.destIdx] = pairScore{
					i:     p.i,
					j:     p.j,
					score: scorePair(vectors[p.i], vectors[p.j], idf),
				}
			}
		}()
	}

	idx := 0
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			ch <- pairInput{i: i, j: j, destIdx: idx}
			idx++
		}
	}
	close(ch)
	wg.Wait()

	return results
}

// scorePair computes the weighted IDF-Jaccard similarity between two feature vectors.
// The idf table scales each token's contribution by its corpus-wide rarity — rare
// tokens discriminate more than common ones. Pass idfTable{} to get flat-Jaccard
// behaviour (all weights default to 1.0 via idfWeight).
func scorePair(a, b featureVector, idf idfTable) float64 {
	score := 0.0
	score += weightDataSources * weightedJaccard(a.dataSources, b.dataSources, idf.dataSources)
	score += weightEntities * weightedJaccard(a.entities, b.entities, idf.entities)
	score += weightActions * weightedJaccard(a.actions, b.actions, idf.actions)
	score += weightConditions * weightedJaccard(a.conditions, b.conditions, idf.conditions)
	score += weightTechniques * weightedJaccard(a.techniques, b.techniques, idf.techniques)
	score += weightGroupBy * weightedJaccard(a.groupByCategories, b.groupByCategories, idf.groupBy)
	score += weightLuceneQuery * weightedJaccard(a.luceneQuery, b.luceneQuery, idf.luceneQuery)

	if a.alertType == b.alertType && a.alertType != "" {
		score += weightAlertType
	}
	if a.timeWindow == b.timeWindow && a.timeWindow != "" {
		score += weightTimeWindow
	}

	return score
}

// jaccard computes |A n B| / |A u B|. Returns 0 when both sets are empty.
func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}

	inter := 0
	for k := range a {
		if _, ok := b[k]; ok {
			inter++
		}
	}

	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}

	return float64(inter) / float64(union)
}

// weightedJaccard computes an IDF-weighted Jaccard (Tanimoto) coefficient.
// Each token's contribution to intersection and union is scaled by its IDF weight,
// so rare tokens matter more than common ones.
// Returns 0 when both sets are empty (consistent with jaccard).
func weightedJaccard(a, b map[string]struct{}, idf map[string]float64) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0.0
	}
	var intersection, union float64
	for t := range a {
		w := idfWeight(t, idf)
		if _, inB := b[t]; inB {
			intersection += w
		}
		union += w
	}
	for t := range b {
		if _, inA := a[t]; !inA {
			union += idfWeight(t, idf)
		}
	}
	if union == 0 {
		return 0.0
	}
	return intersection / union
}

// idfWeight returns the IDF weight for token t from the given table.
// Falls back to 1.0 (neutral weight) for tokens not present in the table,
// so scorePair degrades gracefully to flat Jaccard when called with an empty idfTable.
func idfWeight(t string, idf map[string]float64) float64 {
	if idf != nil {
		if w, ok := idf[t]; ok {
			return w
		}
	}
	return 1.0
}

// ---------------------------------------------------------------------------
// Step 3: Family Grouping (single-linkage clustering)
// ---------------------------------------------------------------------------

// groupFamilies performs single-linkage agglomerative clustering at the given
// threshold and names each cluster by the most frequent technique or action.
func groupFamilies(vectors []featureVector, matrix [][]float64, n int) []models.DetectionFamily {
	// Union-Find for single-linkage clustering.
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}

	var find func(int) int
	find = func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]] // path compression
			x = parent[x]
		}
		return x
	}

	union := func(x, y int) {
		rx, ry := find(x), find(y)
		if rx != ry {
			parent[rx] = ry
		}
	}

	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if matrix[i][j] >= familyThreshold {
				union(i, j)
			}
		}
	}

	// Collect clusters.
	clusters := make(map[int][]int)
	for i := 0; i < n; i++ {
		root := find(i)
		clusters[root] = append(clusters[root], i)
	}

	// Only keep families with 2+ members.
	families := make([]models.DetectionFamily, 0)
	familyNum := 1
	for _, members := range clusters {
		if len(members) < 2 {
			continue
		}

		ids := make([]string, len(members))
		names := make([]string, len(members))
		for k, idx := range members {
			ids[k] = vectors[idx].alertID
			names[k] = vectors[idx].alertName
		}

		familyName := deriveFamilyName(vectors, members, familyNum)
		families = append(families, models.DetectionFamily{
			Name:       familyName,
			AlertIDs:   ids,
			AlertNames: names,
		})
		familyNum++
	}

	// Sort families by size descending for deterministic output.
	sort.Slice(families, func(i, j int) bool {
		return len(families[i].AlertIDs) > len(families[j].AlertIDs)
	})

	return families
}

// deriveFamilyName builds a human-readable family name using a 3-tier strategy:
// Tier 1: most frequent MITRE tactic → human label
// Tier 2: action tokens matched against semantic category map
// Tier 3: most frequent technique or action token (original behaviour)
func deriveFamilyName(vectors []featureVector, members []int, fallbackNum int) string {
	// Tier 1: most frequent MITRE tactic across cluster members.
	tacticFreq := make(map[string]int)
	for _, idx := range members {
		for _, tac := range vectors[idx].tactics {
			tacticFreq[strings.ToLower(tac)]++
		}
	}
	if len(tacticFreq) > 0 {
		bestTactic, bestCount := "", 0
		for tac, count := range tacticFreq {
			if count > bestCount || (count == bestCount && tac < bestTactic) {
				bestTactic = tac
				bestCount = count
			}
		}
		if label, ok := tacticLabels[bestTactic]; ok {
			return label + " Detections"
		}
	}

	// Tier 2: action tokens matched against semantic category map.
	for _, idx := range members {
		for action := range vectors[idx].actions {
			lower := strings.ToLower(action)
			for _, entry := range actionCategories {
				for _, kw := range entry.keywords {
					if strings.Contains(lower, kw) {
						return entry.category + " Detections"
					}
				}
			}
		}
	}

	// Tier 3: most frequent technique or action token (original behaviour).
	freq := make(map[string]int)
	for _, idx := range members {
		for t := range vectors[idx].techniques {
			freq[t]++
		}
	}
	if len(freq) == 0 {
		for _, idx := range members {
			for a := range vectors[idx].actions {
				freq[a]++
			}
		}
	}
	if len(freq) == 0 {
		return fmt.Sprintf("Detection Family %d", fallbackNum)
	}
	bestToken := ""
	bestCount := 0
	for tok, count := range freq {
		if count > bestCount || (count == bestCount && tok < bestToken) {
			bestToken = tok
			bestCount = count
		}
	}
	return toTitle(bestToken) + " Detections"
}

// toTitle converts a hyphenated or snake_cased string to title case.
func toTitle(s string) string {
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, "-", " ")
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// ---------------------------------------------------------------------------
// Step 4: Duplicate Detection
// ---------------------------------------------------------------------------

// identifyDuplicates finds all pairs with similarity >= duplicateThreshold and
// generates an explanatory description for each.
func identifyDuplicates(vectors []featureVector, matrix [][]float64, n int) []models.DuplicateGroup {
	var duplicates []models.DuplicateGroup

	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if matrix[i][j] < duplicateThreshold {
				continue
			}

			explanation := buildDuplicateExplanation(vectors[i], vectors[j])
			duplicates = append(duplicates, models.DuplicateGroup{
				AlertIDs:    []string{vectors[i].alertID, vectors[j].alertID},
				AlertNames:  []string{vectors[i].alertName, vectors[j].alertName},
				Similarity:  math.Round(matrix[i][j]*1000) / 1000, // 3 decimal places
				Explanation: explanation,
			})
		}
	}

	// Sort by similarity descending.
	sort.Slice(duplicates, func(i, j int) bool {
		return duplicates[i].Similarity > duplicates[j].Similarity
	})

	return duplicates
}

// buildDuplicateExplanation generates a human-readable explanation of why two
// alerts are considered duplicates.
func buildDuplicateExplanation(a, b featureVector) string {
	var parts []string

	commonActions := intersectKeys(a.actions, b.actions)
	commonEntities := intersectKeys(a.entities, b.entities)
	commonSources := intersectKeys(a.dataSources, b.dataSources)
	commonConditions := intersectKeys(a.conditions, b.conditions)

	if len(commonActions) > 0 {
		parts = append(parts, fmt.Sprintf("detect %s", strings.Join(commonActions, ", ")))
	}
	if len(commonEntities) > 0 {
		parts = append(parts, fmt.Sprintf("on %s", strings.Join(commonEntities, ", ")))
	}
	if len(commonSources) > 0 {
		parts = append(parts, fmt.Sprintf("from %s", strings.Join(commonSources, ", ")))
	}
	if len(commonConditions) > 0 {
		parts = append(parts, fmt.Sprintf("with %s", strings.Join(commonConditions, ", ")))
	}

	explanation := "Both " + strings.Join(parts, " ")
	if len(parts) == 0 {
		explanation = "Both alerts have highly overlapping feature sets"
	}

	// Check if one is a strict superset of the other.
	if isSuperset(a, b) {
		explanation += fmt.Sprintf(". \"%s\" is a superset of \"%s\"", a.alertName, b.alertName)
	} else if isSuperset(b, a) {
		explanation += fmt.Sprintf(". \"%s\" is a superset of \"%s\"", b.alertName, a.alertName)
	}

	return explanation
}

// isSuperset returns true when every feature token in b is also present in a
// (i.e., a is a strict superset of b across all dimensions).
func isSuperset(a, b featureVector) bool {
	if !setContains(a.dataSources, b.dataSources) {
		return false
	}
	if !setContains(a.entities, b.entities) {
		return false
	}
	if !setContains(a.actions, b.actions) {
		return false
	}
	if !setContains(a.conditions, b.conditions) {
		return false
	}
	if !setContains(a.techniques, b.techniques) {
		return false
	}
	// a must have at least one extra token to be a strict superset.
	totalA := len(a.dataSources) + len(a.entities) + len(a.actions) + len(a.conditions) + len(a.techniques)
	totalB := len(b.dataSources) + len(b.entities) + len(b.actions) + len(b.conditions) + len(b.techniques)
	return totalA > totalB
}

// setContains returns true if every element of b exists in a.
func setContains(a, b map[string]struct{}) bool {
	for k := range b {
		if _, ok := a[k]; !ok {
			return false
		}
	}
	return true
}

// intersectKeys returns the sorted list of keys present in both sets.
func intersectKeys(a, b map[string]struct{}) []string {
	var common []string
	for k := range a {
		if _, ok := b[k]; ok {
			common = append(common, k)
		}
	}
	sort.Strings(common)
	return common
}

// ---------------------------------------------------------------------------
// Step 5: Merge Suggestions
// ---------------------------------------------------------------------------

// buildMergeSuggestions looks for groups of 3+ alerts where the average
// pairwise similarity meets the merge threshold.
func buildMergeSuggestions(vectors []featureVector, matrix [][]float64, n int) []models.MergeSuggestion {
	// Re-use the family clusters but with the merge threshold for average similarity.
	// First, find connected components at the family threshold to limit the search space.
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}

	var find func(int) int
	find = func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}

	unionFn := func(x, y int) {
		rx, ry := find(x), find(y)
		if rx != ry {
			parent[rx] = ry
		}
	}

	// Link any pair above the family threshold so we evaluate coherent groups.
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if matrix[i][j] >= familyThreshold {
				unionFn(i, j)
			}
		}
	}

	clusters := make(map[int][]int)
	for i := 0; i < n; i++ {
		root := find(i)
		clusters[root] = append(clusters[root], i)
	}

	var suggestions []models.MergeSuggestion

	for _, members := range clusters {
		if len(members) < 3 {
			continue
		}

		// Compute average pairwise similarity within the cluster.
		avgSim := averagePairwiseSimilarity(matrix, members)
		if avgSim < mergeAvgThreshold {
			continue
		}

		ids := make([]string, len(members))
		names := make([]string, len(members))
		for k, idx := range members {
			ids[k] = vectors[idx].alertID
			names[k] = vectors[idx].alertName
		}

		reason := describeMergePattern(vectors, members)
		suggestions = append(suggestions, models.MergeSuggestion{
			AlertIDs:   ids,
			AlertNames: names,
			Reason:     reason,
		})
	}

	// Sort by group size descending.
	sort.Slice(suggestions, func(i, j int) bool {
		return len(suggestions[i].AlertIDs) > len(suggestions[j].AlertIDs)
	})

	return suggestions
}

// averagePairwiseSimilarity computes the mean similarity across all pairs in
// a given set of indices.
func averagePairwiseSimilarity(matrix [][]float64, members []int) float64 {
	if len(members) < 2 {
		return 0
	}
	sum := 0.0
	count := 0
	for i := 0; i < len(members); i++ {
		for j := i + 1; j < len(members); j++ {
			sum += matrix[members[i]][members[j]]
			count++
		}
	}
	return sum / float64(count)
}

// describeMergePattern generates a reason string explaining the common pattern
// across a group of mergeable alerts.
func describeMergePattern(vectors []featureVector, members []int) string {
	// Collect the union of common tokens across all members.
	actionFreq := make(map[string]int)
	sourceFreq := make(map[string]int)
	techniqueFreq := make(map[string]int)

	for _, idx := range members {
		for a := range vectors[idx].actions {
			actionFreq[a]++
		}
		for s := range vectors[idx].dataSources {
			sourceFreq[s]++
		}
		for t := range vectors[idx].techniques {
			techniqueFreq[t]++
		}
	}

	threshold := len(members) / 2 // Present in at least half the alerts.
	if threshold < 2 {
		threshold = 2
	}

	var commonPatterns []string
	for tok, c := range techniqueFreq {
		if c >= threshold {
			commonPatterns = append(commonPatterns, tok)
		}
	}
	for tok, c := range actionFreq {
		if c >= threshold {
			commonPatterns = append(commonPatterns, tok)
		}
	}
	for tok, c := range sourceFreq {
		if c >= threshold {
			commonPatterns = append(commonPatterns, tok)
		}
	}
	sort.Strings(commonPatterns)

	n := len(members)
	if len(commonPatterns) > 0 {
		return fmt.Sprintf(
			"You can replace %d rules with 1 generalized rule. Common pattern: %s",
			n, strings.Join(commonPatterns, ", "),
		)
	}

	return fmt.Sprintf(
		"You can replace %d rules with 1 generalized rule covering the same detection intent",
		n,
	)
}

// ---------------------------------------------------------------------------
// Step 6: Coverage Insights
// ---------------------------------------------------------------------------

// analyzeCoverage generates coverage gap insights from MITRE tactic data.
// Returns nil when mitreResult is nil.
func analyzeCoverage(mitreResult *models.MITRECoverageResult) []string {
	if mitreResult == nil {
		return nil
	}
	var insights []string
	var gaps []string
	var thin []string

	for _, tc := range mitreResult.Summary.TacticBreakdown {
		if tc.Total == 0 {
			continue
		}
		switch {
		case tc.Covered == 0:
			gaps = append(gaps, tc.TacticName)
		case tc.Percent < minTacticCoveragePct:
			thin = append(thin, fmt.Sprintf("%s (%.0f%%)", tc.TacticName, tc.Percent))
		}
	}

	sort.Strings(gaps)
	sort.Strings(thin)

	if len(gaps) > 0 {
		insights = append(insights, fmt.Sprintf("No alert coverage for: %s", strings.Join(gaps, ", ")))
	}
	for _, t := range thin {
		insights = append(insights, fmt.Sprintf("Thin coverage: %s — consider adding more detections", t))
	}
	return insights
}

// ---------------------------------------------------------------------------
// Step 7: Unique Detections
// ---------------------------------------------------------------------------

// findUniqueDetections returns the IDs of alerts whose maximum similarity to
// any other alert falls below the uniqueness threshold.
func findUniqueDetections(vectors []featureVector, matrix [][]float64, n int) []string {
	var unique []string
	for i := 0; i < n; i++ {
		maxSim := 0.0
		for j := 0; j < n; j++ {
			if i == j {
				continue
			}
			if matrix[i][j] > maxSim {
				maxSim = matrix[i][j]
			}
		}
		if maxSim < uniqueThreshold {
			unique = append(unique, vectors[i].alertName)
		}
	}
	return unique
}

// ---------------------------------------------------------------------------
// Step 8: Noise Detection
// ---------------------------------------------------------------------------

const behavioralNoiseThreshold = 20 // triggers in 30 days before alert is behaviorally noisy

// findNoiseAlerts applies the hybrid two-signal noise model.
//
//   - Signal 1 (behavioral): alert fired > behavioralNoiseThreshold times in the
//     last 30 days per eventCounts. Pass nil to skip behavioral detection.
//   - Signal 2 (structural): alert is unscoped (no app/subsystem), has no entity,
//     and is a high-volume type (logs_threshold, metric_threshold, logs_immediate).
//
// Exclusions (neither signal applies):
//
//   - Vendor-covered alerts: intentionally sparse, vendor does detection internally.
//   - Building blocks (flow_alert=building block): fragments by design.
//   - Non-security alerts: outside the scope of security noise analysis.
//
// Flow alerts skip structural (assessed via building blocks) but ARE checked for
// behavioral noise — a flow firing too often is genuinely noisy.
//
// alerts is parallel to vectors (same index). Pass nil alerts in tests that
// do not need alert-level fields (behavioral and structural signals are skipped).
func findNoiseAlerts(
	vectors []featureVector,
	alerts []*models.AlertDef,
	eventCounts map[string]int,  // alertID → 30-day trigger count; nil = skip behavioral
	integrationCount int,         // total integrations in org (for structural reason text)
) []models.NoiseAlert {
	var noisy []models.NoiseAlert

	for i, v := range vectors {
		var alert *models.AlertDef
		if alerts != nil && i < len(alerts) {
			alert = alerts[i]
		}

		// ── Exclusions ────────────────────────────────────────────────────
		if alert != nil {
			if alert.Features.VendorCovered {
				continue
			}
			if alert.Labels["flow_alert"] == "building block" {
				continue
			}
			if !alert.Features.IsSecurityAlert {
				continue
			}
		}

		isFlowAlert := alert != nil && alert.AlertType == "flow"

		// ── Signal 1: Behavioral ─────────────────────────────────────────
		var triggerCount int
		if alert != nil && eventCounts != nil {
			triggerCount = eventCounts[alert.ID]
		}
		isBehavioral := eventCounts != nil && triggerCount > behavioralNoiseThreshold

		// ── Signal 2: Structural (skipped for flow alerts) ───────────────
		// An alert is structurally noisy when it is unscoped, has no entity
		// filter, and is a high-volume alert type. When event count data is
		// available we additionally require triggerCount > 0: an alert that
		// never fired in 30 days cannot be producing undesired volume, so
		// flagging it as structural noise would be a false positive (e.g. a
		// tight Lucene query like "Access Review Deletion" with no scope).
		isStructural := false
		if !isFlowAlert && alert != nil {
			app, sub := coralogix.ExtractAppSubsystem(alert.TypeDef)
			isUnscoped := app == "" && sub == ""
			noEntity := len(v.entities) == 0
			isHighVolumeType := alert.AlertType == "logs_threshold" ||
				alert.AlertType == "metric_threshold" ||
				alert.AlertType == "logs_immediate"
			hasEvidenceOfVolume := eventCounts == nil || triggerCount > 0
			isStructural = isUnscoped && noEntity && isHighVolumeType && hasEvidenceOfVolume
		}

		// ── Neither signal → skip ─────────────────────────────────────────
		if !isBehavioral && !isStructural {
			continue
		}

		noisy = append(noisy, models.NoiseAlert{
			Name:            v.alertName,
			MissingFeatures: buildMissingFeatures(v),
			Reason:          buildNoiseReason(triggerCount, integrationCount, isBehavioral, isStructural),
			TriggerCount:    triggerCount,
			NoiseType:       noiseTypeString(isBehavioral, isStructural),
		})
	}

	sort.Slice(noisy, func(i, j int) bool {
		return noisy[i].Name < noisy[j].Name
	})
	return noisy
}

// noiseTypeString returns "behavioral", "structural", or "both".
// Callers must ensure at least one signal is true before calling.
func noiseTypeString(isBehavioral, isStructural bool) string {
	switch {
	case isBehavioral && isStructural:
		return "both"
	case isBehavioral:
		return "behavioral"
	case isStructural:
		return "structural"
	default:
		return "" // unreachable: caller guards on isBehavioral || isStructural
	}
}

// buildNoiseReason returns a specific human-readable reason for the noise classification.
func buildNoiseReason(triggerCount, integrationCount int, isBehavioral, isStructural bool) string {
	var parts []string
	if isBehavioral {
		parts = append(parts, fmt.Sprintf(
			"Fired %d times in the last 30 days — alert is over-triggering.", triggerCount))
	}
	if isStructural {
		if integrationCount >= 10 {
			parts = append(parts, fmt.Sprintf(
				"No app/subsystem scoping across an org with %d integrations — fires on all matching log sources.",
				integrationCount))
		} else {
			parts = append(parts, "No app/subsystem scoping and no entity filter — alert may fire too broadly.")
		}
	}
	return strings.Join(parts, " ")
}

// buildMissingFeatures returns the names of empty feature dimensions for this vector.
func buildMissingFeatures(v featureVector) []string {
	var missing []string
	if len(v.dataSources) == 0 {
		missing = append(missing, "data sources")
	}
	if len(v.entities) == 0 {
		missing = append(missing, "entities")
	}
	if len(v.actions) == 0 {
		missing = append(missing, "actions")
	}
	if len(v.conditions) == 0 {
		missing = append(missing, "conditions")
	}
	if len(v.techniques) == 0 {
		missing = append(missing, "techniques")
	}
	return missing
}
