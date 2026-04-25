package service

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/strvals"

	"github.com/qdeck-app/qdeck/domain"
)

const (
	valuesFileName = "values.yaml"
	saveFilePerm   = 0o644
)

const (
	typeString  = "string"
	typeBool    = "bool"
	typeNumber  = "number"
	typeNull    = "null"
	typeUnknown = "unknown"
)

type stackItem struct {
	prefix string
	val    any
}

type ValuesService struct{}

func NewValuesService() *ValuesService {
	return &ValuesService{}
}

func (s *ValuesService) ReadDefaultValues(ctx context.Context, chartPath string) (*domain.ValuesFile, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("read default values: %w", ctx.Err())
	}

	ch, err := loader.Load(chartPath)
	if err != nil {
		return nil, fmt.Errorf("load chart from %s: %w", chartPath, err)
	}

	parentVF := flattenValues("default", ch.Values)

	if rawData := findRawValuesFile(ch.Raw); rawData != nil {
		if comments, parseErr := parseComments(rawData); parseErr == nil {
			attachComments(parentVF, comments)
		}

		if oc, parseErr := parseOrphanComments(rawData); parseErr == nil {
			parentVF.DocHeadComment = oc.DocHead
			parentVF.DocFootComment = oc.DocFoot
			parentVF.FootComments = oc.Foots
		}

		var doc yaml.Node
		if parseErr := yaml.Unmarshal(rawData, &doc); parseErr == nil && len(doc.Content) > 0 {
			parentVF.NodeTree = doc.Content[0]
		}
	}

	if len(ch.Dependencies()) == 0 {
		return parentVF, nil
	}

	depEntries := collectDependencyValues(ch, "")
	merged := mergeParentOverDeps(parentVF.Entries, depEntries)

	slices.SortFunc(merged, func(a, b domain.ValuesEntry) int {
		return strings.Compare(string(a.Key), string(b.Key))
	})

	return &domain.ValuesFile{
		Source:         "default",
		Entries:        merged,
		NodeTree:       parentVF.NodeTree,
		DocHeadComment: parentVF.DocHeadComment,
		DocFootComment: parentVF.DocFootComment,
		FootComments:   parentVF.FootComments,
	}, nil
}

func (s *ValuesService) LoadDefaultValues(ctx context.Context, chartPath string) (*FlatValues, error) {
	vf, err := s.ReadDefaultValues(ctx, chartPath)
	if err != nil {
		return nil, fmt.Errorf("load default values from %s: %w", chartPath, err)
	}

	return newFlatValues(vf), nil
}

// ComputeDiff computes the difference between default and custom values.
// Uses a two-pointer merge on pre-sorted entry slices -- O(n+m), no quadratic sorts.
//
//nolint:dupl // Two-pointer merge branches are structurally similar but semantically distinct.
func (s *ValuesService) ComputeDiff(defaults, custom *FlatValues) *DiffResult {
	if defaults == nil || custom == nil {
		return &DiffResult{}
	}

	dEntries := defaults.Entries
	cEntries := custom.Entries
	result := &DiffResult{
		Lines: make([]DiffLine, 0, len(dEntries)+len(cEntries)),
	}
	i, j := 0, 0

	for i < len(dEntries) || j < len(cEntries) {
		switch {
		case i >= len(dEntries):
			result.Lines = append(result.Lines, DiffLine{
				Key: cEntries[j].Key, CustomValue: cEntries[j].Value,
				Status: domain.DiffAdded,
			})
			result.Stats.Added++
			j++
		case j >= len(cEntries):
			result.Lines = append(result.Lines, DiffLine{
				Key: dEntries[i].Key, DefaultValue: dEntries[i].Value,
				Status: domain.DiffRemoved,
			})
			result.Stats.Removed++
			i++
		case dEntries[i].Key < cEntries[j].Key:
			result.Lines = append(result.Lines, DiffLine{
				Key: dEntries[i].Key, DefaultValue: dEntries[i].Value,
				Status: domain.DiffRemoved,
			})
			result.Stats.Removed++
			i++
		case dEntries[i].Key > cEntries[j].Key:
			result.Lines = append(result.Lines, DiffLine{
				Key: cEntries[j].Key, CustomValue: cEntries[j].Value,
				Status: domain.DiffAdded,
			})
			result.Stats.Added++
			j++
		default: // keys equal
			status := domain.DiffUnchanged
			if dEntries[i].Value != cEntries[j].Value {
				status = domain.DiffChanged
				result.Stats.Changed++
			} else {
				result.Stats.Unchanged++
			}

			result.Lines = append(result.Lines, DiffLine{
				Key: dEntries[i].Key, DefaultValue: dEntries[i].Value,
				CustomValue: cEntries[j].Value, Status: status,
			})
			i++
			j++
		}
	}

	return result
}

