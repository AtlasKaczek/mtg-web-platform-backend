package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/redis/go-redis/v9"
)

type GameTable struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	HostName    string `json:"hostName"`
	Format      string `json:"format"`
	PlayerCount int    `json:"playerCount"`
	MaxPlayers  int    `json:"maxPlayers"`
}

var (
	verifier *oidc.IDTokenVerifier
	rdb      *redis.Client
	ctx      = context.Background()
)

type keycloakClaims struct {
	PreferredUsername string `json:"preferred_username"`
}

func initOIDC() {
	providerURL := "http://localhost:8081/realms/mtg-realm"
	provider, err := oidc.NewProvider(context.Background(), providerURL)
	if err != nil {
		log.Fatalf("Failed to initialize OIDC provider: %v", err)
	}
	config := &oidc.Config{SkipClientIDCheck: true}
	verifier = provider.Verifier(config)
}

func initRedis() {
	rdb = redis.NewClient(&redis.Options{
		Addr:     "localhost:6379", // Pointing to our new Docker container
		Password: "",               // No password for local dev
		DB:       0,                // Default DB
	})

	// Test the connection
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	log.Println("Connected to Redis successfully!")

	// Optional: Seed Redis with a mock table if it's completely empty
	exists, _ := rdb.Exists(ctx, "mtg:tables").Result()
	if exists == 0 {
		mockTable := GameTable{ID: "1", Name: "Redis Commander Test", HostName: "System", Format: "Commander", PlayerCount: 0, MaxPlayers: 4}
		jsonBytes, _ := json.Marshal(mockTable)
		rdb.HSet(ctx, "mtg:tables", mockTable.ID, jsonBytes)
	}
}

func enableCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next(w, r)
	}
}

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, "Missing or invalid Authorization header", http.StatusUnauthorized)
			return
		}

		rawToken := strings.TrimPrefix(authHeader, "Bearer ")
		idToken, err := verifier.Verify(r.Context(), rawToken)
		if err != nil {
			log.Printf("Token verification failed: %v", err)
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		var claims keycloakClaims
		if err := idToken.Claims(&claims); err != nil {
			http.Error(w, "Failed to parse claims", http.StatusInternalServerError)
			return
		}

		reqCtx := context.WithValue(r.Context(), "username", claims.PreferredUsername)
		next(w, r.WithContext(reqCtx))
	}
}

func handleGetTables(w http.ResponseWriter, r *http.Request) {
	// Fetch all fields and values from the "mtg:tables" hash
	tablesMap, err := rdb.HGetAll(ctx, "mtg:tables").Result()
	if err != nil {
		http.Error(w, "Failed to fetch tables", http.StatusInternalServerError)
		return
	}

	var tables []GameTable
	for _, tableJSON := range tablesMap {
		var table GameTable
		if err := json.Unmarshal([]byte(tableJSON), &table); err == nil {
			tables = append(tables, table)
		}
	}

	// If no tables exist, return an empty array instead of null
	if tables == nil {
		tables = []GameTable{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tables)
}

func handleCreateTable(w http.ResponseWriter, r *http.Request) {
	var newTable GameTable
	if err := json.NewDecoder(r.Body).Decode(&newTable); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	verifiedUsername := r.Context().Value("username").(string)

	newTable.ID = fmt.Sprintf("%d", time.Now().UnixNano())
	newTable.HostName = verifiedUsername
	newTable.PlayerCount = 1

	// Marshal the struct to JSON to store in Redis
	jsonBytes, err := json.Marshal(newTable)
	if err != nil {
		http.Error(w, "Failed to encode table", http.StatusInternalServerError)
		return
	}

	// Save to Redis Hash: Key="mtg:tables", Field=TableID, Value=JSON
	err = rdb.HSet(ctx, "mtg:tables", newTable.ID, jsonBytes).Err()
	if err != nil {
		log.Printf("Failed to save table to Redis: %v", err)
		http.Error(w, "Failed to save table", http.StatusInternalServerError)
		return
	}

	log.Printf("Table created in Redis: %s by %s", newTable.Name, verifiedUsername)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(newTable)
}

func main() {
	initOIDC()
	initRedis()

	http.HandleFunc("/api/tables", enableCORS(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			handleGetTables(w, r)
		} else if r.Method == http.MethodPost {
			authMiddleware(handleCreateTable)(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	}))

	log.Println("Go Backend secured with Keycloak and backed by Redis! Running on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal(err)
	}
}