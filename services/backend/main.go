// Command backend is the HelloCalc calculator API service. It has no UI and
// no dependencies; the frontend service reaches it by service name.
package main

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"

	"github.com/Softbank-Hackathon-2026-Team-Daisy/sample-msa/internal/app"
	"github.com/Softbank-Hackathon-2026-Team-Daisy/sample-msa/internal/buildinfo"
	"github.com/Softbank-Hackathon-2026-Team-Daisy/sample-msa/internal/calculator"
	"github.com/Softbank-Hackathon-2026-Team-Daisy/sample-msa/internal/httpx"
)

// serviceName identifies this service in logs, /version and deploy.yaml.
const serviceName = "hellocalc-backend"

// maxBodyBytes bounds the size of API request bodies.
const maxBodyBytes = 4 << 10

func main() {
	app.Main(serviceName, build)
}

func build(_ func(string) string, _ *slog.Logger, info buildinfo.Info) (*http.ServeMux, *httpx.Probes, error) {
	probes := httpx.NewProbes(nil)
	mux := http.NewServeMux()
	probes.Register(mux)
	mux.HandleFunc("GET /version", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, info)
	})
	mux.HandleFunc("POST /api/calculate", handleCalculate)
	return mux, probes, nil
}

type calculateRequest struct {
	Left     *float64 `json:"left"`
	Operator *string  `json:"operator"`
	Right    *float64 `json:"right"`
}

type calculateResponse struct {
	Result float64 `json:"result"`
}

func handleCalculate(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		httpx.WriteError(w, http.StatusUnsupportedMediaType, "content type must be application/json")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var req calculateRequest
	if err := dec.Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		httpx.WriteError(w, http.StatusBadRequest, "invalid JSON request body")
		return
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		httpx.WriteError(w, http.StatusBadRequest, "request body must contain a single JSON object")
		return
	}
	if req.Left == nil || req.Operator == nil || req.Right == nil {
		httpx.WriteError(w, http.StatusBadRequest, "fields left, operator and right are required")
		return
	}

	result, err := calculator.Calculate(*req.Left, *req.Operator, *req.Right)
	switch {
	case err == nil:
		httpx.WriteJSON(w, http.StatusOK, calculateResponse{Result: result})
	case errors.Is(err, calculator.ErrUnsupportedOperator), errors.Is(err, calculator.ErrInvalidOperand):
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
	default:
		// Well-formed request whose arithmetic has no valid result
		// (division by zero, overflow, non-real result).
		httpx.WriteError(w, http.StatusUnprocessableEntity, err.Error())
	}
}
