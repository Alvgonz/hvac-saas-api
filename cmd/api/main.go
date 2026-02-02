package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/alvgonz/hvac-saas-api/internal/db"
	"github.com/alvgonz/hvac-saas-api/internal/httpapi"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Fatal("JWT_SECRET is required")
	}
	secret := []byte(jwtSecret)

	database, err := db.New(dsn)
	if err != nil {
		log.Fatalf("db connection error: %v", err)
	}
	defer database.Pool.Close()

	mux := http.NewServeMux()

	// =========================
	// Healthcheck
	// =========================
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := database.Pool.Ping(ctx); err != nil {
			http.Error(w, "db not ok", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// =========================
	// Auth
	// =========================
	authHandler := &httpapi.AuthHandler{
		DB:        database.Pool,
		JWTSecret: secret,
	}
	mux.HandleFunc("/auth/login", authHandler.Login)
	mux.Handle("/me", httpapi.AuthMiddleware(secret, http.HandlerFunc(httpapi.Me)))

	// =========================
	// Work Orders
	// =========================
	woHandler := &httpapi.WorkOrdersHandler{DB: database.Pool}

	// GET /work-orders
	// POST /work-orders

	reportsHandler := &httpapi.ReportsHandler{DB: database.Pool}

	mux.Handle("/reports/monthly", httpapi.AuthMiddleware(secret, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			reportsHandler.Monthly(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})))

	// PATCH /work-orders/{id}/complete
	mux.Handle(
		"/work-orders/",
		httpapi.AuthMiddleware(secret, http.HandlerFunc(woHandler.Complete)),
	)

	// =========================
	// Reports (PDF)
	// =========================
	reportsHandler = &httpapi.ReportsHandler{DB: database.Pool}
	mux.Handle(
		"/reports/monthly",
		httpapi.AuthMiddleware(secret, http.HandlerFunc(reportsHandler.Monthly)),
	)

	// =========================
	// Server
	// =========================
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080" // fallback local
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("API running on :%s\n", port)
	log.Fatal(srv.ListenAndServe())

}
