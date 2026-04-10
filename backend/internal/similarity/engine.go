package similarity

import (
	"fmt"
	"math"
	"runtime"
	"sort"
	"strings"
	"sync"

	"coralogix-alert-analyzer/internal/models"
)

// featureVector holds the tokenized, lowercase feature sets for a single alert.
type featureVector struct {
	alertID     string
	alertName   string
	alertType   string
	dataSources map[string]struct{}
	entities    map[string]struct{}
	actions     map[string]struct{}
	conditions  map[string]struct{}
	techniques  map[string]struct{}
}

// pairScore stores the similarity score between two alerts.
type pairScore struct {
	i, j  int
	score float64
}

// Similarity weights for each feature dimension.
const (
	weightDataSources = 0.20
	weightEntities    = 0.15
	weightActions     = 0.20
	weightConditions  = 0.20
	weightTechniques  = 0.15
	weightAlertType   = 0.10

	duplicateThreshold = 0.85
	familyThreshold    = 0.60
	mergeAvgThreshold  = 0.70
	uniqueThreshold    = 0.30

	// When the number of alerts exceeds this value, pairwise comparison is
	// parallelised across a worker pool.
	parallelThreshold = 50
)

// Common security categories used for gap analysis.
var commonCategories = []string{
	// Identity
	"login anomalies",
	"mfa bypass",
	"credential stuffing",
	"token abuse",
	"session hijacking",
	// Endpoint
	"malware execution",
	"persistence",
	"privilege escalation",
	// Cloud
	"iam abuse",
	"storage exfiltration",
	"resource abuse",
	// Network
	"lateral movement",
	"port scanning",
	"c2 traffic",
	// Data
	"data exfiltration",
	"sensitive data access",
	"api abuse",
	// Additional
	"ransomware",
	"supply chain",
	"insider threat",
}

