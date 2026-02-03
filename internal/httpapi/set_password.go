package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PasswordHandler struct {
	DB *pgxpool.Pool
}

type setPasswordRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

type setPasswordResponse struct {
	OK bool `json:"ok"`
}

func hashInviteToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

func (h *PasswordHandler) SetPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req setPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	req.Token = strings.TrimSpace(req.Token)
	req.Password = strings.TrimSpace(req.Password)

	if req.Token == "" || req.Password == "" {
		http.Error(w, "token and password are required", http.StatusBadRequest)
		return
	}
	if len(req.Password) < 6 {
		http.Error(w, "password too short", http.StatusBadRequest)
		return
	}

	tokenHash := hashInviteToken(req.Token)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Buscamos invitación válida (no usada, no expirada)
	var inviteID, userID string
	err := h.DB.QueryRow(ctx, `
		SELECT id, user_id
		FROM user_invitation
		WHERE token_hash = $1
		  AND used_at IS NULL
		  AND expires_at > now()
	`, tokenHash).Scan(&inviteID, &userID)
	if err != nil {
		http.Error(w, "invalid or expired token", http.StatusBadRequest)
		return
	}

	// Hash bcrypt
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "could not hash password", http.StatusInternalServerError)
		return
	}

	// Transacción: update password + mark used
	tx, err := h.DB.Begin(ctx)
	if err != nil {
		http.Error(w, "tx error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		UPDATE "user"
		SET password = $1, updated_at = now()
		WHERE id = $2
	`, string(hash), userID)
	if err != nil {
		http.Error(w, "could not set password", http.StatusInternalServerError)
		return
	}

	_, err = tx.Exec(ctx, `
		UPDATE user_invitation
		SET used_at = now()
		WHERE id = $1
	`, inviteID)
	if err != nil {
		http.Error(w, "could not mark invitation used", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		http.Error(w, "commit error", http.StatusInternalServerError)
		return
	}

	WriteJSON(w, http.StatusOK, setPasswordResponse{OK: true})
}