func (s *ValuesService) ReadCustomValues(ctx context.Context, filePath string) (*domain.ValuesFile, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("read custom values: %w", ctx.Err())
	}

	vals, err := chartutil.ReadValuesFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read values file %s: %w", filePath, err)
	}

	vf := flattenValues(filePath, vals)
	vf.RawValues = vals

	rawData, readErr := os.ReadFile(filePath) //nolint:gosec // filePath already validated by ReadValuesFile above
	if readErr == nil {
		vf.Indent = DetectYAMLIndent(rawData)

		// Silently ignore comment parse errors—if comments can't be extracted,
		// values are still valid, just without associated documentation.
		if comments, parseErr := parseComments(rawData); parseErr == nil {
			attachComments(vf, comments)
		}

		if oc, parseErr := parseOrphanComments(rawData); parseErr == nil {
			vf.DocHeadComment = oc.DocHead
			vf.DocFootComment = oc.DocFoot
			vf.FootComments = oc.Foots
		}

		var doc yaml.Node
		if parseErr := yaml.Unmarshal(rawData, &doc); parseErr == nil && len(doc.Content) > 0 {
			vf.NodeTree = doc.Content[0]
		}
	}

	return vf, nil
}

// ReadAndMergeCustomValues loads multiple values files and deep-merges them.
// Later files override earlier ones for both data and per-leaf head/line
// comments. The file-level shape — DocHeadComment, DocFootComment,
// FootComments, and the preserved yaml.Node tree — is taken from the LAST
// file only; earlier files' banners and trailers are intentionally dropped
// because stacking multiple files' headers has no coherent rendering. The
// indent setting is taken from the FIRST file so the canonical indent of
// the user's primary values.yaml is preserved through edits.
func (s *ValuesService) ReadAndMergeCustomValues(ctx context.Context, paths []string) (*domain.ValuesFile, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("read and merge custom values: %w", ctx.Err())
	}

	merged := make(map[string]any)
	mergedComments := make(map[string]string)
	indent := DefaultYAMLIndent

	var (
		lastNodeTree *yaml.Node
		lastDocHead  string
		lastDocFoot  string
		lastFoots    map[string]string
	)

	for i, path := range paths {
		vals, err := chartutil.ReadValuesFile(path)
		if err != nil {
			return nil, fmt.Errorf("read values file %s: %w", path, err)
		}

		deepMerge(merged, vals)

		rawData, readErr := os.ReadFile(path) //nolint:gosec // path validated by ReadValuesFile
		if readErr == nil {
			// Detect indentation from the first file.
			if i == 0 {
				indent = DetectYAMLIndent(rawData)
			}

			if comments, parseErr := parseComments(rawData); parseErr == nil {
				for k, v := range comments {
					mergedComments[k] = v
				}
			}

			// Banner / trailer / foot comments come from the last file only,
			// matching how lastNodeTree is chosen. Earlier files' file-level
			// comments wouldn't render coherently when stacked.
			if oc, parseErr := parseOrphanComments(rawData); parseErr == nil {
				lastDocHead = oc.DocHead
				lastDocFoot = oc.DocFoot
				lastFoots = oc.Foots
			}

			// Parse yaml.Node tree for comment-preserving serialization.
			// Only the last file's tree is kept — inline/subtree comments from earlier files
			// are lost for keys not redefined in later files. Head comments are preserved
			// separately via mergedComments. This is acceptable because the last file
			// represents the user's most recent intent.
			var doc yaml.Node
			if parseErr := yaml.Unmarshal(rawData, &doc); parseErr == nil && len(doc.Content) > 0 {
				lastNodeTree = doc.Content[0]
			}
		}
	}

	vf := flattenValues("merged", merged)
	vf.RawValues = merged
	vf.Indent = indent
	vf.NodeTree = lastNodeTree
	vf.DocHeadComment = lastDocHead
	vf.DocFootComment = lastDocFoot
	vf.FootComments = lastFoots

	attachComments(vf, mergedComments)

	return vf, nil
}

