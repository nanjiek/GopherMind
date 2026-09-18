package main

import (
	"context"
	"fmt"
	"os"

	"gophermind/internal/config"
	postgresrepo "gophermind/internal/repo/postgres"
)

func main() {
	cfg := config.Load()
	db, err := postgresrepo.OpenDB(cfg.Postgres)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open postgres: %v\n", err)
		os.Exit(1)
	}
	pool, err := db.DB()
	if err != nil {
		fmt.Fprintf(os.Stderr, "get postgres pool: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := postgresrepo.ApplyMigrations(context.Background(), db); err != nil {
		fmt.Fprintf(os.Stderr, "apply migrations: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("postgres migrations are up to date")
}
