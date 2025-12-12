package main

import (
	"encoding/json"
	"log"
	"net/http"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"message": "Hello from test backend!",
			"path":    r.URL.Path,
			"method":  r.Method,
			"headers": r.Header,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)

		log.Printf("Received request: %s %s", r.Method, r.URL.Path)
	})

	log.Println("Test backend starting on port 9000")
	http.ListenAndServe(":9000", nil)
}