// LoadAndMergeCustomValues loads multiple values files and deep-merges them.
// Later files override earlier ones.
func (s *ValuesService) LoadAndMergeCustomValues(ctx context.Context, paths []string) (*FlatValues, error) {
	merged, err := s.ReadAndMergeCustomValues(ctx, paths)
	if err != nil {
		return nil, fmt.Errorf("merge custom values: %w", err)
	}

	return newFlatValues(merged), nil
}

// ParseYAMLText parses raw YAML text into flattened values.
func (s *ValuesService) ParseYAMLText(ctx context.Context, yamlText string) (*domain.ValuesFile, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("parse YAML text: %w", ctx.Err())
	}

	vals, err := chartutil.ReadValues([]byte(yamlText))
	if err != nil {
		return nil, fmt.Errorf("parse YAML text: %w", err)
	}

	vf := flattenValues("editor", vals)
	vf.RawValues = vals

	return vf, nil
}

// ParseEditorContent parses raw YAML text from the editor into flat values.
func (s *ValuesService) ParseEditorContent(ctx context.Context, yamlText string) (*FlatValues, error) {
	vf, err := s.ParseYAMLText(ctx, yamlText)
	if err != nil {
		return nil, fmt.Errorf("parse editor content: %w", err)
	}

	return newFlatValues(vf), nil
}

// BuildOverrideMap converts flat key-value pairs into a nested map suitable
// for Helm template rendering. Uses strvals.ParseInto (same as helm install --set)
// to handle dot-path nesting and type inference.
func (s *ValuesService) BuildOverrideMap(keys []string, values []string) (map[string]any, error) {
	result := make(map[string]any)

	for i := range keys {
		if keys[i] == "" || i >= len(values) || values[i] == "" {
			continue
		}

		if err := strvals.ParseInto(keys[i]+"="+values[i], result); err != nil {
			return nil, fmt.Errorf("parse override %s: %w", keys[i], err)
		}
	}

	return result, nil
}

// SaveValuesFile writes YAML text to the given file path.
func (s *ValuesService) SaveValuesFile(ctx context.Context, yamlText, destPath string) error {
	if ctx.Err() != nil {
		return fmt.Errorf("save values file: %w", ctx.Err())
	}

	if err := os.WriteFile(destPath, []byte(yamlText), saveFilePerm); err != nil {
		return fmt.Errorf("save values file to %s: %w", destPath, err)
	}

	return nil
}

// CompareWithBaseline compares a current values file against baseline content (e.g. from git HEAD).
// Returns a map of flat keys to their change status. Only added/modified keys are included.
func (s *ValuesService) CompareWithBaseline(
	ctx context.Context, currentFilePath string, baselineContent []byte,
) (map[string]domain.GitChangeStatus, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("compare with baseline: %w", ctx.Err())
	}

	// Parse baseline.
	baseVals, err := chartutil.ReadValues(baselineContent)
	if err != nil {
		return nil, fmt.Errorf("parse baseline values: %w", err)
	}

	baseFlat := flattenValues("baseline", baseVals)

	// Parse current file.
	curVals, err := chartutil.ReadValuesFile(currentFilePath)
	if err != nil {
		return nil, fmt.Errorf("read current values %s: %w", currentFilePath, err)
	}

	curFlat := flattenValues(currentFilePath, curVals)

	// Build baseline lookup.
	baseLookup := make(map[string]string, len(baseFlat.Entries))
	for _, e := range baseFlat.Entries {
		baseLookup[string(e.Key)] = e.Value
	}

	changes := make(map[string]domain.GitChangeStatus)

	for _, e := range curFlat.Entries {
		key := string(e.Key)

		baseVal, exists := baseLookup[key]
		if !exists {
			changes[key] = domain.GitAdded
		} else if baseVal != e.Value {
			changes[key] = domain.GitModified
		}
	}

	return changes, nil
}

