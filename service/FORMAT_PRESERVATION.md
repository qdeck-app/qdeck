# Source format preservation on save

`gopkg.in/yaml.v3` is the only sane YAML library for Go, but it is a parse and re emit library, not a round trip library. Parsing the file into `yaml.Node`, then emitting that tree, normalizes everything:

1. Blank lines between mapping entries are dropped. yaml.v3 does not track inter node whitespace at all.
2. Inline comment column alignment collapses. The source `cpu: 100m        # cores` re emits as `cpu: 100m # cores`.
3. Folded scalars `>` get their logical value joined to a single line. yaml.v3 remembers what the value means but not where the user broke it across lines.
4. The merge key `<<:` gets an explicit `!!merge` tag prefix that the user never typed.
5. `!!set` collections lose the explicit key form. `? red` becomes `red:` because yaml.v3's emitter prefers the implicit key form.
6. `!!binary |` block scalars gain an extra blank line between the indicator and the body.
7. Per block indent variation is normalized to one file wide indent.
8. Inline comments on null leaves get dropped entirely (`key: ~ # explicit null` becomes `key: ~`).
9. Empty container placeholders (`key: []`, `key: {}`) sometimes get dropped from the override list and never re emitted.

A clean save through plain yaml.v3 would produce a file with all nine of these normalizations applied, even on a save with zero user edits.

## Architectural overview

`PatchSourceText` is the save pipeline entry point. The controller calls it with the source bytes captured at load time, the parsed `yaml.Node` tree, the override entries the user wants to persist, and the doc level comment bundle.

The function decides between three save paths in order of fidelity:

1. **Byte verbatim short circuit.** When the column has no edits at all and the source bytes are still available, the controller writes the original bytes back to disk without any parsing or emitting. Zero diff guaranteed. This sits one level above `PatchSourceText` in the controller because it skips the entire service layer.

2. **Leaf line splice.** For pure value edits to plain scalars, `planScalarSplice` walks the override entries, confirms every entry is safe to splice, and `SpliceScalarValues` rewrites just the value portion of each changed line. The rest of the file passes through byte for byte. One diff line per edit. This is the common case for any save where the user simply changed a value or two.

3. **Encoder fallback with restoration.** When any entry blocks the splice (new keys, removed keys, comment edits, quoted or block scalar edits, alias target edits, container shape changes, nullify markers, or doc level comment edits) the pipeline falls through to the full `PatchNodeTree` encoder. Its output is then post processed by `PreserveSourceFormatting` to claw back as many bytes of source formatting as possible. The output is not byte identical with source for unchanged keys, but it is close.

## Path 1: byte verbatim short circuit

`onSaveColumnValues` checks four gates before doing anything else:

1. `col.ValuesModified` is false (no editor change has fired).
2. `col.CustomValues.RawBytes` is non empty (the source bytes were retained at load).
3. `col.CustomFilePaths` has exactly one element (not a Save As, not a multi file merge).
4. `col.MergedFileCount` is one or less (single file load).

If all four hold, `saveRawBytesToFile` writes `RawBytes` to the path without any further processing. `EncodeForFile` is deliberately skipped because the BOM and line endings are already baked into the source bytes. Routing through it would double the BOM and rewrite each already CRLF separator to `\r\r\n`.

The external mtime check still runs before the short circuit fires, so if the file changed on disk since load the user gets the overwrite dialog. The dialog confirm path stores the raw bytes string in `overwritePendingYAML` and falls through the normal save handler, which is acceptable because the user explicitly chose to overwrite.

## Path 2: leaf line splice

`SpliceScalarValues` rewrites the value portion of one or more plain scalar leaves in raw bytes, preserving every other byte. Blank lines, inline comment column alignment, scalar styles, anchors, aliases, per block indent: all survive byte identical because the rest of the file is never re emitted.

The viability check `planScalarSplice` runs first and gates the splice. Every entry in the override list must pass:

* The key must physically exist in the source tree. New keys cannot be spliced because they have no source position to insert at.
* The target node must be a plain scalar with `Style == 0`. Quoted scalars need quote aware end detection that the simple plain scanner does not implement. Block scalars span multiple lines and need different handling.
* The entry's `HeadComment` and `LineComment` must match the tree's effective comments. A mismatch means a comment edit, which the splice cannot apply.
* The leaf must not be reached through an alias. Editing a value through an alias would silently change the anchor source's value, changing every other use site too.
* The entry must not be a container placeholder or a nullify marker. Container shape changes need the encoder.
* The new value must be plain safe, meaning yaml.v3 would not auto quote it. The check round trips the value through a single node encode and compares.
* `docsMatchSource` must hold: the doc level comment bundle the controller passed must match what `parseOrphanComments` extracts from raw. A mismatch means a banner, trailer, foot, or section head was edited, and the splice has no path to write those changes.

If any entry fails, splice rejects, all or nothing. The hot path takes the boolean return and discards any diagnostic detail. The companion `describeSpliceBlocker` runs the same logic but formats a human readable description of the first failing check. It is used only by `FirstSpliceBlockerForTest` for cross package test diagnostics; the production save never pays the formatting cost.

The per leaf splice in `spliceScalarValue` finds the value node, reads its `Line` and `Column`, locates the source line, computes the value end with `plainScalarLineEnd` (which scans forward until a ` #` inline comment marker or end of line), and replaces only the value substring. The leading indent, the key, the colon, the spacing between value and comment, and the comment itself are all preserved verbatim.

## Path 3: encoder fallback with restoration

When splice rejects, `PatchNodeTree` runs to produce a full re emitted YAML text. That text gets handed to `PreserveSourceFormatting`, which runs four restoration passes in order.

Pass order matters. The first pass replaces multi line spans, which invalidates any pre computed line maps. Subsequent passes re parse the encoded output internally so they see the post substitution structure.

### Strip the spurious `!!merge` tag

yaml.v3's emitter writes `!!merge <<: *anchor` where the canonical form (and every hand authored source we have seen) writes `<<: *anchor`. A `bytes.ReplaceAll` drops the tag. The parser handles both forms identically, so the user perceives no behavior change, just the source spelling they originally had.

### Byte range substitution for unchanged block subtrees

`substituteBlockScalarRanges` handles two classes of nodes that the encoder fundamentally cannot round trip:

1. **Block scalars** (literal `|`, folded `>`, plus chomp and tag variants like `!!binary |`). The encoder rejoins folded scalars, adds an extra blank between `!!binary |` and its body, and can shift the leading newline of any block scalar by one byte depending on the chomp indicator.

2. **Tagged collections** (`!!set`, `!!omap`, user defined tags). The `!!set` form is the headline example: source `? red` becomes encoder `red:` because yaml.v3 always prefers the implicit key form for null valued mapping entries.

For each such node, the pass computes its source byte range using the key node line and the indent based end detection in `refineBlockScalarEnd`. It then locates the same flat key in the encoder output, computes the encoded byte range the same way, and substitutes source bytes verbatim if the node content is structurally unchanged. Scalars compare by Value modulo leading and trailing newlines; non scalars compare by recursive structural equality through `nodesStructurallyEqual`.

A structural mismatch (the user actually edited the block content) disables substitution for that node, so the user's edit survives.

The Style bit field needs careful handling. `LiteralStyle` is bit 3, `TaggedStyle` is bit 0, so a `!!binary |` node carries `Style == 9` (bit 0 plus bit 3). Equality checks on Style would miss this, so `isBlockScalarNode` checks via bitmask AND, and `isTaggedCollectionNode` checks Tag against the default tag spellings (`!!map`, `!!seq`, and their URI forms) and treats anything else as substitutable.

### Line for line substitution from source

`substituteUnchangedLinesFromSource` is the bulk fidelity recovery pass. For each line in encoded that maps to a flat key (via `lineToFlatKey`), it finds the same flat key's line in source. If the two lines describe the same key, value, and inline comment modulo whitespace (via `linesSemanticallyMatch`), it substitutes the source line verbatim into the encoded output.

