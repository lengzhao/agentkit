# ACP remote model display

**Goal:** Report the remote model during use through existing thinking output; omit missing models.

**Design:** Read ACP configOptions model selections on session creation, restoration, and notifications. Keep current model per ACP session in the subprocess. At each turn, and on changes, emit a thinking delta containing [model: model-id]. Reuse the existing stream lifecycle and thinking buffer so platforms render through their current mechanisms. No new events, platform changes, configuration, or model switching.

## Implementation
1. Test model config extraction, notification updates, session isolation, restoration, and repeated prompts.
2. Capture configOptions from ACP and emit the model through the existing thinking channel.
3. Run the ACP remote tests and existing platform regression tests.
