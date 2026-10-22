# ACP remote steering implementation plan

Goal: forward busy-session steering to capable ACP peers without losing the existing next-turn fallback.

Design approved by the user's continuation of the proposed design: detect top-level initialize `_meta.steering.supported`, send `_session/steering` with host-owned `promptRequired` idle behavior, and leave the original prompt responsible for completion. Unsupported peers keep the existing Loop fallback. Avoid concurrent ordinary prompts and avoid cancelling/restarting the active turn.

Implementation:
1. Add failing tests for capability detection, wire payload/outcomes, in-flight delivery and fallback.
2. Store negotiated capability on each subprocess. Implement a bounded steering worker consuming the session control queue while a prompt runs, and join it before ending the turn.
3. Restore explicitly rejected messages through the control queue; surface ambiguous transport/protocol failures rather than blindly retrying potentially accepted input. Persist accepted user input.
4. Run focused tests, package regression tests and race checks for acpremote and loop.

Tech stack: Go, existing ACP Go SDK extension API. A short polling interval avoids changing the shared control interface; no worker is started for unsupported peers.