This is what recovers inline comment column alignment for every unchanged leaf in the file. The encoder collapses `cpu: 100m        # cores` to `cpu: 100m # cores`; the substitution restores the wide spacing from source.

The equivalence check has one asymmetric loosening: if the source line has an inline comment but the encoded line lacks one, substitute anyway. This recovers the case where yaml.v3 drops inline comments on null leaves (the encoder emits `usePasswordFiles: ~` for source `usePasswordFiles: ~ # explicit null via tilde`). The loosening is intentionally one directional: an encoder line with content the source lacks is not overwritten, because that content might be an edit.

Block scalar key lines are explicitly excluded from substitution. The key line `configuration: |` would match, but the body lines below it do not have per line flat keys, so substituting just the key line would leave the body in encoder form which is what Pass B is for.

### Insert source lines for keys missing from encoded

`insertMissingSourceLines` recovers single line entries that exist in source but were dropped from the encoder output. The headline case is empty container placeholders like `disableCommands: []`: the flattener emits a row for them so the UI can render them, but `collectOverrides` and `PatchNodeTree`'s container no op path together drop them somewhere between override list construction and encoder emission.

For each source key that is in `insertableSingleLineKeys` but missing from the encoded `lineToFlatKey` map, the pass anchors on the previous source key that does exist in encoded, and emits the missing source line immediately after that anchor's encoded position. Anchoring on the previous key rather than the next key is critical: the next key approach landed insertions across section boundaries (an empty container at the tail of `auth:` would slip into `commonLabels:`'s first child position).

Each missing key inserts exactly its own source line. Head comments and blank lines immediately above the missing key are not re attached. That trade off keeps adjacent missing keys from overlapping their source ranges and duplicating lines, which an earlier attempt at full context inclusion did do.

Flow style mappings and sequences are explicitly skipped during `insertableSingleLineKeys` traversal because they put all entries on one source line. `lineToFlatKey` only records one key per line, so the per entry insertion logic would consider the others missing from encoded and try to insert them, producing nonsense.

### Restore blank lines

`restoreBlankLines` is the simplest pass conceptually but the most fragile in practice. It scans source for blank line runs, attributes each run to the previous keyed line as its `afterKey` anchor, and re inserts blanks after the same anchor's line in the encoded output.

Two refinements keep it from misattributing:

1. **Region awareness via `blockScalarRegions`.** Blanks inside block scalar bodies and tagged collection bodies are part of the value, not sibling separators. The block scalar and tagged collection regions are computed via `refineBlockScalarEnd` (indent based, YAML spec correct) and `collectBlankRuns` skips blanks inside any region. Tagged collections specifically must not use the sibling heuristic cap for region computation, because their child node lines appear after the collection's line and would mis cap the region at the first child. Block scalars are safe to use the sibling cap because their child node has no Line of its own.

