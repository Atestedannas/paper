# DOCX RulePack Phase 2 Design

## Goal

Support advanced multi-school DOCX rules without school-specific code branches: odd/even headers, complex page numbering, configurable heading numbering, figure/table/formula caption rules, and multiple reference styles.

## Architecture

School and conference differences live in `templateprofile.RulePack`. The DOCX pipeline uses generic processors that read rule values and report violations. Only low-risk OOXML changes may be auto-repaired; ambiguous academic or semantic requirements fail verification and enter manual review.

## RulePack Additions

- `header_policy`: `none`, `template`, `odd_even`.
- `odd_header_text`, `even_header_text`, `header_line`: expected header strategy.
- `front_page_format`, `body_page_format`, `body_page_start`, `body_page_wrapper`: page-number format strategy.
- `heading_levels`: ordered numbering patterns such as `["第1章","1.1","1.1.1"]`, `["第一章","1.1","1.1.1"]`, `["一","(一)","1"]`.
- `figure_caption_position`, `table_caption_position`, `caption_style_key`: caption placement/style validation.
- `reference_style`: `gb_t_7714_sequence`, `author_year`, `sample_book_journal_basic`, `custom_school_basic`.

## Processors

- `HeaderFooterPolicyProcessor`: validates no-header/template/odd-even policies using section header references and header part text.
- `PageNumberingProcessor`: validates roman front matter, arabic body numbering, and dash-wrapped page numbering evidence in footer XML.
- `HeadingNumberingProcessor`: validates heading numbering patterns from `heading_levels`.
- `FigureTableCaptionProcessor`: validates continuous/chapter figure/table/formula numbering plus caption position around OOXML tables/drawings when detectable.
- `ReferenceStyleProcessor`: validates sequence references, author-year references, and sample school book/journal structure.

## Verification Policy

These processors are primarily check-only in this phase. They return repairable template-profile issues so the workflow can avoid false compliance. Later phases may add safe repair for header removal, `pgNumType`, and caption styles after render verification coverage is broader.

## Tests

Tests should cover each new rule family with minimal OOXML fixtures:

- Odd/even header references and header part text.
- Front roman/body arabic and NUAА-style dash page numbering.
- Three heading systems.
- Figure/table/formula continuous and chapter numbering.
- Figure below/table above position checks where neighboring paragraphs expose captions.
- Reference styles: sequence, author-year, and sample book/journal basics.
