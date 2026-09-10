package handler

import (
	"encoding/json"
	"net/http"

	"github.com/modigo/runner/protocol"
)

// Languages returns the list of supported programming languages.
func Languages(w http.ResponseWriter, r *http.Request) {
	resp := protocol.LanguagesResponse{
		Languages: []protocol.LanguageInfo{
			{ID: "python", Name: "Python", Version: "3.12", Extensions: []string{".py"}},
			{ID: "javascript", Name: "JavaScript", Version: "Node 22", Extensions: []string{".js", ".mjs"}},
			{ID: "c", Name: "C", Version: "GCC 14", Extensions: []string{".c"}},
			{ID: "cpp", Name: "C++", Version: "GCC 14", Extensions: []string{".cpp", ".cc"}},
			{ID: "go", Name: "Go", Version: "1.22", Extensions: []string{".go"}},
			{ID: "java", Name: "Java", Version: "Temurin 21", Extensions: []string{".java"}},
			{ID: "rust", Name: "Rust", Version: "1.82", Extensions: []string{".rs"}},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