// Analyze performs full similarity analysis on a set of alert definitions and
// returns families, duplicates, merge suggestions, coverage insights and
// unique-detection identifiers.
func Analyze(alerts []*models.AlertDef) *models.SimilarityResult {
	if len(alerts) == 0 {
		return &models.SimilarityResult{}
	}

	// Step 1: Build feature vectors.
	vectors := buildFeatureVectors(alerts)

	// Step 2: Compute pairwise similarity scores.
	n := len(vectors)
	scores := computePairwiseScores(vectors, n)

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

	// Step 6: Coverage insights.
	coverageInsights := analyzeCoverage(vectors)

	// Step 7: Unique detections.
	uniqueDetections := findUniqueDetections(vectors, matrix, n)

	// Step 8: Noise detection.
	noiseAlerts := findNoiseAlerts(vectors)

	return &models.SimilarityResult{
		Families:         families,
		Duplicates:       duplicates,
		MergeSuggestions: mergeSuggestions,
		CoverageInsights: coverageInsights,
		UniqueDetections: uniqueDetections,
		NoiseAlerts:      noiseAlerts,
	}
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
			alertID:     a.ID,
			alertName:   a.Name,
			alertType:   strings.ToLower(a.AlertType),
			dataSources: toSet(a.Features.DataSources),
			entities:    toSet(a.Features.Entities),
			actions:     toSet(a.Features.Actions),
			conditions:  toSet(a.Features.Conditions),
			techniques:  toSet(a.Features.Techniques),
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
func computePairwiseScores(vectors []featureVector, n int) []pairScore {
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
				results[idx] = pairScore{i: i, j: j, score: scorePair(vectors[i], vectors[j])}
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
					score: scorePair(vectors[p.i], vectors[p.j]),
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

// scorePair computes the weighted Jaccard similarity between two feature vectors.
func scorePair(a, b featureVector) float64 {
	score := 0.0
	score += weightDataSources * jaccard(a.dataSources, b.dataSources)
	score += weightEntities * jaccard(a.entities, b.entities)
	score += weightActions * jaccard(a.actions, b.actions)
	score += weightConditions * jaccard(a.conditions, b.conditions)
	score += weightTechniques * jaccard(a.techniques, b.techniques)

	if a.alertType == b.alertType && a.alertType != "" {
		score += weightAlertType
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

// deriveFamilyName picks the most frequent technique or action across the
// cluster members and builds a human-readable family name.
func deriveFamilyName(vectors []featureVector, members []int, fallbackNum int) string {
	freq := make(map[string]int)

	// Count technique tokens first (higher signal).
	for _, idx := range members {
		for t := range vectors[idx].techniques {
			freq[t]++
		}
	}

	// If no techniques, fall back to actions.
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

	// Find the most common token.
	bestToken := ""
	bestCount := 0
	for tok, count := range freq {
		if count > bestCount || (count == bestCount && tok < bestToken) {
			bestToken = tok
			bestCount = count
		}
	}

	// Title-case the token for readability.
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

// analyzeCoverage checks for over-covered and under-covered security areas.
func analyzeCoverage(vectors []featureVector) []string {
	var insights []string

	// Count how many alerts mention each common category by scanning all
	// feature dimensions.
	categoryCounts := make(map[string]int, len(commonCategories))
	for _, cat := range commonCategories {
		categoryCounts[cat] = 0
	}

	for _, v := range vectors {
		allTokens := collectAllTokens(v)
		for _, cat := range commonCategories {
			if categoryMatchesTokens(cat, allTokens) {
				categoryCounts[cat]++
			}
		}
	}

	// Identify over-covered and gap areas.
	maxCount := 0
	maxCat := ""
	for _, cat := range commonCategories {
		if categoryCounts[cat] > maxCount {
			maxCount = categoryCounts[cat]
			maxCat = cat
		}
	}

	var gaps []string
	for _, cat := range commonCategories {
		if categoryCounts[cat] == 0 {
			gaps = append(gaps, cat)
		}
	}

	if maxCount > 0 && len(gaps) > 0 {
		insights = append(insights, fmt.Sprintf(
			"You have %d detections for %s but none for %s",
			maxCount, maxCat, strings.Join(gaps, ", "),
		))
	} else if len(gaps) > 0 {
		insights = append(insights, fmt.Sprintf(
			"No coverage detected for: %s",
			strings.Join(gaps, ", "),
		))
	}

	// Per-gap insights.
	for _, gap := range gaps {
		insights = append(insights, fmt.Sprintf(
			"Consider adding detections for %s", gap,
		))
	}

	// Flag heavy concentration.
	for _, cat := range commonCategories {
		if categoryCounts[cat] >= 5 {
			insights = append(insights, fmt.Sprintf(
				"Heavy concentration: %d rules cover %s - review for redundancy",
				categoryCounts[cat], cat,
			))
		}
	}

	return insights
}

// collectAllTokens gathers every feature token from a vector into a single set.
func collectAllTokens(v featureVector) map[string]struct{} {
	all := make(map[string]struct{})
	for k := range v.dataSources {
		all[k] = struct{}{}
	}
	for k := range v.entities {
		all[k] = struct{}{}
	}
	for k := range v.actions {
		all[k] = struct{}{}
	}
	for k := range v.conditions {
		all[k] = struct{}{}
	}
	for k := range v.techniques {
		all[k] = struct{}{}
	}
	if v.alertType != "" {
		all[v.alertType] = struct{}{}
	}
	return all
}

// categoryMatchesTokens returns true if any token in the set contains words
// from the category name (fuzzy keyword matching).
func categoryMatchesTokens(category string, tokens map[string]struct{}) bool {
	// Split the category into keywords (e.g. "privilege escalation" -> ["privilege", "escalation"]).
	keywords := strings.Fields(strings.ToLower(category))

	for tok := range tokens {
		matchCount := 0
		for _, kw := range keywords {
			if strings.Contains(tok, kw) {
				matchCount++
			}
		}
		// All keywords must appear in at least one token.
		if matchCount == len(keywords) {
			return true
		}
	}

	// Also check if all keywords appear across different tokens.
	if len(keywords) > 1 {
		matched := 0
		for _, kw := range keywords {
			for tok := range tokens {
				if strings.Contains(tok, kw) {
					matched++
					break
				}
			}
		}
		if matched == len(keywords) {
			return true
		}
	}

	return false
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
			unique = append(unique, vectors[i].alertID)
		}
	}
	return unique
}

// ---------------------------------------------------------------------------
// Step 8: Noise Detection
// ---------------------------------------------------------------------------

// findNoiseAlerts returns names of alerts whose total unique feature token
// count is below the noise threshold (sparse = likely threshold-only alert).
func findNoiseAlerts(vectors []featureVector) []string {
	const noiseThreshold = 3
	var noisy []string
	for _, v := range vectors {
		total := len(v.dataSources) + len(v.entities) + len(v.actions) +
			len(v.conditions) + len(v.techniques)
		if total < noiseThreshold {
			noisy = append(noisy, v.alertName)
		}
	}
	sort.Strings(noisy)
	return noisy
}
