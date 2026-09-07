# Mobile Copy

Android and iOS share concise product chrome. Screen titles, form labels, clear
actions, server identity, meaningful state, and recovery errors remain visible.
Familiar tool icons retain accessible names. Optional descriptions do not reserve
empty text rows. Inventory and loading states name their state without explaining
implementation details or repeating nearby controls.

Conversation messages, task results, logs, code, patches, filenames, and installed
Skill/Plugin descriptions are content, not chrome. Copy cleanup does not rewrite
or truncate them. Credential and privacy boundaries, destructive consequences,
and unsupported-preview states remain explicit.

Stats preserves exact model identity, price provenance, refresh state, and missing
price reasons. An unknown price is not zero, and a reference estimate is not an
actual billed charge.

A delegated Session whose execution evidence is lost requires review. It is not
presented as Working or completed merely because a prior card said so. Canonical
loss replaces the Work lineage card even without a provider result; reviewing
and resolved states follow canonical lifecycle state. Input is never replayed by
this presentation repair.

Regression coverage lives outside runtime routes in `app/services/appChromeCopy.test.ts`,
`app/components/brain/brainWorkEventPresentation.test.ts`, and
`daemon/brain/turn_loss_presentation_test.go`.
