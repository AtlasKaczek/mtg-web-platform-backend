package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// GameTable represents a single MTG match lobby
type GameTable struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	HostName    string `json:"hostName"`
	Format      string `json:"format"`
	PlayerCount int    `json:"playerCount"`
	MaxPlayers  int    `json:"maxPlayers"`
}

var (
	// Mock database of tables
	currentTables = []GameTable{
		{ID: "1", Name: "Chill Commander", HostName: "Kaczeq", Format: "Commander", PlayerCount: 1, MaxPlayers: 4},
		{ID: "2", Name: "Standard Ranked", HostName: "Spike", Format: "Standard", PlayerCount: 1, MaxPlayers: 2},
		{ID: "3", Name: "Pauper Testing", HostName: "Johnny", Format: "Pauper", PlayerCount: 2, MaxPlayers: 2},
	}
	// Mutex prevents race conditions when multiple users create tables at once
	tableMutex sync.Mutex
)

// enableCORS is a middleware function that adds necessary headers for the frontend to fetch data
func enableCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*") // Allow all origins for local dev
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		// Handle preflight requests from the browser
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}

// handleGetTables returns the current list of tables
func handleGetTables(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tableMutex.Lock()
	defer tableMutex.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(currentTables)
}

// handleCreateTable adds a new table to the list
func handleCreateTable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var newTable GameTable
	if err := json.NewDecoder(r.Body).Decode(&newTable); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Assign a mock ID and default values
	newTable.ID = fmt.Sprintf("%d", time.Now().UnixNano())
	newTable.PlayerCount = 1

	tableMutex.Lock()
	currentTables = append(currentTables, newTable)
	tableMutex.Unlock()

	log.Printf("New table created: %s by %s", newTable.Name, newTable.HostName)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(newTable)
}

func main() {
	// Register the endpoints wrapped in the CORS middleware
	http.HandleFunc("/api/tables", enableCORS(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			handleGetTables(w, r)
		} else if r.Method == http.MethodPost {
			handleCreateTable(w, r)
		}
	}))

	log.Println("Go Backend REST API started on :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}