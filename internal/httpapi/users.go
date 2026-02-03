package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type UsersHandler struct {
	DB *pgxpool.Pool
	// luego: Mailer, FrontendURL
}

type createUserRequest struct {
	CustomerID   *string `json:"customer_id,omitempty"` // requerido si role=client
	Fullname     string  `json:"fullname"`
	Email        string  `json:"email"`
	PhoneNumber  *string `json:"phone_number,omitempty"`
	Role         string  `json:"role"` // dispatcher|technician|client
}

type createUserResponse struct {
	ID         string `json:"id"`
	InviteSent bool   `json:"invite_sent"`
}

func (h *UsersHandler) Create(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Permisos:
	// admin: dispatcher/technician/client
	// dispatcher: solo technician
	if claims.Role != "admin" && claims.Role != "dispatcher" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	req.Fullname = strings.TrimSpace(req.Fullname)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.Role = strings.TrimSpace(req.Role)
	if req.PhoneNumber != nil {
		p := strings.TrimSpace(*req.PhoneNumber)
		req.PhoneNumber = &p
	}

	if req.Fullname == "" || req.Email == "" || req.Role == "" {
		http.Error(w, "missing required fields", http.StatusBadRequest)
		return
	}

	if req.Role != "dispatcher" && req.Role != "technician" && req.Role != "client" {
		http.Error(w, "invalid role", http.StatusBadRequest)
		return
	}

	// reglas por rol
	if claims.Role == "dispatcher" && req.Role != "technician" {
		http.Error(w, "dispatcher can only create technician", http.StatusForbidden)
		return
	}
	if req.Role == "client" {
		if req.CustomerID == nil || strings.TrimSpace(*req.CustomerID) == "" {
			http.Error(w, "customer_id is required for client user", http.StatusBadRequest)
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// si role=client, validamos que ese customer pertenece al mismo service_provider
	if req.Role == "client" {
		var ok bool
		err := h.DB.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM customer
				WHERE id = $1 AND service_provider_id = $2
			)
		`, strings.TrimSpace(*req.CustomerID), claims.ServiceProvider).Scan(&ok)
		if err != nil || !ok {
			http.Error(w, "invalid customer_id for this provider", http.StatusBadRequest)
			return
		}
	}

	// 1) Creamos usuario con password placeholder (NO usable)
	// Nota: como password es NOT NULL, ponemos algo que nunca será válido para login
	placeholder := "!INVITED_USER_NO_PASSWORD!"

	var userID string
	err := h.DB.QueryRow(ctx, `
		INSERT INTO "user" (
			service_provider_id, customer_id,
			fullname, email, password, phone_number,
			role, is_active
		) VALUES (
			$1, $2,
			$3, $4, $5, $6,
			$7, true
		)
		RETURNING id
	`,
		claims.ServiceProvider,
		req.CustomerID,
		req.Fullname, req.Email, placeholder, req.PhoneNumber,
		req.Role,
	).Scan(&userID)
	if err != nil {
		// si email duplicado por provider, caerá aquí
		http.Error(w, "could not create user", http.StatusBadRequest)
		return
	}

	// 2) Creamos invitación (token)
	plain, tokenHash, err := newInviteToken()
	if err != nil {
		http.Error(w, "could not create invite token", http.StatusInternalServerError)
		return
	}

	expiresAt := time.Now().Add(48 * time.Hour)

	_, err = h.DB.Exec(ctx, `
		INSERT INTO user_invitation (
			service_provider_id, user_id,
			token_hash, expires_at,
			created_by
		) VALUES ($1, $2, $3, $4, $5)
	`,
		claims.ServiceProvider, userID,
		tokenHash, expiresAt,
		claims.UserID,
	)
	if err != nil {
		http.Error(w, "could not save invitation", http.StatusInternalServerError)
		return
	}

	// 3) Enviar email (por ahora LOG)
	// Luego lo cambias por tu frontend: https://tuapp.com/set-password?token=...
	link := fmt.Sprintf("SET_PASSWORD_TOKEN=%s", plain)
	log.Printf("[INVITE] user=%s email=%s role=%s expires=%s %s",
		userID, req.Email, req.Role, expiresAt.Format(time.RFC3339), link)

	WriteJSON(w, http.StatusCreated, createUserResponse{
		ID:         userID,
		InviteSent: true,
	})
}