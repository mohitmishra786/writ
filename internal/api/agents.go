package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/mohitmishra786/paypal-ai-hackathon/internal/mesh"
	"github.com/mohitmishra786/paypal-ai-hackathon/internal/procure"
)

// Agents exposes the procurement agent, supplier directory and inventory.
type Agents struct {
	Procure   *procure.Agent
	Suppliers []*mesh.Supplier
}

func (s *Server) MountAgents(a Agents) {
	a2a := http.NewServeMux()
	for _, sup := range a.Suppliers {
		sup.Mount(a2a, s.cfg.PublicBaseURL)
	}
	s.Mount("/a2a/", a2a)
	s.Mount("GET /v1/suppliers", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		out := make([]map[string]any, 0, len(a.Suppliers))
		for _, sup := range a.Suppliers {
			base := strings.TrimRight(s.cfg.PublicBaseURL, "/") + "/a2a/" + sup.Slug
			out = append(out, map[string]any{"ref": sup.Ref, "name": sup.Name, "receiver_email": sup.ReceiverEmail,
				"public_key_hex": sup.PublicKeyHex(), "agent_card": base + "/.well-known/agent-card.json", "listings": sup.Listings})
		}
		writeJSON(w, http.StatusOK, map[string]any{"suppliers": out})
	}))
	s.Mount("GET /v1/inventory", http.HandlerFunc(s.inventory))
	s.Mount("POST /v1/inventory/{sku}/break", s.limited(s.breakItem))
	s.Mount("POST /v1/inventory/{sku}/restock", s.limited(s.restockItem))
	if a.Procure == nil {
		return
	}
	s.Mount("POST /v1/procure/runs", s.limited(func(w http.ResponseWriter, r *http.Request) {
		var in procure.RunRequest
		if !decode(w, r, &in) {
			return
		}
		id, err := a.Procure.Start(in)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"run_id": id})
	}))
	s.Mount("GET /v1/procure/runs/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		run, err := a.Procure.GetRun(r.Context(), r.PathValue("id"))
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "not found")
			return
		}
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, run)
	}))
	s.Mount("POST /v1/procure/simulate-compromise", s.limited(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			MandateID string `json:"mandate_id"`
		}
		if !decode(w, r, &in) {
			return
		}
		d, err := a.Procure.SimulateCompromise(r.Context(), in.MandateID)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, d)
	}))
}

func (s *Server) inventory(w http.ResponseWriter, r *http.Request) {
	rows, err := s.st.Pool.Query(r.Context(), `SELECT sku, title, category, on_hand, par_level, updated_at FROM inventory ORDER BY sku`)
	if err != nil {
		s.fail(w, err)
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var sku, title, cat string
		var onHand, par int
		var at any
		if err := rows.Scan(&sku, &title, &cat, &onHand, &par, &at); err != nil {
			s.fail(w, err)
			return
		}
		out = append(out, map[string]any{"sku": sku, "title": title, "category": cat, "on_hand": onHand, "par_level": par,
			"qty_needed": max(par-onHand, 0), "updated_at": at})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// breakItem simulates breakage at the café (the stock signal for the demo).
func (s *Server) breakItem(w http.ResponseWriter, r *http.Request) {
	tag, err := s.st.Pool.Exec(r.Context(), `UPDATE inventory SET on_hand = greatest(on_hand - 1, 0), updated_at = now() WHERE sku = $1`, r.PathValue("sku"))
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "unknown sku")
		return
	}
	s.hub.Publish("inventory", map[string]string{"sku": r.PathValue("sku")})
	s.inventory(w, r)
}

func (s *Server) restockItem(w http.ResponseWriter, r *http.Request) {
	tag, err := s.st.Pool.Exec(r.Context(), `UPDATE inventory SET on_hand = par_level, updated_at = now() WHERE sku = $1`, r.PathValue("sku"))
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "unknown sku")
		return
	}
	s.hub.Publish("inventory", map[string]string{"sku": r.PathValue("sku")})
	s.inventory(w, r)
}