// ApplyCollapseFilter removes entries whose key has any collapsed ancestor,
// i.e. the entry sits inside a user-collapsed section. The collapsed section
// header itself is kept visible so the user can click its chevron to expand
// again. Reuses `out` (truncated) to avoid per-frame allocation; `out` and
// `indices` may safely alias — the write pointer never overtakes the read
// pointer. When `collapsed` is empty the function short-circuits and returns
// `indices` unchanged (no copy into `out`).
func ApplyCollapseFilter(
	entries []FlatValueEntry,
	indices []int,
	collapsed map[string]bool,
	out []int,
) []int {
	if len(collapsed) == 0 {
		return indices
	}

	out = out[:0]

	// Comment rows have no flat key of their own — visibility piggy-backs on the
	// most recently seen non-comment row so a foot comment under a collapsed
	// section disappears with that section.
	lastNonCommentVisible := true

	for _, idx := range indices {
		if idx >= len(entries) {
			continue
		}

		entry := entries[idx]

		if entry.IsComment() {
			if lastNonCommentVisible {
				out = append(out, idx)
			}

			continue
		}

		key := entry.Key

		// The collapsed section header itself stays visible.
		if collapsed[key] {
			lastNonCommentVisible = true

			out = append(out, idx)

			continue
		}

		hidden := false

		for p := domain.FlatKey(key).Parent(); p != ""; p = p.Parent() {
			if collapsed[string(p)] {
				hidden = true

				break
			}
		}

		lastNonCommentVisible = !hidden

		if !hidden {
			out = append(out, idx)
		}
	}

	return out
}

// UncollapseMatchAncestors mutates `collapsed` to remove any entry that is a
// proper ancestor of a matched index, so search results inside collapsed
// sections become visible. Returns true if the set was modified (caller should
// persist the change).
func UncollapseMatchAncestors(
	entries []FlatValueEntry,
	indices []int,
	collapsed map[string]bool,
) bool {
	if len(collapsed) == 0 {
		return false
	}

	modified := false

	for _, idx := range indices {
		if idx >= len(entries) {
			continue
		}

		for p := domain.FlatKey(entries[idx].Key).Parent(); p != ""; p = p.Parent() {
			if collapsed[string(p)] {
				delete(collapsed, string(p))

				modified = true
			}
		}
	}

	return modified
}

func newFlatValues(vf *domain.ValuesFile) *FlatValues {
	entries := make([]FlatValueEntry, len(vf.Entries))
	for i, e := range vf.Entries {
		entries[i] = FlatValueEntry{
			Key:     string(e.Key),
			Value:   e.Value,
			Type:    e.Type,
			Depth:   e.Key.Depth(),
			Comment: e.Comment,
		}
	}

	// Foot-comment rows are NOT spliced in here. They're file-source-specific
	// (chart defaults vs. user's custom values) and shouldn't be cloned through
	// RebuildUnifiedEntries from defaults — only the user's own custom file
	// annotations belong in the unified table. The unified-entries layer
	// reads FootComments / DocHeadComment / DocFootComment off the column's
	// CustomValues directly and injects rows from there.
	return &FlatValues{
		Entries:        entries,
		RawValues:      vf.RawValues,
		Indent:         vf.Indent,
		NodeTree:       vf.NodeTree,
		Anchors:        ExtractAnchors(vf.NodeTree),
		DocHeadComment: vf.DocHeadComment,
		DocFootComment: vf.DocFootComment,
		FootComments:   vf.FootComments,
		KeyPositions:   buildFlatKeyPositions(vf.NodeTree),
	}
}

// buildFlatKeyPositions walks the yaml.Node tree in DFS order and assigns
// each visited mapping key (and sequence-item synthetic key like "[3]") a
// sequential index, producing a flat-key → file-position map.
//
// The map is then used to sort the UI's unified entries list so it reflects
// the source file's structure instead of alphabetical collation. Returns nil
// for a missing or non-mapping root.
//
// Aliases (`*name`) point to the resolved node so flat keys derived from the
// expanded structure (e.g. `master.persistence.enabled` when
// `persistence: *defaults` aliases a defaults block with that leaf) still get
// positions matching where they'd appear in source order. Merge keys
// (`<<: *base`) inject sibling keys that this walk never visits — those keys
// fall back to "end of file" positioning at sort time, which is fine for the
// rare case of merged-in keys appearing in the unified table.
func buildFlatKeyPositions(root *yaml.Node) map[string]int {
	if root == nil {
		return nil
	}

	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}

	if root.Kind != yaml.MappingNode {
		return nil
	}

	positions := make(map[string]int)
	counter := 0

	var walk func(node *yaml.Node, prefix string)

	walk = func(node *yaml.Node, prefix string) {
		switch node.Kind {
		case yaml.MappingNode:
			for i := 0; i < len(node.Content)-1; i += 2 {
				k := node.Content[i]
				v := node.Content[i+1]

				if k == nil || v == nil || k.Value == "" {
					continue
				}

				// Skip the merge-key indicator itself; merged-in siblings
				// land at the parent's level and aren't part of this walk.
				if k.Tag == mergeTag || k.Value == mergeKey {
					continue
				}

				key := buildKey(prefix, k.Value)

				if _, seen := positions[key]; !seen {
					positions[key] = counter
					counter++
				}

				target := v
				if v.Kind == yaml.AliasNode && v.Alias != nil {
					target = v.Alias
				}

				if target.Kind == yaml.MappingNode || target.Kind == yaml.SequenceNode {
					walk(target, key)
				}
			}
		case yaml.SequenceNode:
			for i, child := range node.Content {
				if child == nil {
					continue
				}

				key := prefix + "[" + strconv.Itoa(i) + "]"

				if _, seen := positions[key]; !seen {
					positions[key] = counter
					counter++
				}

				target := child
				if child.Kind == yaml.AliasNode && child.Alias != nil {
					target = child.Alias
				}

				if target.Kind == yaml.MappingNode || target.Kind == yaml.SequenceNode {
					walk(target, key)
				}
			}
		}
	}

	walk(root, "")

	return positions
}

