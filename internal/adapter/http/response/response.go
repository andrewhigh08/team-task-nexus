package response

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/shalfey088/team-task-nexus/internal/pkg/apperror"
)

type Envelope struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   *ErrorBody  `json:"error,omitempty"`
}

type ErrorBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func JSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(Envelope{
		Success: true,
		Data:    data,
	}); err != nil {
		log.Printf("failed to encode response: %v", err)
	}
}

func Error(w http.ResponseWriter, err error) {
	if appErr, ok := apperror.IsAppError(err); ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(appErr.Code)
		if encErr := json.NewEncoder(w).Encode(Envelope{
			Success: false,
			Error:   &ErrorBody{Code: appErr.Code, Message: appErr.Message},
		}); encErr != nil {
			log.Printf("failed to encode error response: %v", encErr)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	if encErr := json.NewEncoder(w).Encode(Envelope{
		Success: false,
		Error:   &ErrorBody{Code: 500, Message: "internal server error"},
	}); encErr != nil {
		log.Printf("failed to encode error response: %v", encErr)
	}
}
