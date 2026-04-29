package server

import (
	"net/http"

	apperrors "github.com/fulmenhq/limensafe/internal/errors"
)

// HandleError central handler for all errors
func HandleError(w http.ResponseWriter, r *http.Request, err error) {
	apperrors.RespondWithError(w, r, err)
}
