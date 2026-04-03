package service

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

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
	}

	// No dependencies — identical to previous behavior.
	if len(ch.Dependencies()) == 0 {
		return parentVF, nil
	}

	// Collect subchart defaults (recursively) and merge with parent values.
	depEntries := collectDependencyValues(ch, "")
	merged := mergeParentOverDeps(parentVF.Entries, depEntries)

	slices.SortFunc(merged, func(a, b domain.ValuesEntry) int {
		return strings.Compare(string(a.Key), string(b.Key))
	})

	return &domain.ValuesFile{Source: "default", Entries: merged}, nil
}

func (s *ValuesService) LoadDefaultValues(ctx context.Context, chartPath string) (*FlatValues, error) {
	vf, err := s.ReadDefaultValues(ctx, chartPath)
	if err != nil {
		return nil, fmt.Errorf("load default values from %s: %w", chartPath, err)
	}

	return toFlatDTO(vf), nil
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
		// Silently ignore comment parse errors—if comments can't be extracted,
		// values are still valid, just without associated documentation.
		if comments, parseErr := parseComments(rawData); parseErr == nil {
			attachComments(vf, comments)
		}
	}

	return vf, nil
}

// ReadAndMergeCustomValues loads multiple values files and deep-merges them.
// Later files override earlier ones.
func (s *ValuesService) ReadAndMergeCustomValues(ctx context.Context, paths []string) (*domain.ValuesFile, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("read and merge custom values: %w", ctx.Err())
	}

	merged := make(map[string]any)

	for _, path := range paths {
		vals, err := chartutil.ReadValuesFile(path)
		if err != nil {
			return nil, fmt.Errorf("read values file %s: %w", path, err)
		}

		deepMerge(merged, vals)
	}

	vf := flattenValues("merged", merged)
	vf.RawValues = merged

	return vf, nil
}

// LoadAndMergeCustomValues loads multiple values files and deep-merges them.
// Later files override earlier ones.
func (s *ValuesService) LoadAndMergeCustomValues(ctx context.Context, paths []string) (*FlatValues, error) {
	merged, err := s.ReadAndMergeCustomValues(ctx, paths)
	if err != nil {
		return nil, fmt.Errorf("merge custom values: %w", err)
	}

	return toFlatDTO(merged), nil
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

	return toFlatDTO(vf), nil
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

func toFlatDTO(vf *domain.ValuesFile) *FlatValues {
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

	return &FlatValues{Entries: entries, RawValues: vf.RawValues}
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

// resolveDepPrefixes maps each dependency chart name to its effective prefix
// (alias if set, otherwise name) using the parent chart's metadata.
func resolveDepPrefixes(deps []*chart.Dependency) map[string]string {
	prefixes := make(map[string]string, len(deps))

	for _, d := range deps {
		if d.Alias != "" {
			prefixes[d.Name] = d.Alias
		} else {
			prefixes[d.Name] = d.Name
		}
	}

	return prefixes
}

// prefixEntries returns a new slice with each entry's Key prefixed.
func prefixEntries(entries []domain.ValuesEntry, prefix string) []domain.ValuesEntry {
	result := make([]domain.ValuesEntry, len(entries))

	for i, e := range entries {
		result[i] = domain.ValuesEntry{
			Key:     domain.FlatKey(buildKey(prefix, string(e.Key))),
			Value:   e.Value,
			Type:    e.Type,
			Comment: e.Comment,
		}
	}

	return result
}

// collectDependencyValues recursively collects flattened values from all
// chart dependencies, prefixing keys with the dependency name or alias.
func collectDependencyValues(ch *chart.Chart, parentPrefix string) []domain.ValuesEntry {
	deps := ch.Dependencies()
	if len(deps) == 0 {
		return nil
	}

	if ch.Metadata == nil {
		return nil
	}

	prefixMap := resolveDepPrefixes(ch.Metadata.Dependencies)

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

		all = append(all, prefixEntries(subVF.Entries, fullPrefix)...)
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
