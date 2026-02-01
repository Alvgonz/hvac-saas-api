package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/alvgonz/hvac-saas-api/internal/db"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}

	database, err := db.New(dsn)
	if err != nil {
		log.Fatalf("db connection error: %v", err)
	}
	defer database.Pool.Close()

	mux := http.NewServeMux()

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

	srv := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Println("API running on :8080")
	log.Fatal(srv.ListenAndServe())
}
