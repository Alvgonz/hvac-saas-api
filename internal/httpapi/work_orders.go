package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type WorkOrdersHandler struct {
	DB *pgxpool.Pool
}

// =========================
// POST /work-orders
// =========================

type createWorkOrderRequest struct {
	CustomerID  string  `json:"customer_id"`
	SiteID      string  `json:"site_id"`
	AssetID     string  `json:"asset_id"`
	Type        string  `json:"type"`     // preventive|corrective|inspection
	Priority    string  `json:"priority"` // low|medium|high|critical
	Title       string  `json:"title"`
	Description *string `json:"description,omitempty"`
	Notes       *string `json:"notes,omitempty"`
	AssignedTo  *string `json:"assigned_to,omitempty"`
	// scheduled_at lo dejamos para después (cuando definamos ISO parse y validación)
}

type createWorkOrderResponse struct {
	ID string `json:"id"`
}

func (h *WorkOrdersHandler) Create(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// permisos
	if claims.Role != "admin" && claims.Role != "dispatcher" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var req createWorkOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	req.CustomerID = strings.TrimSpace(req.CustomerID)
	req.SiteID = strings.TrimSpace(req.SiteID)
	req.AssetID = strings.TrimSpace(req.AssetID)
	req.Type = strings.TrimSpace(req.Type)
	req.Priority = strings.TrimSpace(req.Priority)
	req.Title = strings.TrimSpace(req.Title)

	if req.CustomerID == "" || req.SiteID == "" || req.AssetID == "" || req.Title == "" {
		http.Error(w, "missing required fields", http.StatusBadRequest)
		return
	}

	if req.Type == "" {
		req.Type = "corrective"
	}
	if req.Priority == "" {
		req.Priority = "medium"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Validación fuerte: customer/site/asset deben existir y pertenecer al mismo service_provider (tenant)
	var ok bool
	err := h.DB.QueryRow(ctx, `
		SELECT EXISTS (
		  SELECT 1
		  FROM customer c
		  JOIN site s ON s.id = $2 AND s.customer_id = c.id
		  JOIN asset a ON a.id = $3 AND a.customer_id = c.id AND a.site_id = s.id
		  WHERE c.id = $1 AND c.service_provider_id = $4
		)
	`, req.CustomerID, req.SiteID, req.AssetID, claims.ServiceProvider).Scan(&ok)
	if err != nil || !ok {
		http.Error(w, "invalid customer/site/asset for this provider", http.StatusBadRequest)
		return
	}

	var id string
	err = h.DB.QueryRow(ctx, `
		INSERT INTO work_order (
		  service_provider_id,
		  customer_id, site_id, asset_id,
		  type, priority, status,
		  title, description, notes,
		  created_by, assigned_to
		) VALUES (
		  $1,
		  $2, $3, $4,
		  $5, $6, 'open',
		  $7, $8, $9,
		  $10, $11
		)
		RETURNING id
	`,
		claims.ServiceProvider,
		req.CustomerID, req.SiteID, req.AssetID,
		req.Type, req.Priority,
		req.Title, req.Description, req.Notes,
		claims.UserID, req.AssignedTo,
	).Scan(&id)

	if err != nil {
		http.Error(w, "could not create work order", http.StatusInternalServerError)
		return
	}

	WriteJSON(w, http.StatusCreated, createWorkOrderResponse{ID: id})
}

// =========================
// GET /work-orders
// =========================

type workOrderItem struct {
	ID          string     `json:"id"`
	CustomerID  string     `json:"customer_id"`
	SiteID      string     `json:"site_id"`
	AssetID     string     `json:"asset_id"`
	Type        string     `json:"type"`
	Priority    string     `json:"priority"`
	Status      string     `json:"status"`
	Title       string     `json:"title"`
	AssignedTo  *string    `json:"assigned_to,omitempty"`
	CreatedBy   string     `json:"created_by"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

func (h *WorkOrdersHandler) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	q := r.URL.Query()
	customerID := strings.TrimSpace(q.Get("customer_id"))
	status := strings.TrimSpace(q.Get("status"))
	limit := 50
	offset := 0

	// Reglas por rol:
	// - admin/dispatcher: ven todo del provider (y pueden filtrar por customer_id)
	// - technician: solo asignadas a él
	// - client: solo su customer_id (del token)
	if claims.Role == "client" {
		if claims.CustomerID == nil {
			http.Error(w, "client has no customer_id", http.StatusForbidden)
			return
		}
		customerID = *claims.CustomerID
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	args := []any{claims.ServiceProvider}
	where := `WHERE service_provider_id = $1`
	argn := 2

	// customer filter (no aplica para technician; a technician le filtramos por assigned_to)
	if customerID != "" && claims.Role != "technician" {
		where += " AND customer_id = $" + itoa(argn)
		args = append(args, customerID)
		argn++
	}

	if status != "" {
		where += " AND status = $" + itoa(argn)
		args = append(args, status)
		argn++
	}

	if claims.Role == "technician" {
		where += " AND assigned_to = $" + itoa(argn)
		args = append(args, claims.UserID)
		argn++
	}

	rows, err := h.DB.Query(ctx, `
		SELECT
		  id, customer_id, site_id, asset_id,
		  type, priority, status, title,
		  assigned_to, created_by,
		  completed_at, created_at
		FROM work_order
		`+where+`
		ORDER BY created_at DESC
		LIMIT `+itoa(limit)+` OFFSET `+itoa(offset)+`
	`, args...)
	if err != nil {
		http.Error(w, "could not list work orders", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	out := make([]workOrderItem, 0, 50)
	for rows.Next() {
		var it workOrderItem
		if err := rows.Scan(
			&it.ID, &it.CustomerID, &it.SiteID, &it.AssetID,
			&it.Type, &it.Priority, &it.Status, &it.Title,
			&it.AssignedTo, &it.CreatedBy,
			&it.CompletedAt, &it.CreatedAt,
		); err != nil {
			http.Error(w, "scan error", http.StatusInternalServerError)
			return
		}
		out = append(out, it)
	}

	WriteJSON(w, http.StatusOK, out)
}

// helper pequeño para no depender de strconv (si prefieres strconv, lo cambiamos)
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return sign + string(b[i:])
}

type completeWorkOrderRequest struct {
	Notes *string `json:"notes,omitempty"`
}

func (h *WorkOrdersHandler) Complete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Esperamos path: /work-orders/{id}/complete
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) != 3 || parts[0] != "work-orders" || parts[2] != "complete" {
		http.NotFound(w, r)
		return
	}
	workOrderID := strings.TrimSpace(parts[1])
	if workOrderID == "" {
		http.Error(w, "missing work order id", http.StatusBadRequest)
		return
	}

	if claims.Role == "client" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var req completeWorkOrderRequest
	_ = json.NewDecoder(r.Body).Decode(&req) // body opcional

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Reglas:
	// - admin/dispatcher: puede completar cualquier WO del provider
	// - technician: solo si assigned_to = él
	var tag string
	var args []any

	if claims.Role == "technician" {
		tag = `
			UPDATE work_order
			SET status = 'completed',
			    completed_at = now(),
			    notes = COALESCE($3, notes),
			    updated_at = now()
			WHERE id = $1
			  AND service_provider_id = $2
			  AND assigned_to = $4
			  AND status != 'cancelled'
			RETURNING id
		`
		args = []any{workOrderID, claims.ServiceProvider, req.Notes, claims.UserID}
	} else {
		// admin/dispatcher
		tag = `
			UPDATE work_order
			SET status = 'completed',
			    completed_at = now(),
			    notes = COALESCE($3, notes),
			    updated_at = now()
			WHERE id = $1
			  AND service_provider_id = $2
			  AND status != 'cancelled'
			RETURNING id
		`
		args = []any{workOrderID, claims.ServiceProvider, req.Notes}
	}

	var id string
	err := h.DB.QueryRow(ctx, tag, args...).Scan(&id)
	if err != nil {
		http.Error(w, "work order not found or not allowed", http.StatusNotFound)
		return
	}

	WriteJSON(w, http.StatusOK, map[string]string{"id": id, "status": "completed"})
}

