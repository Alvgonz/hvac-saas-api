package httpapi

import "net/http"

func Me(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	WriteJSON(w, http.StatusOK, claims)
}
