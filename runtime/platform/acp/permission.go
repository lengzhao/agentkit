package acpplatform

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lengzhao/agentkit"
	"github.com/lengzhao/agentkit/cap/permission"
)

func (p *Platform) handlePermission(ctx context.Context, sess *sessionState, data json.RawMessage) error {
	var payload permission.RequestPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	if payload.ID == "" {
		return fmt.Errorf("permission/request missing id")
	}
	acpReq := permissionToACP(sess.acpSessionID, payload)
	resp, err := p.conn.RequestPermission(ctx, acpReq)
	if err != nil {
		return err
	}
	reply := acpPermissionToReply(payload.ID, resp)
	sessionID, _ := ctx.Value(agentkit.KeySessionID).(agentkit.SessionID)
	if deliverer, ok := ctx.Value(agentkit.KeySessionControl).(permissionDeliverer); ok && deliverer != nil {
		if !deliverer.DeliverPermissionReply(sessionID, reply) {
			return fmt.Errorf("permission reply not delivered")
		}
	}
	return nil
}
