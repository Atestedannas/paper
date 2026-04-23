# DOCX Single-Template Closed-Loop Redesign

Date: 2026-04-23

## Goal

Redesign the current paper-format system into a single deterministic production path for:

- `DOCX`
- `single school`
- `single template`
- `automatic check`
- `automatic repair`
- `automatic re-verify`
- direct final file download

Target outcome:

- one main pipeline only
- no multi-engine fallback in production
- no preview file
- no "best effort" export
- browser downloads only the final verified `docx`

## Non-Goals

This redesign does not target:

- multi-template matching
- PDF primary support
- AI-first runtime repair
- vector database driven production decisions
- manual diff review as a core production step
- browser preview as a deliverable path

## Core Product Decision

The system will no longer "repair the student document in place".

The system will:

`parse student docx -> map content into template slots -> generate final document from template skeleton -> apply whitelisted OOXML patches -> verify independently -> download only if passed`

Template order is the only structural truth.
Student content may be reordered into template slots.
Unclear content must not be force-fitted into normal blocks.

## Why The Current System Fails

The current system has multiple overlapping chains in production:

- upload path mixes parsing, AI, repair, experiments, and generation
- service layer mixes checking, fixing, exporting, and engine fallback
- comparison chain exists as a parallel half-production path
- multiple repair engines can produce incompatible outputs

This causes four systemic failures:

1. there is no single source of truth for final formatting
2. check results cannot map reliably to executable repairs
3. engine switching makes output behavior unpredictable
4. the frontend interacts with a fragmented workflow instead of one state machine

## Final Technical Direction

The chosen technical direction is:

`Go + single-template precompile + template skeleton transplant + block-level content mapping + native OOXML targeted patching + independent verifier`

### Technology Decisions

- Service/API: `Go + Gin`
- Package read/write truth layer: `DOCX zip package + native OOXML XML manipulation`
- Read-only structural assistance: `unioffice`
- XML editing: DOM-style OOXML editing library or equivalent low-level XML package
- Runtime classification: deterministic Go rule engine
- State management: database-backed explicit job state machine
- Output file: verified final `docx` only

### Explicitly Rejected For Production Main Path

- Python + OOXML as the main repair engine
- Word COM as the main repair engine
- vector database / RAG in the runtime decision loop
- LLM as the final block mapper or final repair authority
- multi-engine runtime fallback chain
- generic style patching as the main repair model

## High-Level Architecture

The new production pipeline has exactly seven core modules:

1. `TemplateCompiler`
2. `PaperParser`
3. `BlockMapper`
4. `Transplanter`
5. `OOXMLPatchWriter`
6. `Verifier`
7. `LoopController`

### Main Flow

`uploaded -> parsed -> mapped -> transplanted -> patched -> verified_pass`

Failure path:

- if only whitelisted repairable issues exist: one more patch attempt
- otherwise: `manual_review`

No engine switch is allowed inside the loop.

## Module Design

### 1. TemplateCompiler

Responsibility:

- compile one official school template into a versioned template asset package
- transform a raw template file into a stable formatting contract

Input:

- official template `docx`

Output:

- compiled template package

The compiled package includes:

1. `manifest`
2. `skeleton`
3. `block_catalog`
4. `style_profiles`
5. `mapping_contract`
6. `verification_rules`
7. `patch_targets`

### 2. PaperParser

Responsibility:

- parse the student document only
- build a content structure tree
- never repair
- never apply production formatting

Output content categories:

- cover fields
- chinese abstract
- chinese keywords
- english abstract
- english keywords
- heading tree
- body paragraphs
- figure captions
- table captions
- body tables
- references
- acknowledgements
- abnormal blocks

### 3. BlockMapper

Responsibility:

- map parsed student content into template slots
- obey template order as the single structural truth

Mapping rule layers:

1. strong anchor rules
2. document state machine rules
3. local context rules
4. block capacity rules
5. abnormal fallback rules

Output:

- `block_bindings`
- `generated_blocks`
- `unmapped_blocks`
- `ambiguous_blocks`
- `verifier_hints`

### 4. Transplanter

Responsibility:

- generate the final paper from the compiled template skeleton
- transplant student content into template slots in template order

It does not:

- repair arbitrary structure
- reclassify content
- patch anything outside its generation scope

### 5. OOXMLPatchWriter

Responsibility:

- apply only small whitelisted post-generation corrections

Allowed categories:

- paragraph spacing, indent, alignment, pagination flags
- run font, size, bold, italic, color, superscript, subscript
- numbering references to template numbering system
- table cell text-bearing run/paragraph adjustments in whitelisted cells
- directory field settings and refresh-related metadata
- relationship patching for required media/hyperlinks
- whitelisted section-property completions

