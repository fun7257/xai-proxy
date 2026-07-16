package proxy

import (
	"encoding/json"
	"net/http"
)

type errorBody struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

func writeJSONError(w http.ResponseWriter, status int, message, code string) {
	var body errorBody
	body.Error.Message = message
	body.Error.Type = code
	body.Error.Code = code
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
