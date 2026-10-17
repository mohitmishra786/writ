package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// MountConsole adds the read endpoints the operator console needs and the
// AI proxy for the AG Studio agent.
func (s *Server) MountConsole() {
	s.Mount("GET /v1/settlements", http.HandlerFunc(s.settlements))
	s.Mount("GET /v1/procure/runs", http.HandlerFunc(s.listRuns))
	s.Mount("POST /v1/ai/responses", s.limited(s.aiResponses))
	s.Mount("GET /v1/ai/calls", http.HandlerFunc(s.aiCalls))
}

func (s *Server) settlements(w http.ResponseWriter, r *http.Request) {
	mandateID := r.URL.Query().Get("mandate_id")
	rows, err := s.st.Pool.Query(r.Context(), `SELECT o.id, o.mandate_id, o.intent_id, o.payee_ref, o.receiver_email, o.amount_minor, o.currency, o.state,
		coalesce(o.payout_item_id,''), coalesce(b.id,''), coalesce(b.sender_batch_id,''), coalesce(b.payout_batch_id,''), coalesce(b.status,''), o.created_at, o.updated_at
		FROM obligations o LEFT JOIN payout_batches b ON b.id = o.batch_id
		WHERE ($1 = '' OR o.mandate_id = $1) ORDER BY o.created_at DESC LIMIT 500`, mandateID)
	if err != nil {
		s.fail(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, mid, iid, payee, email, cur, state, item, batch, sender, payout, bstatus string
		var amt int64
		var created, updated time.Time
		if err := rows.Scan(&id, &mid, &iid, &payee, &email, &amt, &cur, &state, &item, &batch, &sender, &payout, &bstatus, &created, &updated); err != nil {
			s.fail(w, err)
			return
		}
		out = append(out, map[string]any{"id": id, "mandate_id": mid, "intent_id": iid, "payee_ref": payee, "receiver_email": email,
			"amount_minor": amt, "currency": cur, "state": state, "payout_item_id": item, "batch_id": batch, "sender_batch_id": sender,
			"payout_batch_id": payout, "batch_status": bstatus, "created_at": created, "updated_at": updated})
	}
	writeJSON(w, http.StatusOK, map[string]any{"obligations": out})
}

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	rows, err := s.st.Pool.Query(r.Context(), `SELECT id, mandate_id, sku, status, coalesce(final,''), steps, coalesce(intent_id,''), coalesce(error,''), created_at
		FROM agent_runs WHERE ($1 = '' OR mandate_id = $1) ORDER BY created_at DESC LIMIT 50`, r.URL.Query().Get("mandate_id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, mid, sku, status, final, intent, errMsg string
		var steps json.RawMessage
		var created time.Time
		if err := rows.Scan(&id, &mid, &sku, &status, &final, &steps, &intent, &errMsg, &created); err != nil {
			s.fail(w, err)
			return
		}
		out = append(out, map[string]any{"id": id, "mandate_id": mid, "sku": sku, "status": status, "final": final, "steps": steps,
			"intent_id": intent, "error": errMsg, "created_at": created})
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": out})
}

// decodeLoose decodes JSON allowing unknown fields (the Studio request is a
// superset of what is forwarded).
func decodeLoose(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if strings.TrimSpace(x) == v {
			return true
		}
	}
	return false
}
