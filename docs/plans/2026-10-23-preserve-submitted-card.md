# Preserve submitted card presentation

Approved design: preserve original title, description, layout and options when closing a default question card. Disable Feishu controls and mark the selected answer. Slack controls without disabled support become readable equivalents in their original block positions. Retain the existing submission gate and plugin ownership. If no usable original card is available, skip destructive replacement and retain server-side protection.

Implementation: first reproduce lost button answer/flattened layout in tests; add platform-native JSON transformations with original metadata preserved; normalize button answers without treating action identifiers as answers; verify mocked updates, all platform tests and focused race tests. No release or push is included.