// SortByFilePositions reorders entries to match the source file's DFS layout.
// Entries whose flat key has no recorded position (custom-only keys not in
// the chart-defaults file, or keys merged in via `<<: *base`) sort to the
// end, ordered alphabetically among themselves so the result stays
// deterministic. Stable sort preserves relative order for ties — important
// when the same position would otherwise produce a flap between frames.
//
// Empty positions or empty entries are no-ops; the caller can pass nil
// without checking.
func SortByFilePositions(entries []FlatValueEntry, positions map[string]int) {
	if len(positions) == 0 || len(entries) == 0 {
		return
	}

	end := len(positions)

	slices.SortStableFunc(entries, func(a, b FlatValueEntry) int {
		pa, hasA := positions[a.Key]
		pb, hasB := positions[b.Key]

		if !hasA {
			pa = end
		}

		if !hasB {
			pb = end
		}

		if pa != pb {
			return pa - pb
		}

		return strings.Compare(a.Key, b.Key)
	})
}

// flattenValues converts a nested map[string]any to a sorted flat list.
// Uses a stack-based iterative approach. All sorting uses slices.SortFunc (pdqsort).
func flattenValues(source string, vals map[string]any) *domain.ValuesFile {
	vf := &domain.ValuesFile{Source: source}
	if len(vals) == 0 {
		return vf
	}

	stack := []stackItem{{prefix: "", val: vals}}

	for len(stack) > 0 {
		item := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		switch typedVal := item.val.(type) {
		case map[string]any:
			if len(typedVal) == 0 {
				vf.Entries = append(vf.Entries, domain.ValuesEntry{
					Key: domain.FlatKey(item.prefix), Value: "{}", Type: "map",
				})

				continue
			}

			// Emit section header for non-root maps.
			if item.prefix != "" {
				vf.Entries = append(vf.Entries, domain.ValuesEntry{
					Key: domain.FlatKey(item.prefix), Type: "map",
				})
			}

			keys := make([]string, 0, len(typedVal))
			for k := range typedVal {
				keys = append(keys, k)
			}

			slices.SortFunc(keys, strings.Compare)
			// Push in reverse order so they come off stack in sorted order.
			for i := len(keys) - 1; i >= 0; i-- {
				k := keys[i]
				fullKey := buildKey(item.prefix, k)
				stack = append(stack, stackItem{prefix: fullKey, val: typedVal[k]})
			}

		case []any:
			if len(typedVal) == 0 {
				vf.Entries = append(vf.Entries, domain.ValuesEntry{
					Key: domain.FlatKey(item.prefix), Value: "[]", Type: "list",
				})

				continue
			}

			// Emit section header for non-empty lists.
			if item.prefix != "" {
				vf.Entries = append(vf.Entries, domain.ValuesEntry{
					Key: domain.FlatKey(item.prefix), Type: "list",
				})
			}
			// Push in reverse order for correct index ordering.
			for i := len(typedVal) - 1; i >= 0; i-- {
				prefix := item.prefix + "[" + strconv.Itoa(i) + "]"
				stack = append(stack, stackItem{prefix: prefix, val: typedVal[i]})
			}

		default:
			vf.Entries = append(vf.Entries, domain.ValuesEntry{
				Key:   domain.FlatKey(item.prefix),
				Value: fmt.Sprintf("%v", typedVal),
				Type:  inferType(typedVal),
			})
		}
	}

	slices.SortFunc(vf.Entries, func(a, b domain.ValuesEntry) int {
		return strings.Compare(string(a.Key), string(b.Key))
	})

	return vf
}