It must not become a second repair engine.

### 6. Verifier

Responsibility:

- verify output independently
- never reuse mapping or repair decisions as truth

Verification layers:

1. block-level
2. style-level
3. package-level
4. safety-level

### 7. LoopController

Responsibility:

- orchestrate the deterministic closed loop
- allow at most one patch retry after first verification

Automatic loop budget:

- first full generation
- one patch retry only

Anything beyond that goes to `manual_review`.

## Compiled Template Package

### Manifest

Fields:

- `template_id`
- `template_version`
- `school_id`
- `docx_hash`
- `compiled_at`
- `compiler_version`

### Block Catalog

Each template block should contain:

- `block_id`
- `kind`
- `slot_type`
- `order_index`
- `parent_block_id`
- `style_profile_id`
- `anchor`
- `source_region`
- `capacity`
- `required`
- `accepts`
- `patch_policy`
- `verify_policy`

Recommended `slot_type` values:

- `fixed`
- `single`
- `repeatable`
- `generated`

Recommended first-version block kinds:

- `cover_title`
- `cover_meta_label`
- `cover_meta_value`
- `cover_date`
- `abstract_cn_title`
- `abstract_cn_body`
- `keywords_cn`
- `abstract_en_title`
- `abstract_en_body`
- `keywords_en`
- `toc_title`
- `toc_entry_container`
- `heading_1`
- `heading_2`
- `heading_3`
- `body_para`
- `figure_caption`
- `table_caption`
- `reference_title`
- `reference_item`
- `ack_title`
- `ack_body`

### Style Profiles

Each style profile stores:

- paragraph spec
- run spec
- numbering spec
- table/cell spec where applicable
- section constraints
- forbidden mutations

### Mapping Contract

Defines:

- accepted input block types
- multiplicity
- whether split is allowed
- whether merge is allowed
- whether empty is allowed
- overflow handling
- ambiguous handling

### Verification Rules

Defines:

- existence constraints
- count constraints
- ordering constraints
- style constraints
- anchor constraints
- safety constraints

### Patch Targets

Defines the only OOXML targets allowed for post-generation patching.

## Mapping Rules

### Structural Truth

The final document order is always the template order.

The student document does not control final block order.

### Hard Rules

- cover content maps by explicit fields, not free paragraphs
- TOC is generated from final heading tree, not inherited from student TOC
- heading numbering uses template numbering only
- headers and footers come from template only
- section properties come from template only
- reference items are isolated from body paragraphs
- unrecognized content goes to abnormal buckets, never to normal slots

### Abnormal Buckets

The mapper must emit:

- `unmapped_blocks`
- `ambiguous_blocks`
- `overflow_blocks`

These are first-class outputs, not debug leftovers.

## Generation Rules

### General Rule

The template skeleton is the output base.
The student document is the content source.

### Cover

- never rebuild cover table geometry
- replace text only in designated slots or cells
- preserve template table layout, merge, width, border, and positioning

### Abstracts and Keywords

- use template-owned shell paragraphs
- transplant content only
- preserve template label formatting

### Headings

- build heading tree from parsed student content
- write heading text into template heading prototypes
- use template numbering system only

### Body Paragraphs

Body is transplanted as content atoms, not loose text only.

Recommended content atoms:

- `text_run`
- `inline_image`
- `inline_formula`
- `footnote_ref`
- `hyperlink`
- `inline_break`

### Captions

- figure captions and table captions are isolated block types
- never silently mix them into body paragraphs

### Tables

- preserve student table content
- allow only whitelisted table text formatting updates
- do not rebuild geometry in production v1

### References

- each reference item clones the template reference prototype
- formatting is driven by the template profile, not student formatting

### TOC

- template owns the TOC container
- service-side TOC page numbers do not need to be exact in v1
- field structure must be correct and refreshable when opened

## PatchWriter Boundaries

### Allowed

- `w:t`
- `w:r`
- `w:rPr`
- `w:pPr`
- `w:numPr`
- `w:br`
- `w:tab`
- whitelisted `w:tc` text-bearing descendants
- required relation entries
- whitelisted TOC metadata

### Forbidden

- replacing the production repair engine
- rewriting template block order
- modifying template cover table geometry
- rebuilding `styles.xml` as a second style system
- open-ended section rewriting
- arbitrary reclassification after transplant

## Verifier Design

### Verification Layers

1. `Block-level`
2. `Style-level`
3. `Package-level`
4. `Safety-level`

### Verify Result

Recommended result structure:

- `passed`
- `score`
- `fatal_issues`
- `repairable_issues`
- `warnings`
- `unmapped_blocks`
- `ambiguous_blocks`
- `output_hash`

### Fatal Issue Examples

- required template block missing
- ambiguous single-value slot
- heading tree invalid
- references section boundary invalid
- forbidden OOXML mutation detected
- second verification still fails

### Repairable Issue Examples

- whitelisted paragraph spacing mismatch
- numbering reference mismatch
- TOC field metadata patchable
- whitelisted run formatting mismatch

## Loop Policy

Closed loop policy:

- one full generation
- one patch retry maximum
- no runtime engine switching
- no open-ended recursive retries

State transitions:

- `uploaded`
- `template_compiled`
- `parsed`
- `mapped`
- `transplanted`
- `patched`
- `verified_pass`
- `verified_fail`
- `manual_review`

## Delivery Policy

Only one output file is allowed in production:

- final verified `docx`

Rules:

- no preview file
- no best-effort file
- no manual-review export file
- browser downloads only the final corrected file

If result is `verified_pass`:

- return download URL

If result is `verified_fail` or `manual_review`:

- return issues only
- do not expose any output file for download

## API Design

Production v2 API should be reduced to one workflow-oriented set:

### `POST /api/v2/templates/compile`

Compiles one template.

### `POST /api/v2/papers`

Uploads one student paper and binds it to a compiled template.

Input:

- `paper.docx`
- `template_id`

Output:

- `job_id`

### `POST /api/v2/jobs/:job_id/run`

Starts the deterministic closed loop.

Internal flow:

- parse
- map
- transplant
- patch
- verify

### `GET /api/v2/jobs/:job_id`

Returns job status and issues.

### `GET /api/v2/jobs/:job_id/download`

Downloads only the final verified paper.
Available only for `verified_pass`.

## Frontend Workflow

Frontend should become a single straight-through workflow:

1. select template
2. upload paper
3. create job
4. run job
5. poll status
6. direct final file download if passed

The frontend should stop exposing:

- multiple repair paths
- manual engine selection
- production diff-apply loop
- preview-first workflow

## Legacy Code Strategy

The current production path must be simplified aggressively.

### Must Exit Production Main Path

- upload-time AI/experimental mixed processing
- multi-engine repair fallback chain
- comparison service as a production parallel chain
- generic style patching as the main repair strategy
- vector DB / RAG / LLM runtime dependency in the production loop

### Can Be Archived Or Reused Selectively

- low-level OOXML helpers
- existing template block detection utilities
- useful strict formatting logic that fits the new module boundaries
- some read-only parsing helpers

### Codebase Direction

Recommended module layout:

- `internal/core/templatecompile`
- `internal/core/paperparse`
- `internal/core/blockmap`
- `internal/core/transplant`
- `internal/core/ooxmlpatch`
- `internal/core/verify`
- `internal/core/workflow`

Handlers should become thin request/response adapters.
Workflow orchestration should move into dedicated core services.

## Migration Plan

### Phase 1: Freeze and isolate

- define new v2 workflow package boundaries
- stop adding new logic to the old mixed upload/service path
- mark old multi-engine path as legacy

### Phase 2: Build template compiler

- compile a single official school template
- produce versioned template package
- define block catalog and style profiles

### Phase 3: Build parser and mapper

- parse student content tree
- implement deterministic mapper
- emit abnormal buckets explicitly

### Phase 4: Build transplanter and patch writer

- generate final document from template skeleton
- add only whitelisted OOXML patches

### Phase 5: Build verifier and loop controller

- independent verification
- one retry maximum
- stable pass/fail/manual_review behavior

### Phase 6: Replace frontend path

- switch to single job workflow
- remove preview-first and multi-path behavior

### Phase 7: Remove legacy production chain

- disconnect old handlers/services from production routes
- archive or delete dead code after cutover

## Success Criteria

The redesign is considered successful when:

- one template can be compiled once and reused deterministically
- one student paper produces one final output path only
- the pipeline does not switch repair engines at runtime
- template order is always preserved
- required blocks are never silently skipped
- failure is explicit and safe
- only `verified_pass` files are downloadable

## Final Decision Summary

The new system is a deterministic template-driven document assembly system, not a generic document repair system.

Its truths are:

- content truth: student paper
- format truth: compiled official template
- generation truth: template skeleton transplant
- patch truth: whitelist only
- quality truth: independent verifier

That is the architectural basis for reaching high implementation reliability on the scoped target:

`DOCX + single school + single template + automatic check + automatic repair + automatic re-verify + direct final file download`
