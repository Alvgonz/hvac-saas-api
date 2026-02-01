package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/alvgonz/hvac-saas-api/internal/auth"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type AuthHandler struct {
	DB        *pgxpool.Pool
	JWTSecret []byte
}

type loginRequest struct {
	ServiceProviderID string `json:"service_provider_id"`
	Email             string `json:"email"`
	Password          string `json:"password"`
}

type loginResponse struct {
	Token string `json:"token"`
	User  struct {
		ID              string  `json:"id"`
		ServiceProvider string  `json:"service_provider_id"`
		CustomerID      *string `json:"customer_id,omitempty"`
		Fullname        string  `json:"fullname"`
		Email           string  `json:"email"`
		Role            string  `json:"role"`
	} `json:"user"`
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.ServiceProviderID = strings.TrimSpace(req.ServiceProviderID)

	if req.Email == "" || req.Password == "" || req.ServiceProviderID == "" {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	var (
		userID     string
		spid       string
		customerID *string
		fullname   string
		email      string
		role       string
		isActive   bool
		hash       string
	)

	err := h.DB.QueryRow(ctx, `
		SELECT id, service_provider_id, customer_id, fullname, email, role, is_active, password
		FROM "user"
		WHERE service_provider_id = $1 AND email = $2
	`, req.ServiceProviderID, req.Email).Scan(
		&userID, &spid, &customerID, &fullname, &email, &role, &isActive, &hash,
	)
	if err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	if !isActive {
		http.Error(w, "user inactive", http.StatusForbidden)
		return
	}

	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	claims := auth.Claims{
		UserID:          userID,
		ServiceProvider: spid,
		CustomerID:      customerID,
		Role:            role,
	}

	token, err := auth.SignToken(h.JWTSecret, claims)
	if err != nil {
		http.Error(w, "could not sign token", http.StatusInternalServerError)
		return
	}

	var resp loginResponse
	resp.Token = token
	resp.User.ID = userID
	resp.User.ServiceProvider = spid
	resp.User.CustomerID = customerID
	resp.User.Fullname = fullname
	resp.User.Email = email
	resp.User.Role = role

	WriteJSON(w, http.StatusOK, resp)
}