2. **Key adjacency tracking.** A blank only generates a run when no non blank non key line (typically a top level comment block attached as a HeadComment to some future node) intervenes between the last keyed line and the blank. Without this check, the blank between a top level comment wall and the next major section would get attributed to whichever leaf was most recently keyed, and `restoreBlankLines` would land the inserted blank in the wrong place (often inside a previous block scalar's body).

The trade off: a user pattern of `# comment for X` followed by a blank followed by `X:` would lose the blank because the comment line breaks adjacency. This pattern is uncommon enough that we accept it.

## State preservation across editor reparses

### `RawBytes` carry forward

Every keystroke in any cell editor fires `commitOverrideUpdate`, which rebuilds the entire column's YAML via `OverridesToYAML` and feeds the result to `ParseEditorContent`. `ParseEditorContent` returns a fresh `*FlatValues` from the rebuilt text. The controller's `pollEditorParse` then replaces `col.CustomValues` with that fresh value.

`ParseEditorContent` has no canonical source file (it parses arbitrary editor text), so `RawBytes` on its result is always nil. Without explicit carry forward, the first keystroke wipes `col.CustomValues.RawBytes`, and from that point on every save falls through to the encoder path because `PatchSourceText` short circuits the splice when raw is empty.

The fix: in `pollEditorParse`, after the new `FlatValues` is built but before it replaces `col.CustomValues`, copy `RawBytes` from the outgoing value to the incoming one. The source bytes never change as the user edits, so they survive any number of reparses.

### Per entry `Comment` carry forward

The same reparse cycle drops per entry `Comment` fields. The flattener's `attachComments` populates those at load time by parsing the raw bytes with `parseComments`, but `ParseEditorContent` does not run `parseComments` (the editor text is not the source file, and a parse on it might attribute differently).

When `entry.Comment` is empty for a leaf that had a comment in the source, `loadFormForEditor` returns just the value without any `# ` prefix. The editor still holds its original text (with the `# ` prefix from load time), so the equality check in `collectOverrides`'s fast path fails. The slow path then runs, and `StripYAMLComments` greedily eats every line that starts with `#` at the start of the editor text, treating them all as head comments.

For a single line value this is harmless. For a multi line block scalar whose body legitimately contains `#` lines (like `configuration: |` carrying a redis config snippet), this is catastrophic: every leading body line that starts with `#` gets stripped from the value and promoted to the entry's `HeadComment`. PatchNodeTree then writes the corrupted value into the in memory tree and the encoder emits a file where the `#` content lines now appear above the block scalar key instead of inside the body.

The fix: in `pollEditorParse`, build a `key to Comment` map from the outgoing `col.CustomValues.Entries` and apply it to the incoming entries. Comments come from the source file, not the edited editor text, so the load time values remain authoritative.

### `LoadAndMergeCustomValues` for single file loads

The controller's load path always goes through `LoadAndMergeCustomValues`, even for single file opens. `ReadAndMergeCustomValues` intentionally leaves `RawBytes` nil because a multi file merge has no canonical source bytes.

For single file loads this disabled the splice path globally. Service level tests passed because they called `ReadCustomValues` directly which does retain `RawBytes`; the production path diverged silently.

The fix: in `ReadAndMergeCustomValues`, when the input path list has exactly one entry, read and retain that file's bytes on `vf.RawBytes`. Multi file merges still leave it nil so the encoder fallback runs.

The e2e test now exercises `LoadAndMergeCustomValues` specifically to lock this in.

## Tagged scalar precision in `ParseEditorContent`

`ParseEditorContent` originally went through `chartutil.ReadValues` which routes through `sigs.k8s.io/yaml`. That library loses precision on tagged scalars: `!!binary` is decoded silently, `!!int "9007199254740993"` rounds to float64, large integers lose their literal text, and tagged set members are normalized.

After the keystroke reparse, `col.CustomValues.NodeTree` is patched back from the load time tree (it carries source line positions and the literal scalar text yaml.v3 captured). But the per entry `Value` fields come from `ParseEditorContent`, which used the lossy decoder.

For every tagged scalar in the file (the redis fixture has `!!binary`, `!!int "3"`, `!!str "42"`, several others), the entry Value disagreed with what `findEffectiveScalar` returned for the same path in the tree. `planScalarSplice` saw this disagreement as "value changed" and rejected the splice on every save.

The fix: in `ParseYAMLText` (the shared parser under `ParseEditorContent`), re parse the same text through yaml.v3 directly, plant the result on `vf.NodeTree`, and run `rewriteValuesFromNodeTree` to swap each entry's Value with the literal scalar text from the new tree. The chartutil path keeps running for compatibility with the rest of the system, but the canonical scalar text comes from yaml.v3 and matches what the load time tree carries.

## The bool switch add scenario

A bool typed cell in the UI shows a switch widget. When the user toggles a switch on a chart only key (a key that exists in the chart defaults but the user has not yet overridden), `layoutBoolSwitchCell` writes the chart's default value into the override editor. This is intentional UX: the user sees the inherited value and can toggle without first typing anything.

The save side effect: this looks like an add operation. The flat key goes through `collectOverrides` as a new override; the encoder inserts it into the tree at its conventional position; the encoder output has lines for it that source does not have.

For splice this is fatal: `planScalarSplice` rejects on `findEffectiveScalar hasValue == false` for the new key. The encoder fallback runs.

The four restoration passes in `PreserveSourceFormatting` were designed to make this fallback acceptable. The encoder writes the new key in its emitted position; the line for line substitution restores every other leaf to source form; the block range substitution restores any unchanged tagged or block scalars; the blank line restore re inserts source blanks. The user's resulting diff is the new key (two or three lines, depending on how nested the parent is) plus a small fixed residual of cosmetic differences (one head comment we cannot easily re attach, one trailing blank, one sequence comment relocation by one position).

## Block scalar content as comment bug

yaml.v3's parser treats `#` at a key column indent as a YAML comment, regardless of whether it lexically sits inside a block scalar body. For the redis fixture's `master.configuration: |` block, the source line `    # Pasted verbatim from redis.conf.` at column 4 is parsed as a HeadComment of the next mapping pair rather than as block scalar content.

On clean re emit, the encoder writes the comment above `configuration:` and the block body no longer starts with that line. Subsequent saves preserve the corrupted form because parsing the corrupted file gives a HeadComment that matches the prior emission.

This is the only sticky corruption in the pipeline. Once a file has been saved through a buggy intermediate, the comment placement does not self heal. The fix path requires reverting the file from git. The byte range substitution in Pass B specifically guards against introducing this corruption from a clean input: the block scalar's source bytes (including the `#` content line) are spliced verbatim, bypassing the parser's misattribution entirely.

## BOM and CRLF round trip

`EncodeForFile` prepends the UTF 8 BOM when the source file had one, and rewrites `\n` to `\r\n` when the source used CRLF. It is meant to run on yaml.v3 encoder output (which always emits UTF 8 LF).

When `PatchSourceText` returns raw source bytes verbatim (either through the no edit short circuit or through the splice path), those bytes already carry the BOM and CRLF. Routing them through `EncodeForFile` doubles the BOM and rewrites each `\r\n` to `\r\r\n`, producing a corrupted file.

The fix runs at two layers:

1. The no edit short circuit in the controller writes raw bytes via `SaveRawBytes`, which skips `EncodeForFile` entirely.

2. The splice path returns bytes that already carry the source's BOM and CRLF, so passing them through `EncodeForFile` with the column's source encoding labels would double encode. The `TestPatchSourceText_BOM_CRLF_RoundTripsThroughEncodeForFile` test in `yaml_format_test.go` pins this contract for both paths so any future regression at this seam fails loudly.

## Known limitations

The pipeline does not achieve byte identical round trip on every fixture. The remaining unintended diff lines on a clean save of the cornercase fixture with one added key are:

1. **One head comment** for `auth.extraPolicies` is lost. `insertMissingSourceLines` re emits the empty container line but not the head comment that sits above it in source. Restoring the comment requires tracking head comment line ranges per insertable key and including them in the insertion span without overlapping adjacent missing keys' ranges.

2. **One sequence comment** (`# - DEBUG` in `disableCommands`) shifts by one source position. yaml.v3 attaches the comment to a different node than where it lexically sits, and the encoder places it on the post attachment side of the blank line that separates it from the previous sequence item.

3. **One trailing blank line** appears at the end of the `cornerCases.taggedCollections` subtree. The encoder writes one, `restoreBlankLines` writes another, and the deduplication does not currently detect the overlap.

4. **Undo to original is not detected.** Typing a value, then typing the original back, leaves `ValuesModified` set to true. The encoder path runs even though the result would be source identical. A snapshot and diff at save time would catch this but the complexity is not warranted given the encoder fallback's restoration passes produce near identical output anyway.

5. **A genuine block scalar body edit can lose the substitution.** `substituteBlockScalarRanges` compares scalar values modulo leading and trailing newlines but otherwise demands exact match. A user edit that touches a `|` block body (rare through the table UI, which does not surface block scalars as edit cells) will disable substitution for that block, falling back to encoder output for the body.

6. **Doc level comment edits combined with value edits.** The splice rejects when `docsMatchSource` fails, but the encoder fallback writes doc comments through `applyDocFoots` and friends. The combination works, but the result is encoder formatted for the rest of the file too, so the value edit no longer benefits from the splice's byte fidelity.