func buildKey(prefix, segment string) string {
	if prefix == "" {
		return segment
	}

	return prefix + "." + segment
}

func inferType(v any) string {
	switch v.(type) {
	case string:
		return typeString
	case bool:
		return typeBool
	case int, int64, float64:
		return typeNumber
	case nil:
		return typeNull
	default:
		return typeUnknown
	}
}

// findRawValuesFile finds the raw values.yaml content from the chart's Raw files.
func findRawValuesFile(rawFiles []*chart.File) []byte {
	for _, f := range rawFiles {
		if f.Name == valuesFileName {
			return f.Data
		}
	}

	return nil
}

// deepMerge recursively merges src into dst. Values in src override dst.
func deepMerge(dst, src map[string]any) {
	for key, srcVal := range src {
		dstVal, exists := dst[key]
		if !exists {
			dst[key] = srcVal

			continue
		}

		srcMap, srcIsMap := srcVal.(map[string]any)
		dstMap, dstIsMap := dstVal.(map[string]any)

		if srcIsMap && dstIsMap {
			deepMerge(dstMap, srcMap)
		} else {
			dst[key] = srcVal
		}
	}
}

// attachComments merges parsed comments into flattened entries.
func attachComments(vf *domain.ValuesFile, comments map[string]string) {
	for i := range vf.Entries {
		if c, ok := comments[string(vf.Entries[i].Key)]; ok {
			vf.Entries[i].Comment = c
		}
	}
}

// collectDependencyValues recursively collects flattened values from all
// chart dependencies, prefixing keys with the dependency name or alias.
func collectDependencyValues(ch *chart.Chart, parentPrefix string) []domain.ValuesEntry {
	deps := ch.Dependencies()
	if len(deps) == 0 || ch.Metadata == nil {
		return nil
	}

	// Each dependency's effective prefix is its alias when declared, otherwise
	// its name. Build the lookup once so nested charts resolve in O(1).
	prefixMap := make(map[string]string, len(ch.Metadata.Dependencies))

	for _, d := range ch.Metadata.Dependencies {
		prefix := d.Alias
		if prefix == "" {
			prefix = d.Name
		}

		prefixMap[d.Name] = prefix
	}

	var all []domain.ValuesEntry

	for _, dep := range deps {
		prefix := prefixMap[dep.Name()]
		if prefix == "" {
			prefix = dep.Name()
		}

		fullPrefix := prefix
		if parentPrefix != "" {
			fullPrefix = buildKey(parentPrefix, prefix)
		}

		subVF := flattenValues("dep:"+dep.Name(), dep.Values)

		if rawData := findRawValuesFile(dep.Raw); rawData != nil {
			// Silently ignore comment parse errors—if comments can't be extracted,
			// values are still valid, just without associated documentation.
			if comments, parseErr := parseComments(rawData); parseErr == nil {
				attachComments(subVF, comments)
			}
		}

		for _, e := range subVF.Entries {
			all = append(all, domain.ValuesEntry{
				Key:     domain.FlatKey(buildKey(fullPrefix, string(e.Key))),
				Value:   e.Value,
				Type:    e.Type,
				Comment: e.Comment,
			})
		}

		all = append(all, collectDependencyValues(dep, fullPrefix)...)
	}

	return all
}

// mergeParentOverDeps merges parent entries over dependency entries.
// Parent entries always win on key collision. Subchart comments are
// backfilled onto parent entries that lack their own comment.
func mergeParentOverDeps(parentEntries, depEntries []domain.ValuesEntry) []domain.ValuesEntry {
	parentMap := make(map[domain.FlatKey]int, len(parentEntries))
	merged := make([]domain.ValuesEntry, 0, len(parentEntries)+len(depEntries))
	merged = append(merged, parentEntries...)

	for i, e := range merged {
		parentMap[e.Key] = i
	}

	for _, e := range depEntries {
		if idx, exists := parentMap[e.Key]; exists {
			// Backfill comment from subchart if parent has none.
			if merged[idx].Comment == "" && e.Comment != "" {
				merged[idx].Comment = e.Comment
			}
		} else {
			merged = append(merged, e)
		}
	}

	return merged
}
